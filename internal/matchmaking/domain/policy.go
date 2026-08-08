package domain

import (
	"errors"
	"math"
	"strings"
	"time"
)

var ErrInvalidPolicy = errors.New("invalid match policy")

type TeamAlgorithm string

const (
	TeamAlgorithmGreedy TeamAlgorithm = "greedy"
	TeamAlgorithmBeam   TeamAlgorithm = "beam"
)

type MatchPolicy struct {
	Version string

	TeamSize       int
	CandidateLimit int
	TeamAlgorithm  TeamAlgorithm
	BeamWidth      int

	SkillWeight   float64
	RoleWeight    float64
	LatencyWeight float64
	PartyWeight   float64
	WaitWeight    float64

	InitialRatingRange        float64
	MaxRatingRange            float64
	RatingExpansionPerSecond  float64
	InitialLatencyMS          int
	MaxLatencyMS              int
	LatencyExpansionPerSecond float64
	RoleRelaxationAfter       time.Duration
	RoleRelaxationPerSecond   float64
	MaxNonPreferredRoleScore  float64
	MinQualityScore           float64
	ReservationTTL            time.Duration
	TicketTTL                 time.Duration
}

func DefaultPolicy() MatchPolicy {
	return MatchPolicy{
		Version:                   "v1-greedy",
		TeamSize:                  5,
		CandidateLimit:            30,
		TeamAlgorithm:             TeamAlgorithmGreedy,
		BeamWidth:                 64,
		SkillWeight:               0.40,
		RoleWeight:                0.20,
		LatencyWeight:             0.20,
		PartyWeight:               0.10,
		WaitWeight:                0.10,
		InitialRatingRange:        100,
		MaxRatingRange:            400,
		RatingExpansionPerSecond:  2,
		InitialLatencyMS:          120,
		MaxLatencyMS:              250,
		LatencyExpansionPerSecond: 1,
		RoleRelaxationAfter:       60 * time.Second,
		RoleRelaxationPerSecond:   0.5,
		MaxNonPreferredRoleScore:  50,
		MinQualityScore:           60,
		ReservationTTL:            15 * time.Second,
		TicketTTL:                 10 * time.Minute,
	}
}

func BeamPolicy() MatchPolicy {
	policy := DefaultPolicy()
	policy.Version = "v2-beam"
	policy.TeamAlgorithm = TeamAlgorithmBeam
	return policy
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
	if (p.TeamAlgorithm != TeamAlgorithmGreedy && p.TeamAlgorithm != TeamAlgorithmBeam) || p.BeamWidth < 1 || p.BeamWidth > 1024 {
		return ErrInvalidPolicy
	}
	if p.InitialRatingRange < 0 || p.MaxRatingRange < p.InitialRatingRange || invalidNonNegativeFloat(p.RatingExpansionPerSecond) {
		return ErrInvalidPolicy
	}
	if p.InitialLatencyMS <= 0 || p.MaxLatencyMS < p.InitialLatencyMS || invalidNonNegativeFloat(p.LatencyExpansionPerSecond) {
		return ErrInvalidPolicy
	}
	if p.RoleRelaxationAfter < 0 || invalidNonNegativeFloat(p.RoleRelaxationPerSecond) ||
		math.IsNaN(p.MaxNonPreferredRoleScore) || math.IsInf(p.MaxNonPreferredRoleScore, 0) ||
		p.MaxNonPreferredRoleScore < 0 || p.MaxNonPreferredRoleScore > 100 {
		return ErrInvalidPolicy
	}
	if p.MinQualityScore < 0 || p.MinQualityScore > 100 {
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

func (p MatchPolicy) LatencyLimit(wait time.Duration) int {
	if wait < 0 {
		wait = 0
	}
	limit := float64(p.InitialLatencyMS) + wait.Seconds()*p.LatencyExpansionPerSecond
	if limit >= float64(p.MaxLatencyMS) {
		return p.MaxLatencyMS
	}
	return int(limit)
}

func (p MatchPolicy) NonPreferredRoleScore(wait time.Duration) float64 {
	if wait <= p.RoleRelaxationAfter {
		return 0
	}
	score := (wait - p.RoleRelaxationAfter).Seconds() * p.RoleRelaxationPerSecond
	if score > p.MaxNonPreferredRoleScore {
		return p.MaxNonPreferredRoleScore
	}
	return score
}

func invalidNonNegativeFloat(value float64) bool {
	return math.IsNaN(value) || math.IsInf(value, 0) || value < 0
}
