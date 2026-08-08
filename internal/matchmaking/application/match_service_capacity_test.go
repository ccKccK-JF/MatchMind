package application_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/ccKccK-JF/MatchMind/internal/matchmaking/application"
	"github.com/ccKccK-JF/MatchMind/internal/matchmaking/domain"
	"github.com/ccKccK-JF/MatchMind/internal/matchmaking/repository/memory"
)

func TestMatchCompletionReleasesLocalServerCapacity(t *testing.T) {
	now := time.Date(2026, time.August, 8, 12, 0, 0, 0, time.UTC)
	allocator, err := application.NewLocalAllocatorWithCapacities(
		map[string]int{"tokyo": 1},
		func() (string, error) { return "connection-token", nil },
	)
	if err != nil {
		t.Fatal(err)
	}
	allocation, err := allocator.Allocate(context.Background(), "match-1", "tokyo")
	if err != nil {
		t.Fatal(err)
	}
	match, err := domain.NewMatch(domain.NewMatchParams{
		ID: "match-1", Mode: "ranked_5v5", TeamA: capacityTestTeam("a", 0), TeamB: capacityTestTeam("b", 5),
		ServerRegion: "tokyo", PolicyVersion: "v1", Quality: domain.MatchQuality{
			TotalScore: 90, SkillScore: 90, RoleScore: 90, LatencyScore: 90, PartyScore: 90, WaitScore: 90,
			PredictedWinRateA: 0.5, PredictedWinRateB: 0.5,
		}, CreatedAt: now,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := match.StartAllocation(now); err != nil {
		t.Fatal(err)
	}
	if err := match.MarkReady(allocation.Address, allocation.Token, now); err != nil {
		t.Fatal(err)
	}
	store := memory.NewMatchStore()
	if err := store.Create(context.Background(), match); err != nil {
		t.Fatal(err)
	}
	clock := now
	service := application.NewMatchService(store, nil, func() time.Time { return clock })
	service.SetServerReleaser(allocator)
	if _, err := service.StartMatch(context.Background(), match.ID()); err != nil {
		t.Fatal(err)
	}
	clock = now.Add(20 * time.Minute)
	if _, err := service.CompleteMatch(context.Background(), match.ID(), domain.MatchResult{
		WinningTeam: domain.WinningTeamA, DurationSeconds: 1200, ScoreA: 20, ScoreB: 15,
		MaxAdvantage: 5000, ActualQualityScore: 85,
	}); err != nil {
		t.Fatal(err)
	}
	capacities, err := allocator.Capacities(context.Background())
	if err != nil || len(capacities) != 1 || capacities[0].AvailableServers != 1 {
		t.Fatalf("capacity after completion = %+v, %v", capacities, err)
	}
}

func capacityTestTeam(id string, offset int) domain.MatchTeam {
	roles := []domain.Role{domain.RoleVanguard, domain.RoleRoamer, domain.RoleCore, domain.RoleRanged, domain.RoleSupport}
	team := domain.MatchTeam{ID: "team-" + id, AverageRating: 1500}
	for index := range 5 {
		player := index + offset
		team.Players = append(team.Players, domain.MatchPlayer{
			PlayerID: fmt.Sprintf("player-%d", player), TicketID: fmt.Sprintf("ticket-%d", player),
			Role: roles[index], Rating: 1500,
		})
	}
	return team
}
