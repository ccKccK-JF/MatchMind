package glicko2

import (
	"errors"
	"math"
)

const (
	ratingCenter         = 1500.0
	ratingScale          = 173.7178
	convergenceTolerance = 0.000001
	maxIterations        = 100
)

var ErrInvalidInput = errors.New("invalid Glicko-2 input")

type Rating struct {
	Rating     float64
	Deviation  float64
	Volatility float64
}

type Result struct {
	Opponent Rating
	Score    float64
}

type Calculator struct {
	tau float64
}

func NewCalculator(tau float64) (Calculator, error) {
	if !finite(tau) || tau < 0.3 || tau > 1.2 {
		return Calculator{}, ErrInvalidInput
	}
	return Calculator{tau: tau}, nil
}

func (c Calculator) Tau() float64 {
	return c.tau
}

func (c Calculator) Update(player Rating, results []Result) (Rating, error) {
	if !validRating(player) || len(results) == 0 {
		return Rating{}, ErrInvalidInput
	}
	mu := (player.Rating - ratingCenter) / ratingScale
	phi := player.Deviation / ratingScale
	varianceInverse := 0.0
	scoreSum := 0.0
	for _, result := range results {
		if !validRating(result.Opponent) || (result.Score != 0 && result.Score != 0.5 && result.Score != 1) {
			return Rating{}, ErrInvalidInput
		}
		opponentMu := (result.Opponent.Rating - ratingCenter) / ratingScale
		opponentPhi := result.Opponent.Deviation / ratingScale
		impact := g(opponentPhi)
		expected := expectation(mu, opponentMu, opponentPhi)
		varianceInverse += impact * impact * expected * (1 - expected)
		scoreSum += impact * (result.Score - expected)
	}
	if varianceInverse <= 0 || !finite(varianceInverse) {
		return Rating{}, ErrInvalidInput
	}
	variance := 1 / varianceInverse
	delta := variance * scoreSum
	newVolatility, err := c.updateVolatility(phi, player.Volatility, delta, variance)
	if err != nil {
		return Rating{}, err
	}
	prePeriodDeviation := math.Sqrt(phi*phi + newVolatility*newVolatility)
	newDeviation := 1 / math.Sqrt(1/(prePeriodDeviation*prePeriodDeviation)+1/variance)
	newMu := mu + newDeviation*newDeviation*scoreSum
	updated := Rating{
		Rating:     ratingCenter + ratingScale*newMu,
		Deviation:  ratingScale * newDeviation,
		Volatility: newVolatility,
	}
	if !validRating(updated) {
		return Rating{}, ErrInvalidInput
	}
	return updated, nil
}

func (c Calculator) updateVolatility(phi, volatility, delta, variance float64) (float64, error) {
	a := math.Log(volatility * volatility)
	objective := func(x float64) float64 {
		exponential := math.Exp(x)
		denominator := phi*phi + variance + exponential
		return exponential*(delta*delta-phi*phi-variance-exponential)/(2*denominator*denominator) -
			(x-a)/(c.tau*c.tau)
	}
	left := a
	var right float64
	if delta*delta > phi*phi+variance {
		right = math.Log(delta*delta - phi*phi - variance)
	} else {
		found := false
		for k := 1; k <= maxIterations; k++ {
			right = a - float64(k)*c.tau
			if objective(right) >= 0 {
				found = true
				break
			}
		}
		if !found {
			return 0, ErrInvalidInput
		}
	}
	fLeft := objective(left)
	fRight := objective(right)
	for iteration := 0; math.Abs(right-left) > convergenceTolerance; iteration++ {
		if iteration >= maxIterations || !finite(fLeft) || !finite(fRight) || fRight == fLeft {
			return 0, ErrInvalidInput
		}
		middle := left + (left-right)*fLeft/(fRight-fLeft)
		fMiddle := objective(middle)
		if fMiddle*fRight <= 0 {
			left = right
			fLeft = fRight
		} else {
			fLeft /= 2
		}
		right = middle
		fRight = fMiddle
	}
	result := math.Exp(left / 2)
	if !finite(result) || result <= 0 {
		return 0, ErrInvalidInput
	}
	return result, nil
}

func g(deviation float64) float64 {
	return 1 / math.Sqrt(1+3*deviation*deviation/(math.Pi*math.Pi))
}

func expectation(player, opponent, opponentDeviation float64) float64 {
	return 1 / (1 + math.Exp(-g(opponentDeviation)*(player-opponent)))
}

func validRating(rating Rating) bool {
	return finite(rating.Rating) && rating.Rating > 0 &&
		finite(rating.Deviation) && rating.Deviation > 0 &&
		finite(rating.Volatility) && rating.Volatility > 0
}

func finite(value float64) bool {
	return !math.IsNaN(value) && !math.IsInf(value, 0)
}
