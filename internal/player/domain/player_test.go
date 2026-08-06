package domain

import (
	"errors"
	"testing"
	"time"
)

func TestNewPlayer(t *testing.T) {
	createdAt := time.Date(2026, time.August, 1, 10, 0, 0, 0, time.FixedZone("CST", 8*60*60))
	roles := []Role{RoleCore, RoleSupport}
	latency := map[string]int{"singapore": 32, "hongkong": 48}

	player, err := NewPlayer(NewPlayerParams{
		ID:             " player-1001 ",
		Name:           " Nova ",
		InitialRating:  1800,
		PreferredRoles: roles,
		HomeRegion:     " HONGKONG ",
		RegionLatency:  latency,
		BehaviorScore:  96,
		CreatedAt:      createdAt,
	})
	if err != nil {
		t.Fatalf("NewPlayer() error = %v", err)
	}

	if player.ID() != "player-1001" {
		t.Fatalf("ID() = %q, want player-1001", player.ID())
	}
	if player.Name() != "Nova" {
		t.Fatalf("Name() = %q, want Nova", player.Name())
	}
	if player.HomeRegion() != "hongkong" {
		t.Fatalf("HomeRegion() = %q, want hongkong", player.HomeRegion())
	}
	if player.RatingDeviation() != DefaultRatingDeviation {
		t.Fatalf("RatingDeviation() = %v, want %v", player.RatingDeviation(), DefaultRatingDeviation)
	}
	if !player.CreatedAt().Equal(createdAt.UTC()) {
		t.Fatalf("CreatedAt() = %v, want %v", player.CreatedAt(), createdAt.UTC())
	}

	roles[0] = RoleVanguard
	latency["hongkong"] = 999
	if player.PreferredRoles()[0] != RoleCore {
		t.Fatal("player roles changed when the constructor input was mutated")
	}
	if player.RegionLatency()["hongkong"] != 48 {
		t.Fatal("player latency changed when the constructor input was mutated")
	}
}

func TestNewPlayerRejectsInvalidInput(t *testing.T) {
	valid := func() NewPlayerParams {
		return NewPlayerParams{
			ID:             "player-1001",
			Name:           "Nova",
			InitialRating:  1500,
			PreferredRoles: []Role{RoleCore},
			HomeRegion:     "hongkong",
			RegionLatency:  map[string]int{"hongkong": 30},
			BehaviorScore:  90,
			CreatedAt:      time.Now(),
		}
	}

	tests := []struct {
		name   string
		change func(*NewPlayerParams)
	}{
		{name: "missing id", change: func(p *NewPlayerParams) { p.ID = "" }},
		{name: "missing name", change: func(p *NewPlayerParams) { p.Name = "" }},
		{name: "invalid rating", change: func(p *NewPlayerParams) { p.InitialRating = 0 }},
		{name: "missing roles", change: func(p *NewPlayerParams) { p.PreferredRoles = nil }},
		{name: "unsupported role", change: func(p *NewPlayerParams) { p.PreferredRoles = []Role{"mage"} }},
		{name: "duplicate role", change: func(p *NewPlayerParams) { p.PreferredRoles = []Role{RoleCore, RoleCore} }},
		{name: "missing region", change: func(p *NewPlayerParams) { p.HomeRegion = "" }},
		{name: "missing latency", change: func(p *NewPlayerParams) { p.RegionLatency = nil }},
		{name: "negative latency", change: func(p *NewPlayerParams) { p.RegionLatency = map[string]int{"hongkong": -1} }},
		{name: "behavior over maximum", change: func(p *NewPlayerParams) { p.BehaviorScore = 101 }},
		{name: "missing created time", change: func(p *NewPlayerParams) { p.CreatedAt = time.Time{} }},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			params := valid()
			test.change(&params)
			_, err := NewPlayer(params)
			if !errors.Is(err, ErrInvalidPlayer) {
				t.Fatalf("NewPlayer() error = %v, want ErrInvalidPlayer", err)
			}
		})
	}
}

func TestPlayerAccessorsReturnCopies(t *testing.T) {
	player, err := NewPlayer(NewPlayerParams{
		ID:             "player-1001",
		Name:           "Nova",
		InitialRating:  1500,
		PreferredRoles: []Role{RoleCore},
		HomeRegion:     "hongkong",
		RegionLatency:  map[string]int{"hongkong": 30},
		BehaviorScore:  90,
		CreatedAt:      time.Now(),
	})
	if err != nil {
		t.Fatal(err)
	}

	roles := player.PreferredRoles()
	roles[0] = RoleSupport
	latency := player.RegionLatency()
	latency["hongkong"] = 500

	if player.PreferredRoles()[0] != RoleCore {
		t.Fatal("PreferredRoles returned mutable internal state")
	}
	if player.RegionLatency()["hongkong"] != 30 {
		t.Fatal("RegionLatency returned mutable internal state")
	}
}

func TestRestorePlayerPreservesDurableRatingDeviation(t *testing.T) {
	createdAt := time.Now().UTC().Truncate(time.Microsecond)
	player, err := RestorePlayer(PlayerSnapshot{
		ID: "player-1", Name: "Nova", Rating: 1725, RatingDeviation: 82,
		PreferredRoles: []Role{RoleCore}, HomeRegion: "hongkong",
		RegionLatency: map[string]int{"hongkong": 31}, BehaviorScore: 97,
		CreatedAt: createdAt,
	})
	if err != nil {
		t.Fatal(err)
	}
	if player.Rating() != 1725 || player.RatingDeviation() != 82 || !player.CreatedAt().Equal(createdAt) {
		t.Fatalf("restored player = rating %v, deviation %v, created %v", player.Rating(), player.RatingDeviation(), player.CreatedAt())
	}
}

func TestRestorePlayerRejectsInvalidSnapshot(t *testing.T) {
	_, err := RestorePlayer(PlayerSnapshot{
		ID: "player-1", Name: "Nova", Rating: 1500, RatingDeviation: 0,
		PreferredRoles: []Role{RoleCore}, HomeRegion: "hongkong",
		RegionLatency: map[string]int{"hongkong": 31}, BehaviorScore: 97,
		CreatedAt: time.Now(),
	})
	if !errors.Is(err, ErrInvalidPlayer) {
		t.Fatalf("RestorePlayer() error = %v, want ErrInvalidPlayer", err)
	}
}
