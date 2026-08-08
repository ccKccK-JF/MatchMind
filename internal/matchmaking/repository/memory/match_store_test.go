package memory

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/ccKccK-JF/MatchMind/internal/matchmaking/application"
	"github.com/ccKccK-JF/MatchMind/internal/matchmaking/domain"
)

func TestMatchStoreCreateGetUpdate(t *testing.T) {
	store := NewMatchStore()
	match := newTestMatchForStore(t)
	if err := store.Create(context.Background(), match); err != nil {
		t.Fatal(err)
	}
	if err := store.Create(context.Background(), match); !errors.Is(err, application.ErrMatchAlreadyExists) {
		t.Fatalf("second Create() error = %v, want ErrMatchAlreadyExists", err)
	}
	got, err := store.Get(context.Background(), match.ID())
	if err != nil || got.ID() != match.ID() {
		t.Fatalf("Get() = %v, %v", got, err)
	}
	if err := got.StartAllocation(time.Now()); err != nil {
		t.Fatal(err)
	}
	if err := store.Update(context.Background(), got); err != nil {
		t.Fatal(err)
	}
	updated, _ := store.Get(context.Background(), match.ID())
	if updated.State() != got.State() {
		t.Fatalf("updated state = %s, want %s", updated.State(), got.State())
	}
	stale := match.Clone()
	if err := stale.StartAllocation(time.Now()); err != nil {
		t.Fatal(err)
	}
	if err := store.Update(context.Background(), stale); !errors.Is(err, application.ErrMatchRevisionConflict) {
		t.Fatalf("stale Update() error = %v, want ErrMatchRevisionConflict", err)
	}
}

func TestMatchStoreListFinishedAppliesFilters(t *testing.T) {
	ctx := context.Background()
	store := NewMatchStore()
	match := newTestMatchForStore(t)
	createdAt := match.CreatedAt()
	if err := store.Create(ctx, match); err != nil {
		t.Fatal(err)
	}
	transitions := []func() error{
		func() error { return match.StartAllocation(createdAt.Add(time.Second)) },
		func() error { return match.MarkReady("127.0.0.1:7000", "token", createdAt.Add(2*time.Second)) },
		func() error { return match.Start(createdAt.Add(3 * time.Second)) },
		func() error {
			return match.Complete(domain.MatchResult{
				WinningTeam: domain.WinningTeamA, DurationSeconds: 1200,
				ScoreA: 20, ScoreB: 15, MaxAdvantage: 5000, ActualQualityScore: 85,
			}, createdAt.Add(20*time.Minute))
		},
	}
	for _, transition := range transitions {
		if err := transition(); err != nil {
			t.Fatal(err)
		}
		if err := store.Update(ctx, match); err != nil {
			t.Fatal(err)
		}
	}
	result, err := store.ListFinished(ctx, application.MatchHistoryFilter{
		PolicyVersion: "v1", Mode: "ranked_5v5", ServerRegion: "hongkong",
		From: createdAt.Add(-time.Second), To: createdAt.Add(time.Second), Limit: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(result) != 1 || result[0].ID() != match.ID() {
		t.Fatalf("finished matches = %#v", result)
	}
	filtered, err := store.ListFinished(ctx, application.MatchHistoryFilter{PolicyVersion: "other", Limit: 10})
	if err != nil || len(filtered) != 0 {
		t.Fatalf("filtered matches = %#v, %v", filtered, err)
	}
}

func newTestMatchForStore(t *testing.T) *domain.Match {
	t.Helper()
	team := func(prefix string) domain.MatchTeam {
		players := make([]domain.MatchPlayer, 0, 5)
		for index := 1; index <= 5; index++ {
			players = append(players, domain.MatchPlayer{
				PlayerID: fmt.Sprintf("%s-player-%d", prefix, index),
				TicketID: fmt.Sprintf("%s-ticket-%d", prefix, index),
				Role:     []domain.Role{domain.RoleVanguard, domain.RoleRoamer, domain.RoleCore, domain.RoleRanged, domain.RoleSupport}[index-1],
				Rating:   1500,
			})
		}
		return domain.MatchTeam{ID: "team-" + prefix, Players: players, AverageRating: 1500}
	}
	match, err := domain.NewMatch(domain.NewMatchParams{
		ID:            "match-1",
		Mode:          "ranked_5v5",
		TeamA:         team("a"),
		TeamB:         team("b"),
		ServerRegion:  "hongkong",
		PolicyVersion: "v1",
		Quality: domain.MatchQuality{
			TotalScore: 90, SkillScore: 100, RoleScore: 100, LatencyScore: 80,
			PartyScore: 100, WaitScore: 90, PredictedWinRateA: 0.5, PredictedWinRateB: 0.5,
		},
		CreatedAt: time.Now(),
	})
	if err != nil {
		t.Fatal(err)
	}
	return match
}
