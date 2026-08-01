package elo

import (
	"errors"
	"math"
)

const ratingScale = 400.0

var ErrInvalidInput = errors.New("invalid Elo input")

type Calculator struct {
	kFactor float64
}

type PairResult struct {
	RatingA   float64
	RatingB   float64
	DeltaA    float64
	DeltaB    float64
	ExpectedA float64
	ExpectedB float64
}

func NewCalculator(kFactor float64) (Calculator, error) {
	if !finite(kFactor) || kFactor <= 0 {
		return Calculator{}, ErrInvalidInput
	}
	return Calculator{kFactor: kFactor}, nil
}

func (c Calculator) KFactor() float64 {
	return c.kFactor
}

// ExpectedScore returns the expected score for ratingA against ratingB.
func (c Calculator) ExpectedScore(ratingA, ratingB float64) (float64, error) {
	if !validRating(ratingA) || !validRating(ratingB) {
		return 0, ErrInvalidInput
	}
	return 1 / (1 + math.Pow(10, (ratingB-ratingA)/ratingScale)), nil
}

// UpdatePair updates two ratings. scoreA must be 1 for a win, 0.5 for a draw,
// or 0 for a loss. The update is zero-sum before optional display rounding.
func (c Calculator) UpdatePair(ratingA, ratingB, scoreA float64) (PairResult, error) {
	if scoreA != 0 && scoreA != 0.5 && scoreA != 1 {
		return PairResult{}, ErrInvalidInput
	}
	expectedA, err := c.ExpectedScore(ratingA, ratingB)
	if err != nil {
		return PairResult{}, err
	}
	expectedB := 1 - expectedA
	deltaA := c.kFactor * (scoreA - expectedA)
	deltaB := c.kFactor * ((1 - scoreA) - expectedB)
	return PairResult{
		RatingA:   ratingA + deltaA,
		RatingB:   ratingB + deltaB,
		DeltaA:    deltaA,
		DeltaB:    deltaB,
		ExpectedA: expectedA,
		ExpectedB: expectedB,
	}, nil
}

func validRating(rating float64) bool {
	return finite(rating) && rating > 0
}

func finite(value float64) bool {
	return !math.IsNaN(value) && !math.IsInf(value, 0)
}
