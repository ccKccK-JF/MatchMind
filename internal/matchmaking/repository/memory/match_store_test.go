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
