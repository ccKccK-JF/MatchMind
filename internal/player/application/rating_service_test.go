package application_test

import (
	"context"
	"errors"
	"fmt"
	"math"
	"testing"
	"time"

	"github.com/ccKccK-JF/MatchMind/internal/player/application"
	"github.com/ccKccK-JF/MatchMind/internal/player/domain"
	"github.com/ccKccK-JF/MatchMind/internal/player/repository/memory"
	"github.com/ccKccK-JF/MatchMind/internal/rating/elo"
	"github.com/ccKccK-JF/MatchMind/internal/rating/glicko2"
)

func TestRatingServiceRecordsIdempotentTeamResult(t *testing.T) {
	ctx := context.Background()
	repository := memory.NewRepository()
	playerService := application.NewService(repository, nil)
	teamA := createRatedTeam(t, ctx, playerService, "a", 1600)
	teamB := createRatedTeam(t, ctx, playerService, "b", 1500)
	calculator, err := elo.NewCalculator(32)
	if err != nil {
		t.Fatal(err)
	}
	updatedAt := time.Date(2026, time.August, 1, 14, 0, 0, 0, time.UTC)
	ratingService := application.NewRatingService(repository, calculator, func() time.Time { return updatedAt })
	command := application.RecordMatchResultCommand{
		MatchID:        "match-1001",
		TeamAPlayerIDs: teamA,
		TeamBPlayerIDs: teamB,
		Outcome:        application.MatchOutcomeTeamAWin,
		Reason:         "ranked_match",
	}

	changes, err := ratingService.RecordMatchResult(ctx, command)
	if err != nil {
		t.Fatalf("RecordMatchResult() error = %v", err)
	}
	if len(changes) != 10 {
		t.Fatalf("changes = %d, want 10", len(changes))
	}

	var totalDelta float64
	for index, change := range changes {
		totalDelta += change.Delta()
		if index < 5 && change.Delta() <= 0 {
			t.Fatalf("team A change %d delta = %v, want positive", index, change.Delta())
		}
		if index >= 5 && change.Delta() >= 0 {
			t.Fatalf("team B change %d delta = %v, want negative", index, change.Delta())
		}
	}
	if math.Abs(totalDelta) > 1e-9 {
		t.Fatalf("total rating delta = %.12f, want zero", totalDelta)
	}

	firstAfter, err := playerService.GetPlayer(ctx, teamA[0])
	if err != nil {
		t.Fatal(err)
	}
	history, err := ratingService.History(ctx, teamA[0])
	if err != nil {
		t.Fatal(err)
	}
	if len(history) != 1 || history[0].MatchID() != command.MatchID {
		t.Fatalf("rating history = %+v, want one entry for %s", history, command.MatchID)
	}

	duplicateChanges, err := ratingService.RecordMatchResult(ctx, command)
	if err != nil {
		t.Fatalf("duplicate RecordMatchResult() error = %v", err)
	}
	if len(duplicateChanges) != len(changes) {
		t.Fatalf("duplicate changes = %d, want %d", len(duplicateChanges), len(changes))
	}
	secondAfter, err := playerService.GetPlayer(ctx, teamA[0])
	if err != nil {
		t.Fatal(err)
	}
	if secondAfter.Rating() != firstAfter.Rating() {
		t.Fatalf("duplicate result changed rating from %v to %v", firstAfter.Rating(), secondAfter.Rating())
	}
}

func TestRatingServiceRejectsInvalidTeams(t *testing.T) {
	repository := memory.NewRepository()
	calculator, _ := elo.NewCalculator(32)
	service := application.NewRatingService(repository, calculator, nil)

	teamA := []string{"duplicate", "a2", "a3", "a4", "a5"}
	teamB := []string{"duplicate", "b2", "b3", "b4", "b5"}
	_, err := service.RecordMatchResult(context.Background(), application.RecordMatchResultCommand{
		MatchID:        "match-1001",
		TeamAPlayerIDs: teamA,
		TeamBPlayerIDs: teamB,
		Outcome:        application.MatchOutcomeTeamAWin,
	})
	if !errors.Is(err, application.ErrInvalidMatchResult) {
		t.Fatalf("RecordMatchResult() error = %v, want ErrInvalidMatchResult", err)
	}
}

