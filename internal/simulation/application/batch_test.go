package application

import (
	"context"
	"errors"
	"reflect"
	"testing"

	simulationdomain "github.com/ccKccK-JF/MatchMind/internal/simulation/domain"
)

func TestSimulateBatchIsDeterministicAcrossConcurrency(t *testing.T) {
	service := NewService(nil, nil, nil, simulationdomain.NewSimulator())
	inputs := make([]simulationdomain.Input, 1000)
	for index := range inputs {
		inputs[index] = simulationdomain.Input{
			MatchID: "offline-case", RandomSeed: int64(index + 1),
			RatingA: 1520, RatingB: 1480, PredictedWinRateA: .557,
			RoleScore: 90, LatencyScore: 85, PartyScore: 100,
		}
	}
	sequential, err := service.SimulateBatch(context.Background(), inputs, 1)
	if err != nil {
		t.Fatal(err)
	}
	concurrent, err := service.SimulateBatch(context.Background(), inputs, 16)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(sequential, concurrent) {
		t.Fatal("batch result changed with concurrency")
	}
	if concurrent.SimulationCount != len(inputs) || concurrent.TeamAWinRate <= .5 || concurrent.AverageActualQuality <= 0 {
		t.Fatalf("batch summary = %#v", concurrent)
	}
}

func TestSimulateBatchRejectsInvalidInputAndCancellation(t *testing.T) {
	service := NewService(nil, nil, nil, simulationdomain.NewSimulator())
	if _, err := service.SimulateBatch(context.Background(), nil, 1); !errors.Is(err, simulationdomain.ErrInvalidSimulation) {
		t.Fatalf("empty SimulateBatch() error = %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := service.SimulateBatch(ctx, []simulationdomain.Input{{
		MatchID: "case", RatingA: 1500, RatingB: 1500, PredictedWinRateA: .5,
	}}, 1)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled SimulateBatch() error = %v", err)
	}
}
