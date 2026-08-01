package domain

import (
	"errors"
	"math"
	"strings"
	"time"
)

var ErrInvalidPolicy = errors.New("invalid match policy")

type MatchPolicy struct {
	Version string

	TeamSize       int
	CandidateLimit int

	SkillWeight   float64
	RoleWeight    float64
	LatencyWeight float64
	PartyWeight   float64
	WaitWeight    float64

	InitialRatingRange       float64
	MaxRatingRange           float64
	RatingExpansionPerSecond float64
	MaxLatencyMS             int
	MinQualityScore          float64
	ReservationTTL           time.Duration
	TicketTTL                time.Duration
}

func DefaultPolicy() MatchPolicy {
	return MatchPolicy{
		Version:                  "v1",
		TeamSize:                 5,
		CandidateLimit:           30,
		SkillWeight:              0.40,
		RoleWeight:               0.20,
		LatencyWeight:            0.20,
		PartyWeight:              0.10,
		WaitWeight:               0.10,
		InitialRatingRange:       100,
		MaxRatingRange:           400,
		RatingExpansionPerSecond: 2,
		MaxLatencyMS:             250,
		MinQualityScore:          60,
		ReservationTTL:           15 * time.Second,
		TicketTTL:                10 * time.Minute,
	}
}

func (p MatchPolicy) Validate() error {
	weights := []float64{p.SkillWeight, p.RoleWeight, p.LatencyWeight, p.PartyWeight, p.WaitWeight}
	var total float64
	for _, weight := range weights {
		if math.IsNaN(weight) || math.IsInf(weight, 0) || weight < 0 || weight > 1 {
			return ErrInvalidPolicy
		}
		total += weight
	}
	if math.Abs(total-1) > 1e-9 {
		return ErrInvalidPolicy
	}
	if strings.TrimSpace(p.Version) == "" || p.TeamSize != 5 || p.CandidateLimit < p.TeamSize*2 {
		return ErrInvalidPolicy
	}
	if p.InitialRatingRange < 0 || p.MaxRatingRange < p.InitialRatingRange || p.RatingExpansionPerSecond < 0 {
		return ErrInvalidPolicy
	}
	if p.MaxLatencyMS <= 0 || p.MinQualityScore < 0 || p.MinQualityScore > 100 {
		return ErrInvalidPolicy
	}
	if p.ReservationTTL <= 0 || p.TicketTTL <= 0 {
		return ErrInvalidPolicy
	}
	return nil
}

func (p MatchPolicy) RatingRange(wait time.Duration) float64 {
	if wait < 0 {
		wait = 0
	}
	ratingRange := p.InitialRatingRange + wait.Seconds()*p.RatingExpansionPerSecond
	if ratingRange > p.MaxRatingRange {
		return p.MaxRatingRange
	}
	return ratingRange
}
