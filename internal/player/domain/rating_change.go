package domain

import (
	"errors"
	"fmt"
	"math"
	"strings"
	"time"
)

var ErrInvalidRatingChange = errors.New("invalid rating change")

type RatingSystem string

const (
	RatingSystemElo     RatingSystem = "elo"
	RatingSystemGlicko2 RatingSystem = "glicko2"
)

type RatingState struct {
	Rating     float64
	Deviation  float64
	Volatility float64
}

type NewRatingChangeParams struct {
	PlayerID  string
	MatchID   string
	Before    RatingState
	After     RatingState
	System    RatingSystem
	Reason    string
	CreatedAt time.Time
}

type RatingChange struct {
	playerID         string
	matchID          string
	before           float64
	after            float64
	deviationBefore  float64
	deviationAfter   float64
	volatilityBefore float64
	volatilityAfter  float64
	system           RatingSystem
	reason           string
	createdAt        time.Time
}

func NewRatingChange(playerID, matchID string, before, after float64, reason string, createdAt time.Time) (*RatingChange, error) {
	return NewRatingChangeWithState(NewRatingChangeParams{
		PlayerID: playerID, MatchID: matchID,
		Before: RatingState{Rating: before}, After: RatingState{Rating: after},
		System: RatingSystemElo, Reason: reason, CreatedAt: createdAt,
	})
}

func NewRatingChangeWithState(params NewRatingChangeParams) (*RatingChange, error) {
	params.PlayerID = strings.TrimSpace(params.PlayerID)
	params.MatchID = strings.TrimSpace(params.MatchID)
	params.Reason = strings.TrimSpace(params.Reason)
	if params.PlayerID == "" || params.MatchID == "" || params.Reason == "" || params.CreatedAt.IsZero() {
		return nil, ErrInvalidRatingChange
	}
	if params.System != RatingSystemElo && params.System != RatingSystemGlicko2 {
		return nil, ErrInvalidRatingChange
	}
	if !validRatingValue(params.Before.Rating) || !validRatingValue(params.After.Rating) {
		return nil, ErrInvalidRatingChange
	}
	hasUncertainty := params.Before.Deviation != 0 || params.After.Deviation != 0 ||
		params.Before.Volatility != 0 || params.After.Volatility != 0
	if hasUncertainty && (!validUncertaintyState(params.Before) || !validUncertaintyState(params.After)) {
		return nil, ErrInvalidRatingChange
	}
	return &RatingChange{
		playerID: params.PlayerID, matchID: params.MatchID,
		before: params.Before.Rating, after: params.After.Rating,
		deviationBefore: params.Before.Deviation, deviationAfter: params.After.Deviation,
		volatilityBefore: params.Before.Volatility, volatilityAfter: params.After.Volatility,
		system: params.System, reason: params.Reason, createdAt: params.CreatedAt.UTC(),
	}, nil
}

func (c *RatingChange) PlayerID() string          { return c.playerID }
func (c *RatingChange) MatchID() string           { return c.matchID }
func (c *RatingChange) Before() float64           { return c.before }
func (c *RatingChange) After() float64            { return c.after }
func (c *RatingChange) DeviationBefore() float64  { return c.deviationBefore }
func (c *RatingChange) DeviationAfter() float64   { return c.deviationAfter }
func (c *RatingChange) VolatilityBefore() float64 { return c.volatilityBefore }
func (c *RatingChange) VolatilityAfter() float64  { return c.volatilityAfter }
func (c *RatingChange) System() RatingSystem      { return c.system }
func (c *RatingChange) HasUncertaintyState() bool { return c.deviationBefore > 0 }
func (c *RatingChange) Delta() float64            { return c.after - c.before }
func (c *RatingChange) Reason() string            { return c.reason }
func (c *RatingChange) CreatedAt() time.Time      { return c.createdAt }

func (c *RatingChange) Clone() *RatingChange {
	if c == nil {
		return nil
	}
	clone := *c
	return &clone
}

func (p *Player) WithRating(rating float64) (*Player, error) {
	if !validRatingValue(rating) {
		return nil, fmt.Errorf("%w: rating must be finite and greater than zero", ErrInvalidPlayer)
	}
	clone := p.Clone()
	clone.rating = rating
	return clone, nil
}

func (p *Player) RatingState() RatingState {
	return RatingState{Rating: p.rating, Deviation: p.ratingDeviation, Volatility: p.ratingVolatility}
}

func (p *Player) WithRatingState(state RatingState) (*Player, error) {
	if !validUncertaintyState(state) {
		return nil, fmt.Errorf("%w: rating state must contain positive finite values", ErrInvalidPlayer)
	}
	clone := p.Clone()
	clone.rating = state.Rating
	clone.ratingDeviation = state.Deviation
	clone.ratingVolatility = state.Volatility
	return clone, nil
}

func validRatingValue(rating float64) bool {
	return rating > 0 && !math.IsNaN(rating) && !math.IsInf(rating, 0)
}

func validUncertaintyState(state RatingState) bool {
	return validRatingValue(state.Rating) && validRatingValue(state.Deviation) && validRatingValue(state.Volatility)
}
