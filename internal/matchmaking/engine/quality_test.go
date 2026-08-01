package engine

import (
	"fmt"
	"testing"
	"time"

	"github.com/ccKccK-JF/MatchMind/internal/matchmaking/domain"
)

func TestEvaluateQualityIdealMatch(t *testing.T) {
	now := time.Now()
	tickets := make([]*domain.MatchTicket, 0, 10)
	for index := range 10 {
		tickets = append(tickets, engineTicketWithRoles(
			t,
			fmt.Sprintf("ticket-%02d", index),
			fmt.Sprintf("player-%02d", index),
			"",
			1500,
			[]domain.Role{canonicalRoles[index%5]},
			now.Add(-time.Minute),
		))
	}
	formation, err := FormTeams(CandidateResult{Anchor: tickets[0], Tickets: tickets}, domain.DefaultPolicy())
	if err != nil {
		t.Fatal(err)
	}
	quality, err := EvaluateQuality(formation, "hongkong", now, domain.DefaultPolicy())
	if err != nil {
		t.Fatal(err)
	}
	if quality.SkillScore != 100 || quality.RoleScore != 100 || quality.PartyScore != 100 {
		t.Fatalf("ideal sub-scores = skill %.1f role %.1f party %.1f", quality.SkillScore, quality.RoleScore, quality.PartyScore)
	}
	if quality.TotalScore < 90 {
		t.Fatalf("total score = %.2f, want at least 90", quality.TotalScore)
	}
	if quality.PredictedWinRateA != 0.5 || quality.PredictedWinRateB != 0.5 {
		t.Fatalf("predicted win rates = %.3f/%.3f", quality.PredictedWinRateA, quality.PredictedWinRateB)
	}
}

func TestEvaluateQualityPenalizesMissingRegionLatency(t *testing.T) {
	now := time.Now()
	tickets := makeEngineTickets(t, 10, now.Add(-time.Minute))
	formation, err := FormTeams(CandidateResult{Anchor: tickets[0], Tickets: tickets}, domain.DefaultPolicy())
	if err != nil {
		t.Fatal(err)
	}
	quality, err := EvaluateQuality(formation, "tokyo", now, domain.DefaultPolicy())
	if err != nil {
		t.Fatal(err)
	}
	if quality.LatencyScore != 0 {
		t.Fatalf("latency score = %.2f, want 0", quality.LatencyScore)
	}
	if len(quality.Reasons) == 0 {
		t.Fatal("expected a network quality explanation")
	}
}
