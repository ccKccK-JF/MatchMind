package application

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/ccKccK-JF/MatchMind/internal/matchmaking/domain"
	"github.com/ccKccK-JF/MatchMind/internal/matchmaking/engine"
)

type selectorAllocator struct {
	capacities []RegionCapacity
}

func (a selectorAllocator) Capacities(context.Context) ([]RegionCapacity, error) {
	return append([]RegionCapacity(nil), a.capacities...), nil
}

func (selectorAllocator) Allocate(context.Context, string, string) (Allocation, error) {
	return Allocation{}, nil
}

func TestSelectServerRegionBalancesNetworkQualityAndCapacity(t *testing.T) {
	now := time.Date(2026, time.August, 8, 12, 0, 0, 0, time.UTC)
	policy := domain.DefaultPolicy()
	tickets := selectorTickets(t, now, func(index int) map[string]int {
		return map[string]int{"hongkong": 100, "tokyo": 20}
	})
	candidates, err := engine.GenerateCandidates(tickets, now, policy)
	if err != nil {
		t.Fatal(err)
	}
	selection, err := selectServerRegion(context.Background(), selectorAllocator{capacities: []RegionCapacity{
		{Region: "hongkong", AvailableServers: 10},
		{Region: "tokyo", AvailableServers: 1},
	}}, candidates, now, policy)
	if err != nil {
		t.Fatal(err)
	}
	if selection.Region != "tokyo" {
		t.Fatalf("selected region = %s, want tokyo", selection.Region)
	}
	if selection.AverageLatencyMS != 20 || selection.MaxLatencyMS != 20 || selection.AvailableServers != 1 {
		t.Fatalf("selection metrics = %+v", selection)
	}
}

func TestScoreServerRegionPenalizesTeamDifferenceAndVariance(t *testing.T) {
	now := time.Now()
	tickets := selectorTickets(t, now, func(index int) map[string]int {
		return map[string]int{"uneven": 10, "balanced": 55}
	})
	formation := engine.TeamFormation{
		TeamA: selectorTeam(tickets[:5]),
		TeamB: selectorTeam(tickets[5:]),
	}
	for index := range 5 {
		latencies := tickets[index+5].RegionLatency()
		latencies["uneven"] = 90
		tickets[index+5] = replaceSelectorTicketLatency(t, tickets[index+5], latencies)
	}
	formation.TeamB = selectorTeam(tickets[5:])
	uneven, ok := scoreServerRegion(formation, RegionCapacity{Region: "uneven", AvailableServers: 5}, 5, 120, 250)
	if !ok {
		t.Fatal("uneven region unexpectedly ineligible")
	}
	balanced, ok := scoreServerRegion(formation, RegionCapacity{Region: "balanced", AvailableServers: 5}, 5, 120, 250)
	if !ok {
		t.Fatal("balanced region unexpectedly ineligible")
	}
	if balanced.Score <= uneven.Score || uneven.TeamAverageDifferenceMS != 80 || uneven.LatencyStandardDeviation != 40 {
		t.Fatalf("balanced/uneven selections = %+v / %+v", balanced, uneven)
	}
}

func TestSelectServerRegionRequiresCapacityAndAdmissibleLatency(t *testing.T) {
	now := time.Now()
	policy := domain.DefaultPolicy()
	tickets := selectorTickets(t, now, func(index int) map[string]int {
		return map[string]int{"hongkong": 30, "tokyo": 130}
	})
	candidates, err := engine.GenerateCandidates(tickets, now, policy)
	if err != nil {
		t.Fatal(err)
	}
	_, err = selectServerRegion(context.Background(), selectorAllocator{}, candidates, now, policy)
	if !errors.Is(err, ErrNoServerCapacity) {
		t.Fatalf("empty capacities error = %v", err)
	}
	_, err = selectServerRegion(context.Background(), selectorAllocator{capacities: []RegionCapacity{{Region: "tokyo", AvailableServers: 2}}}, candidates, now, policy)
	if !errors.Is(err, ErrNoSuitableServerRegion) {
		t.Fatalf("inadmissible latency error = %v", err)
	}
}

func selectorTickets(t *testing.T, now time.Time, latencies func(int) map[string]int) []*domain.MatchTicket {
	t.Helper()
	roles := []domain.Role{domain.RoleVanguard, domain.RoleRoamer, domain.RoleCore, domain.RoleRanged, domain.RoleSupport}
	result := make([]*domain.MatchTicket, 0, 10)
	for index := range 10 {
		ticket, err := domain.NewTicket(domain.NewTicketParams{
			ID: fmt.Sprintf("ticket-%02d", index), PlayerID: fmt.Sprintf("player-%02d", index),
			Mode: "ranked_5v5", ClientVersion: "1.0.0", Region: "hongkong", Rating: 1500,
			PreferredRoles: []domain.Role{roles[index%5]}, RegionLatency: latencies(index), CreatedAt: now,
		})
		if err != nil {
			t.Fatal(err)
		}
		if err := ticket.Queue(now); err != nil {
			t.Fatal(err)
		}
		result = append(result, ticket)
	}
	return result
}

func replaceSelectorTicketLatency(t *testing.T, ticket *domain.MatchTicket, latencies map[string]int) *domain.MatchTicket {
	t.Helper()
	restored, err := domain.NewTicket(domain.NewTicketParams{
		ID: ticket.ID(), PlayerID: ticket.PlayerID(), PartyID: ticket.PartyID(), Mode: ticket.Mode(),
		ClientVersion: ticket.ClientVersion(), Region: ticket.Region(), Rating: ticket.Rating(),
		PreferredRoles: ticket.PreferredRoles(), RegionLatency: latencies, CreatedAt: ticket.CreatedAt(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := restored.Queue(ticket.CreatedAt()); err != nil {
		t.Fatal(err)
	}
	return restored
}

func selectorTeam(tickets []*domain.MatchTicket) engine.Team {
	team := engine.Team{Players: make([]engine.AssignedPlayer, 0, len(tickets))}
	for _, ticket := range tickets {
		team.Players = append(team.Players, engine.AssignedPlayer{Ticket: ticket, Role: domain.RoleCore})
		team.AverageRating += ticket.Rating()
	}
	team.AverageRating /= float64(len(tickets))
	return team
}
