package glicko2

import (
	"errors"
	"math"
	"testing"
)

func TestCalculatorMatchesGlickmanPublishedExample(t *testing.T) {
	calculator, err := NewCalculator(0.5)
	if err != nil {
		t.Fatal(err)
	}
	updated, err := calculator.Update(Rating{Rating: 1500, Deviation: 200, Volatility: 0.06}, []Result{
		{Opponent: Rating{Rating: 1400, Deviation: 30, Volatility: 0.06}, Score: 1},
		{Opponent: Rating{Rating: 1550, Deviation: 100, Volatility: 0.06}, Score: 0},
		{Opponent: Rating{Rating: 1700, Deviation: 300, Volatility: 0.06}, Score: 0},
	})
	if err != nil {
		t.Fatal(err)
	}
	assertClose(t, updated.Rating, 1464.06, 0.01)
	assertClose(t, updated.Deviation, 151.52, 0.01)
	assertClose(t, updated.Volatility, 0.05999, 0.00001)
}

func TestCalculatorRejectsInvalidInputs(t *testing.T) {
	if _, err := NewCalculator(0.2); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("NewCalculator() error = %v", err)
	}
	calculator, _ := NewCalculator(0.5)
	for _, test := range []struct {
		player  Rating
		results []Result
	}{
		{player: Rating{Rating: 1500, Deviation: 200, Volatility: 0.06}},
		{player: Rating{Rating: -1, Deviation: 200, Volatility: 0.06}, results: []Result{{Opponent: Rating{Rating: 1500, Deviation: 200, Volatility: 0.06}, Score: 1}}},
		{player: Rating{Rating: 1500, Deviation: 200, Volatility: 0.06}, results: []Result{{Opponent: Rating{Rating: 1500, Deviation: 200, Volatility: 0.06}, Score: 0.25}}},
	} {
		if _, err := calculator.Update(test.player, test.results); !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("Update(%+v) error = %v", test, err)
		}
	}
}

func assertClose(t *testing.T, actual, expected, tolerance float64) {
	t.Helper()
	if math.Abs(actual-expected) > tolerance {
		t.Fatalf("value = %.12f, want %.12f +/- %.12f", actual, expected, tolerance)
	}
}
