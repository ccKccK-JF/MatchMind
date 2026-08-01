package domain

import (
	"errors"
	"fmt"
	"math"
	"strings"
	"time"
)

var ErrInvalidRatingChange = errors.New("invalid rating change")

type RatingChange struct {
	playerID  string
	matchID   string
	before    float64
	after     float64
	reason    string
	createdAt time.Time
}

func NewRatingChange(playerID, matchID string, before, after float64, reason string, createdAt time.Time) (*RatingChange, error) {
	playerID = strings.TrimSpace(playerID)
	matchID = strings.TrimSpace(matchID)
	reason = strings.TrimSpace(reason)
	if playerID == "" || matchID == "" || reason == "" || createdAt.IsZero() {
		return nil, ErrInvalidRatingChange
	}
	if !validRatingValue(before) || !validRatingValue(after) {
		return nil, ErrInvalidRatingChange
	}
	return &RatingChange{
		playerID:  playerID,
		matchID:   matchID,
		before:    before,
		after:     after,
		reason:    reason,
		createdAt: createdAt.UTC(),
	}, nil
}

func (c *RatingChange) PlayerID() string     { return c.playerID }
func (c *RatingChange) MatchID() string      { return c.matchID }
func (c *RatingChange) Before() float64      { return c.before }
func (c *RatingChange) After() float64       { return c.after }
func (c *RatingChange) Delta() float64       { return c.after - c.before }
func (c *RatingChange) Reason() string       { return c.reason }
func (c *RatingChange) CreatedAt() time.Time { return c.createdAt }

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

func validRatingValue(rating float64) bool {
	return rating > 0 && !math.IsNaN(rating) && !math.IsInf(rating, 0)
}
