package domain

import (
	"errors"
	"testing"
	"time"
)

func TestRatingChangeAndPlayerWithRating(t *testing.T) {
	createdAt := time.Now()
	change, err := NewRatingChange("player-1", "match-1", 1500, 1516, "ranked_match", createdAt)
	if err != nil {
		t.Fatal(err)
	}
	if change.Delta() != 16 {
		t.Fatalf("Delta() = %v, want 16", change.Delta())
	}

	player, err := NewPlayer(NewPlayerParams{
		ID:             "player-1",
		Name:           "Nova",
		InitialRating:  1500,
		PreferredRoles: []Role{RoleCore},
		HomeRegion:     "hongkong",
		RegionLatency:  map[string]int{"hongkong": 30},
		BehaviorScore:  90,
		CreatedAt:      createdAt,
	})
	if err != nil {
		t.Fatal(err)
	}
	updated, err := player.WithRating(change.After())
	if err != nil {
		t.Fatal(err)
	}
	if player.Rating() != 1500 || updated.Rating() != 1516 {
		t.Fatalf("ratings before/after = %v/%v, want 1500/1516", player.Rating(), updated.Rating())
	}
}

func TestRatingChangeAndPlayerWithFullRatingState(t *testing.T) {
	createdAt := time.Now()
	change, err := NewRatingChangeWithState(NewRatingChangeParams{
		PlayerID: "player-1", MatchID: "match-1",
		Before: RatingState{Rating: 1500, Deviation: 350, Volatility: 0.06},
		After:  RatingState{Rating: 1620, Deviation: 210, Volatility: 0.0599},
		System: RatingSystemGlicko2, Reason: "ranked_match", CreatedAt: createdAt,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !change.HasUncertaintyState() || change.System() != RatingSystemGlicko2 ||
		change.DeviationAfter() != 210 || change.VolatilityAfter() != 0.0599 {
		t.Fatalf("rating change = %+v", change)
	}
	player, err := NewPlayer(NewPlayerParams{
		ID: "player-1", Name: "Nova", InitialRating: 1500,
		PreferredRoles: []Role{RoleCore}, HomeRegion: "hongkong",
		RegionLatency: map[string]int{"hongkong": 30}, BehaviorScore: 90, CreatedAt: createdAt,
	})
	if err != nil {
		t.Fatal(err)
	}
	updated, err := player.WithRatingState(RatingState{
		Rating: change.After(), Deviation: change.DeviationAfter(), Volatility: change.VolatilityAfter(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if updated.Rating() != 1620 || updated.RatingDeviation() != 210 || updated.RatingVolatility() != 0.0599 {
		t.Fatalf("updated state = %+v", updated.RatingState())
	}
}

func TestNewRatingChangeRejectsInvalidInput(t *testing.T) {
	_, err := NewRatingChange("", "match-1", 1500, 1516, "ranked_match", time.Now())
	if !errors.Is(err, ErrInvalidRatingChange) {
		t.Fatalf("NewRatingChange() error = %v, want ErrInvalidRatingChange", err)
	}
}
