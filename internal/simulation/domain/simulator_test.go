package domain

import (
	"reflect"
	"testing"
)

func TestSimulatorIsReproducible(t *testing.T) {
	simulator := NewSimulator()
	input := Input{
		MatchID: "match-1", RandomSeed: 42, RatingA: 1600, RatingB: 1500,
		PredictedWinRateA: 0.64, RoleScore: 90, LatencyScore: 85, PartyScore: 100,
	}
	first, err := simulator.Simulate(input)
	if err != nil {
		t.Fatal(err)
	}
	second, err := simulator.Simulate(input)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(first, second) {
		t.Fatalf("same seed produced different results:\n%+v\n%+v", first, second)
	}
}

func TestHigherRatedTeamWinsMoreOften(t *testing.T) {
	simulator := NewSimulator()
	const simulations = 10000
	winsA := 0
	for seed := range simulations {
		result, err := simulator.Simulate(Input{
			MatchID: "match-1", RandomSeed: int64(seed), RatingA: 1800, RatingB: 1500,
			PredictedWinRateA: 0.849, RoleScore: 90, LatencyScore: 90, PartyScore: 100,
		})
		if err != nil {
			t.Fatal(err)
		}
		if result.WinningTeam == WinningTeamA {
			winsA++
		}
	}
	winRate := float64(winsA) / simulations
	if winRate < 0.80 || winRate > 0.90 {
		t.Fatalf("team A win rate = %.4f, want roughly 0.849", winRate)
	}
}

func TestSimulatorOutputRanges(t *testing.T) {
	result, err := NewSimulator().Simulate(Input{
		MatchID: "match-1", RandomSeed: 7, RatingA: 1500, RatingB: 1500,
		PredictedWinRateA: 0.5, RoleScore: 100, LatencyScore: 90, PartyScore: 100,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.DurationSeconds < 600 || result.DurationSeconds > 1500 {
		t.Fatalf("duration = %d", result.DurationSeconds)
	}
	if result.ScoreA == result.ScoreB {
		t.Fatalf("scores should not tie: %d/%d", result.ScoreA, result.ScoreB)
	}
	if result.ActualQualityScore < 0 || result.ActualQualityScore > 100 {
		t.Fatalf("actual quality = %v", result.ActualQualityScore)
	}
}

func TestHeroProficiencyChangesWinRate(t *testing.T) {
	simulator := NewSimulator()
	const simulations = 10000
	winsA := 0
	for seed := range simulations {
		result, err := simulator.Simulate(Input{
			MatchID: "match-hero", RandomSeed: int64(seed), RatingA: 1500, RatingB: 1500,
			PredictedWinRateA: 0.5, RoleScore: 100, LatencyScore: 100, PartyScore: 100,
			HeroProficiencyA: 100, HeroProficiencyB: 0, BehaviorScoreA: 100, BehaviorScoreB: 100,
		})
		if err != nil {
			t.Fatal(err)
		}
		if result.WinningTeam == WinningTeamA {
			winsA++
		}
	}
	winRate := float64(winsA) / simulations
	if winRate < 0.56 || winRate > 0.62 {
		t.Fatalf("high-proficiency team A win rate = %.4f", winRate)
	}
}

func TestLowBehaviorScoreIncreasesAFKRate(t *testing.T) {
	simulator := NewSimulator()
	const simulations = 10000
	lowBehaviorAFKs, highBehaviorAFKs := 0, 0
	for seed := range simulations {
		base := Input{
			MatchID: "match-behavior", RandomSeed: int64(seed), RatingA: 1500, RatingB: 1500,
			PredictedWinRateA: 0.5, RoleScore: 100, LatencyScore: 100, PartyScore: 100,
			HeroProficiencyA: 50, HeroProficiencyB: 50,
		}
		low := base
		low.BehaviorScoreA, low.BehaviorScoreB = 0, 0
		high := base
		high.BehaviorScoreA, high.BehaviorScoreB = 100, 100
		lowResult, err := simulator.Simulate(low)
		if err != nil {
			t.Fatal(err)
		}
		highResult, err := simulator.Simulate(high)
		if err != nil {
			t.Fatal(err)
		}
		if lowResult.HasAFK {
			lowBehaviorAFKs++
		}
		if highResult.HasAFK {
			highBehaviorAFKs++
		}
	}
	if lowBehaviorAFKs < highBehaviorAFKs+1200 {
		t.Fatalf("AFK counts low/high behavior = %d/%d", lowBehaviorAFKs, highBehaviorAFKs)
	}
}
