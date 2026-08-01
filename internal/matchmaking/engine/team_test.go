package engine

import (
	"fmt"
	"math"
	"testing"
	"time"

	"github.com/ccKccK-JF/MatchMind/internal/matchmaking/domain"
)

func TestFormTeamsBalancesRatingsAndAssignsEveryRole(t *testing.T) {
	now := time.Now()
	tickets := make([]*domain.MatchTicket, 0, 10)
	for index := range 10 {
		role := canonicalRoles[index%5]
		tickets = append(tickets, engineTicketWithRoles(
			t,
			fmt.Sprintf("ticket-%02d", index),
			fmt.Sprintf("player-%02d", index),
			"",
			1500+float64(index)*10,
			[]domain.Role{role},
			now.Add(time.Duration(index)*time.Second),
		))
	}
	candidates := CandidateResult{Anchor: tickets[0], Tickets: tickets}
	formation, err := FormTeams(candidates, domain.DefaultPolicy())
	if err != nil {
		t.Fatal(err)
	}
	if len(formation.TeamA.Players) != 5 || len(formation.TeamB.Players) != 5 {
		t.Fatalf("team sizes = %d/%d", len(formation.TeamA.Players), len(formation.TeamB.Players))
	}
	if math.Abs(formation.TeamA.AverageRating-formation.TeamB.AverageRating) > 10 {
		t.Fatalf("average ratings = %.2f/%.2f", formation.TeamA.AverageRating, formation.TeamB.AverageRating)
	}
	if formation.TeamA.RoleScore < 80 || formation.TeamB.RoleScore < 80 {
		t.Fatalf("role scores = %.2f/%.2f, want at least 80", formation.TeamA.RoleScore, formation.TeamB.RoleScore)
	}
	assertEveryRoleOnce(t, formation.TeamA)
	assertEveryRoleOnce(t, formation.TeamB)
}

func TestFormTeamsKeepsPartiesTogether(t *testing.T) {
	now := time.Now()
	tickets := make([]*domain.MatchTicket, 0, 10)
	parties := []string{"party-a", "party-a", "party-a", "party-b", "party-b", "", "", "", "", ""}
	for index := range 10 {
		tickets = append(tickets, engineTicketWithRoles(
			t,
			fmt.Sprintf("ticket-%02d", index),
			fmt.Sprintf("player-%02d", index),
			parties[index],
			1500,
			[]domain.Role{canonicalRoles[index%5]},
			now.Add(time.Duration(index)*time.Second),
		))
	}
	formation, err := FormTeams(CandidateResult{Anchor: tickets[0], Tickets: tickets}, domain.DefaultPolicy())
	if err != nil {
		t.Fatal(err)
	}
	if teamForPlayer(formation, "player-00") != teamForPlayer(formation, "player-01") ||
		teamForPlayer(formation, "player-01") != teamForPlayer(formation, "player-02") {
		t.Fatal("party-a was split across teams")
	}
	if teamForPlayer(formation, "player-03") != teamForPlayer(formation, "player-04") {
		t.Fatal("party-b was split across teams")
	}
}

func assertEveryRoleOnce(t *testing.T, team Team) {
	t.Helper()
	seen := make(map[domain.Role]int)
	for _, player := range team.Players {
		seen[player.Role]++
	}
	for _, role := range canonicalRoles {
		if seen[role] != 1 {
			t.Fatalf("role %s count = %d, want 1", role, seen[role])
		}
	}
}

func teamForPlayer(formation TeamFormation, playerID string) string {
	for _, player := range formation.TeamA.Players {
		if player.Ticket.PlayerID() == playerID {
			return "A"
		}
	}
	for _, player := range formation.TeamB.Players {
		if player.Ticket.PlayerID() == playerID {
			return "B"
		}
	}
	return ""
}

func engineTicketWithRoles(
	t *testing.T,
	ticketID, playerID, partyID string,
	rating float64,
	roles []domain.Role,
	createdAt time.Time,
) *domain.MatchTicket {
	t.Helper()
	ticket, err := domain.NewTicket(domain.NewTicketParams{
		ID:             ticketID,
		PlayerID:       playerID,
		PartyID:        partyID,
		Mode:           "ranked_5v5",
		ClientVersion:  "1.0.0",
		Region:         "hongkong",
		Rating:         rating,
		PreferredRoles: roles,
		RegionLatency:  map[string]int{"hongkong": 30},
		CreatedAt:      createdAt,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := ticket.Queue(createdAt); err != nil {
		t.Fatal(err)
	}
	return ticket
}