func TestGlicko2RatingServiceUpdatesUncertaintyAndIsIdempotent(t *testing.T) {
	ctx := context.Background()
	repository := memory.NewRepository()
	playerService := application.NewService(repository, nil)
	teamA := createRatedTeam(t, ctx, playerService, "glicko-a", 1600)
	teamB := createRatedTeam(t, ctx, playerService, "glicko-b", 1500)
	calculator, err := glicko2.NewCalculator(0.5)
	if err != nil {
		t.Fatal(err)
	}
	ratingService := application.NewGlicko2RatingService(repository, calculator, nil)
	if ratingService.System() != domain.RatingSystemGlicko2 {
		t.Fatalf("System() = %q, want %q", ratingService.System(), domain.RatingSystemGlicko2)
	}
	command := application.RecordMatchResultCommand{
		MatchID:        "glicko-match-1",
		TeamAPlayerIDs: teamA,
		TeamBPlayerIDs: teamB,
		Outcome:        application.MatchOutcomeTeamAWin,
		Reason:         "ranked_match",
	}

	changes, err := ratingService.RecordMatchResult(ctx, command)
	if err != nil {
		t.Fatalf("RecordMatchResult() error = %v", err)
	}
	if len(changes) != 10 {
		t.Fatalf("changes = %d, want 10", len(changes))
	}
	for index, change := range changes {
		if change.System() != domain.RatingSystemGlicko2 || !change.HasUncertaintyState() {
			t.Fatalf("change %d system/state = %q/%v", index, change.System(), change.HasUncertaintyState())
		}
		if change.DeviationAfter() >= change.DeviationBefore() {
			t.Fatalf("change %d deviation = %v -> %v, want decrease", index, change.DeviationBefore(), change.DeviationAfter())
		}
		if change.VolatilityAfter() <= 0 {
			t.Fatalf("change %d volatility = %v, want positive", index, change.VolatilityAfter())
		}
		if index < 5 && change.Delta() <= 0 {
			t.Fatalf("team A change %d delta = %v, want positive", index, change.Delta())
		}
		if index >= 5 && change.Delta() >= 0 {
			t.Fatalf("team B change %d delta = %v, want negative", index, change.Delta())
		}
	}

	firstPlayer, err := playerService.GetPlayer(ctx, teamA[0])
	if err != nil {
		t.Fatal(err)
	}
	if firstPlayer.RatingDeviation() != changes[0].DeviationAfter() ||
		firstPlayer.RatingVolatility() != changes[0].VolatilityAfter() {
		t.Fatalf("stored state = %+v, change = %+v", firstPlayer.RatingState(), changes[0])
	}
	duplicate, err := ratingService.RecordMatchResult(ctx, command)
	if err != nil {
		t.Fatalf("duplicate RecordMatchResult() error = %v", err)
	}
	if len(duplicate) != len(changes) || duplicate[0].After() != changes[0].After() ||
		duplicate[0].DeviationAfter() != changes[0].DeviationAfter() {
		t.Fatalf("duplicate result = %+v, want original %+v", duplicate, changes)
	}
	secondPlayer, err := playerService.GetPlayer(ctx, teamA[0])
	if err != nil {
		t.Fatal(err)
	}
	if secondPlayer.RatingState() != firstPlayer.RatingState() {
		t.Fatalf("duplicate changed state from %+v to %+v", firstPlayer.RatingState(), secondPlayer.RatingState())
	}
}

func createRatedTeam(
	t *testing.T,
	ctx context.Context,
	service *application.Service,
	prefix string,
	rating float64,
) []string {
	t.Helper()
	playerIDs := make([]string, 0, 5)
	for index := 1; index <= 5; index++ {
		playerID := fmt.Sprintf("%s-%d", prefix, index)
		_, err := service.CreatePlayer(ctx, application.CreatePlayerCommand{
			ID:             playerID,
			Name:           playerID,
			InitialRating:  rating,
			PreferredRoles: []domain.Role{domain.RoleCore},
			HomeRegion:     "hongkong",
			RegionLatency:  map[string]int{"hongkong": 30},
			BehaviorScore:  90,
		})
		if err != nil {
			t.Fatalf("CreatePlayer(%s) error = %v", playerID, err)
		}
		playerIDs = append(playerIDs, playerID)
	}
	return playerIDs
}
