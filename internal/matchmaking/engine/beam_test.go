package engine

import (
	"fmt"
	"testing"
	"time"

	"github.com/ccKccK-JF/MatchMind/internal/matchmaking/domain"
)

func TestBeamSearchImprovesRoleQualityAndIsDeterministic(t *testing.T) {
	now := time.Date(2026, time.August, 8, 12, 0, 0, 0, time.UTC)
	tickets := make([]*domain.MatchTicket, 0, 15)
	for index := range 10 {
		tickets = append(tickets, engineTicketWithRoles(
			t, fmt.Sprintf("early-%02d", index), fmt.Sprintf("early-player-%02d", index), "",
			1500, []domain.Role{domain.RoleCore}, now.Add(time.Duration(index)*time.Millisecond),
		))
	}
	for index, role := range canonicalRoles {
		tickets = append(tickets, engineTicketWithRoles(
			t, fmt.Sprintf("specialist-%02d", index), fmt.Sprintf("specialist-player-%02d", index), "",
			1500, []domain.Role{role}, now.Add(time.Duration(20+index)*time.Millisecond),
		))
	}
	candidates := CandidateResult{Anchor: tickets[0], Tickets: tickets}
	policy := domain.BeamPolicy()
	policy.BeamWidth = 128
	comparison, err := CompareTeamAlgorithms(candidates, "hongkong", now.Add(time.Second), policy)
	if err != nil {
		t.Fatal(err)
	}
	if comparison.Beam.Quality.RoleScore <= comparison.Greedy.Quality.RoleScore {
		t.Fatalf("Beam role score %.2f did not improve greedy %.2f", comparison.Beam.Quality.RoleScore, comparison.Greedy.Quality.RoleScore)
	}
	if comparison.Beam.Quality.TotalScore <= comparison.Greedy.Quality.TotalScore {
		t.Fatalf("Beam total score %.2f did not improve greedy %.2f", comparison.Beam.Quality.TotalScore, comparison.Greedy.Quality.TotalScore)
	}
	if comparison.Beam.Diagnostics.FormationsEvaluated == 0 || comparison.Beam.Diagnostics.CandidateSets == 0 {
		t.Fatalf("Beam diagnostics = %#v", comparison.Beam.Diagnostics)
	}

	second, err := CompareTeamAlgorithms(candidates, "hongkong", now.Add(time.Second), policy)
	if err != nil {
		t.Fatal(err)
	}
	if formationSignature(comparison.Beam.Formation) != formationSignature(second.Beam.Formation) {
		t.Fatalf("Beam output is not deterministic:\n%s\n%s", formationSignature(comparison.Beam.Formation), formationSignature(second.Beam.Formation))
	}
	if teamForTestPlayer(comparison.Beam.Formation, tickets[0].PlayerID()) == "" {
		t.Fatal("Beam Search dropped the anchor player")
	}
}

func TestBeamSearchKeepsPartyTogether(t *testing.T) {
	now := time.Now().UTC()
	tickets := make([]*domain.MatchTicket, 0, 12)
	for index := range 12 {
		partyID := ""
		if index < 3 {
			partyID = "party-anchor"
		}
		tickets = append(tickets, engineTicketWithRoles(
			t, fmt.Sprintf("ticket-%02d", index), fmt.Sprintf("player-%02d", index), partyID,
			1500+float64(index%2)*10, []domain.Role{canonicalRoles[index%5]}, now.Add(time.Duration(index)*time.Millisecond),
		))
	}
	policy := domain.BeamPolicy()
	formation, _, _, err := OptimizeTeams(
		CandidateResult{Anchor: tickets[0], Tickets: tickets}, "hongkong", now.Add(time.Second), policy,
	)
	if err != nil {
		t.Fatal(err)
	}
	team := teamForTestPlayer(formation, "player-00")
	if team == "" || teamForTestPlayer(formation, "player-01") != team || teamForTestPlayer(formation, "player-02") != team {
		t.Fatal("Beam Search split the anchor party")
	}
}

func teamForTestPlayer(formation TeamFormation, playerID string) string {
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
