package application

import (
	"context"
	"errors"
	"testing"

	simulationdomain "github.com/ccKccK-JF/MatchMind/internal/simulation/domain"
)

type modeTestMatchGateway struct {
	snapshot       MatchSnapshot
	started        int
	completed      int
	completedMatch simulationdomain.Result
}

func (gateway *modeTestMatchGateway) GetMatch(context.Context, string) (MatchSnapshot, error) {
	return gateway.snapshot, nil
}

func (gateway *modeTestMatchGateway) StartMatch(context.Context, string) error {
	gateway.started++
	return nil
}

func (gateway *modeTestMatchGateway) CompleteMatch(_ context.Context, result simulationdomain.Result) error {
	gateway.completed++
	gateway.completedMatch = result
	return nil
}

type modeTestRatingGateway struct{ applied int }

func (gateway *modeTestRatingGateway) ApplyMatchResult(
	context.Context,
	string,
	[]string,
	[]string,
	simulationdomain.WinningTeam,
) error {
	gateway.applied++
	return nil
}

type modeTestResultStore struct {
	result *simulationdomain.Result
}

func (store *modeTestResultStore) Get(context.Context, string) (simulationdomain.Result, error) {
	if store.result == nil {
		return simulationdomain.Result{}, ErrResultNotFound
	}
	return *store.result, nil
}

func (store *modeTestResultStore) Save(_ context.Context, result simulationdomain.Result) error {
	store.result = &result
	return nil
}

func TestSimulationUpdatesRatingOnlyForRankedMode(t *testing.T) {
	for _, test := range []struct {
		mode          string
		expectedApply int
	}{
		{mode: "ranked_5v5", expectedApply: 1},
		{mode: "normal_5v5", expectedApply: 0},
		{mode: "training_5v5", expectedApply: 0},
	} {
		t.Run(test.mode, func(t *testing.T) {
			matches := &modeTestMatchGateway{snapshot: validModeTestSnapshot(test.mode)}
			ratings := &modeTestRatingGateway{}
			store := &modeTestResultStore{}
			service := NewService(matches, ratings, store, simulationdomain.NewSimulator())
			result, err := service.SimulateMatch(context.Background(), "match-1", 42)
			if err != nil {
				t.Fatal(err)
			}
			if ratings.applied != test.expectedApply || matches.started != 1 || matches.completed != 1 ||
				store.result == nil || matches.completedMatch.MatchID != result.MatchID {
				t.Fatalf("mode effects ratings/start/complete/store = %d/%d/%d/%v", ratings.applied, matches.started, matches.completed, store.result)
			}
		})
	}
}

func TestSimulationRejectsUnsupportedMode(t *testing.T) {
	matches := &modeTestMatchGateway{snapshot: validModeTestSnapshot("custom_5v5")}
	service := NewService(matches, &modeTestRatingGateway{}, &modeTestResultStore{}, simulationdomain.NewSimulator())
	_, err := service.SimulateMatch(context.Background(), "match-1", 42)
	if !errors.Is(err, simulationdomain.ErrInvalidSimulation) {
		t.Fatalf("SimulateMatch() error = %v", err)
	}
	if matches.started != 0 || matches.completed != 0 {
		t.Fatal("unsupported mode changed match state")
	}
}

func validModeTestSnapshot(mode string) MatchSnapshot {
	return MatchSnapshot{
		ID: "match-1", Mode: mode, State: "READY",
		TeamAPlayerIDs: []string{"a"}, TeamBPlayerIDs: []string{"b"},
		TeamAAverageRating: 1500, TeamBAverageRating: 1500, PredictedWinRateA: 0.5,
		RoleScore: 100, LatencyScore: 100, PartyScore: 100,
		TeamAHeroProficiency: 50, TeamBHeroProficiency: 50,
		TeamABehaviorScore: 100, TeamBBehaviorScore: 100,
	}
}
