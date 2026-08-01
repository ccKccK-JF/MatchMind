package elo

import (
	"errors"
	"math"
	"testing"
)

func TestExpectedScore(t *testing.T) {
	calculator, err := NewCalculator(32)
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name          string
		ratingA       float64
		ratingB       float64
		wantExpectedA float64
	}{
		{name: "equal ratings", ratingA: 1500, ratingB: 1500, wantExpectedA: 0.5},
		{name: "four hundred higher", ratingA: 1900, ratingB: 1500, wantExpectedA: 10.0 / 11.0},
		{name: "four hundred lower", ratingA: 1500, ratingB: 1900, wantExpectedA: 1.0 / 11.0},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := calculator.ExpectedScore(test.ratingA, test.ratingB)
			if err != nil {
				t.Fatalf("ExpectedScore() error = %v", err)
			}
			if math.Abs(got-test.wantExpectedA) > 1e-12 {
				t.Fatalf("ExpectedScore() = %.12f, want %.12f", got, test.wantExpectedA)
			}
		})
	}
}

func TestUpdatePairIsZeroSum(t *testing.T) {
	calculator, _ := NewCalculator(32)
	result, err := calculator.UpdatePair(1600, 1500, 1)
	if err != nil {
		t.Fatalf("UpdatePair() error = %v", err)
	}
	if result.DeltaA <= 0 || result.DeltaB >= 0 {
		t.Fatalf("winner/loser deltas = %.4f/%.4f", result.DeltaA, result.DeltaB)
	}
	if math.Abs(result.DeltaA+result.DeltaB) > 1e-12 {
		t.Fatalf("rating update is not zero-sum: %.12f", result.DeltaA+result.DeltaB)
	}
}

func TestUpdatePairDraw(t *testing.T) {
	calculator, _ := NewCalculator(32)
	result, err := calculator.UpdatePair(1700, 1500, 0.5)
	if err != nil {
		t.Fatal(err)
	}
	if result.DeltaA >= 0 || result.DeltaB <= 0 {
		t.Fatalf("favored player should lose points on a draw: %.4f/%.4f", result.DeltaA, result.DeltaB)
	}
}

func TestCalculatorRejectsInvalidInput(t *testing.T) {
	if _, err := NewCalculator(0); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("NewCalculator() error = %v, want ErrInvalidInput", err)
	}
	calculator, _ := NewCalculator(32)
	if _, err := calculator.ExpectedScore(math.NaN(), 1500); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("ExpectedScore() error = %v, want ErrInvalidInput", err)
	}
	if _, err := calculator.UpdatePair(1500, 1500, 0.25); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("UpdatePair() error = %v, want ErrInvalidInput", err)
	}
}
