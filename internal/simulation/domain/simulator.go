package domain

import (
	"errors"
	"math"
	"math/rand"
)

var ErrInvalidSimulation = errors.New("invalid simulation input")

type WinningTeam string

const (
	WinningTeamA WinningTeam = "A"
	WinningTeamB WinningTeam = "B"
)

type Input struct {
	MatchID           string
	RandomSeed        int64
	RatingA           float64
	RatingB           float64
	PredictedWinRateA float64
	RoleScore         float64
	LatencyScore      float64
	PartyScore        float64
	HeroProficiencyA  float64
	HeroProficiencyB  float64
	BehaviorScoreA    float64
	BehaviorScoreB    float64
}

type Result struct {
	MatchID            string
	RandomSeed         int64
	WinningTeam        WinningTeam
	DurationSeconds    int
	ScoreA             int
	ScoreB             int
	MaxAdvantage       float64
	HasAFK             bool
	Surrendered        bool
	OneSided           bool
	ActualQualityScore float64
}

type Simulator struct{}

func NewSimulator() Simulator { return Simulator{} }

func (Simulator) Simulate(input Input) (Result, error) {
	if input.MatchID == "" || input.RatingA <= 0 || input.RatingB <= 0 ||
		input.PredictedWinRateA < 0 || input.PredictedWinRateA > 1 ||
		!validScore(input.RoleScore) || !validScore(input.LatencyScore) || !validScore(input.PartyScore) ||
		!validScore(input.HeroProficiencyA) || !validScore(input.HeroProficiencyB) ||
		!validScore(input.BehaviorScoreA) || !validScore(input.BehaviorScoreB) {
		return Result{}, ErrInvalidSimulation
	}
	random := rand.New(rand.NewSource(input.RandomSeed))
	result := Result{MatchID: input.MatchID, RandomSeed: input.RandomSeed}

	effectiveRatingA := input.RatingA + (input.HeroProficiencyA-50)*1.5 + (input.BehaviorScoreA-50)*0.5
	effectiveRatingB := input.RatingB + (input.HeroProficiencyB-50)*1.5 + (input.BehaviorScoreB-50)*0.5
	effectivePrediction := 1 / (1 + math.Pow(10, (effectiveRatingB-effectiveRatingA)/400))
	winProbabilityA := clamp01(input.PredictedWinRateA*0.65 + effectivePrediction*0.35)
	if random.Float64() < winProbabilityA {
		result.WinningTeam = WinningTeamA
	} else {
		result.WinningTeam = WinningTeamB
	}

	closeness := 1 - math.Abs(winProbabilityA-0.5)*2
	closeness = clamp01(closeness)
	durationJitter := random.Intn(181) - 90
	result.DurationSeconds = clampInt(600+int(900*closeness)+durationJitter, 600, 1500)

	baseScore := 10 + random.Intn(11)
	strengthMargin := int(math.Abs(winProbabilityA-0.5)*24) + 1 + random.Intn(6)
	if result.WinningTeam == WinningTeamA {
		result.ScoreA = baseScore + strengthMargin
		result.ScoreB = baseScore
	} else {
		result.ScoreA = baseScore
		result.ScoreB = baseScore + strengthMargin
	}
	result.MaxAdvantage = float64(strengthMargin*750 + random.Intn(1501))

	stabilityPenalty := (100-clampScore(input.LatencyScore))/100*0.08 + (100-clampScore(input.RoleScore))/100*0.04
	stabilityPenalty += (100 - (input.BehaviorScoreA+input.BehaviorScoreB)/2) / 100 * 0.18
	result.HasAFK = random.Float64() < 0.02+stabilityPenalty
	result.OneSided = strengthMargin >= 10 || result.MaxAdvantage >= 9000
	result.Surrendered = result.OneSided && result.DurationSeconds >= 720 && random.Float64() < 0.45
	if result.Surrendered {
		result.DurationSeconds = clampInt(result.DurationSeconds-180, 600, 1500)
	}

	processScore := closeness * 100
	if result.OneSided {
		processScore -= 25
	}
	if result.HasAFK {
		processScore -= 35
	}
	if result.Surrendered {
		processScore -= 10
	}
	heroQuality := (input.HeroProficiencyA + input.HeroProficiencyB) / 2
	inputQuality := (clampScore(input.RoleScore) + clampScore(input.LatencyScore) + clampScore(input.PartyScore) + heroQuality) / 4
	result.ActualQualityScore = clampScore(processScore*0.6 + inputQuality*0.4)
	return result, nil
}

func validScore(value float64) bool {
	return !math.IsNaN(value) && !math.IsInf(value, 0) && value >= 0 && value <= 100
}

func clampScore(value float64) float64 {
	if value < 0 {
		return 0
	}
	if value > 100 {
		return 100
	}
	return value
}

func clamp01(value float64) float64 { return clampScore(value*100) / 100 }

func clampInt(value, minimum, maximum int) int {
	if value < minimum {
		return minimum
	}
	if value > maximum {
		return maximum
	}
	return value
}
