package domain

import (
	"errors"
	"testing"
	"time"
)

func TestDefaultPolicy(t *testing.T) {
	policy := DefaultPolicy()
	if err := policy.Validate(); err != nil {
		t.Fatalf("DefaultPolicy().Validate() error = %v", err)
	}
	if policy.TeamAlgorithm != TeamAlgorithmGreedy || BeamPolicy().TeamAlgorithm != TeamAlgorithmBeam {
		t.Fatalf("policy algorithms = %s/%s", policy.TeamAlgorithm, BeamPolicy().TeamAlgorithm)
	}
	if got := policy.RatingRange(30 * time.Second); got != 160 {
		t.Fatalf("RatingRange(30s) = %v, want 160", got)
	}
	if got := policy.RatingRange(time.Hour); got != policy.MaxRatingRange {
		t.Fatalf("RatingRange(1h) = %v, want max %v", got, policy.MaxRatingRange)
	}
	if got := policy.LatencyLimit(30 * time.Second); got != 150 {
		t.Fatalf("LatencyLimit(30s) = %v, want 150", got)
	}
	if got := policy.LatencyLimit(time.Hour); got != policy.MaxLatencyMS {
		t.Fatalf("LatencyLimit(1h) = %v, want max %v", got, policy.MaxLatencyMS)
	}
	if got := policy.NonPreferredRoleScore(60 * time.Second); got != 0 {
		t.Fatalf("NonPreferredRoleScore(60s) = %v, want 0", got)
	}
	if got := policy.NonPreferredRoleScore(90 * time.Second); got != 15 {
		t.Fatalf("NonPreferredRoleScore(90s) = %v, want 15", got)
	}
	if got := policy.NonPreferredRoleScore(time.Hour); got != policy.MaxNonPreferredRoleScore {
		t.Fatalf("NonPreferredRoleScore(1h) = %v, want max %v", got, policy.MaxNonPreferredRoleScore)
	}
}

func TestPolicyRejectsInvalidExpansionSettings(t *testing.T) {
	for name, mutate := range map[string]func(*MatchPolicy){
		"latency range":     func(policy *MatchPolicy) { policy.InitialLatencyMS = policy.MaxLatencyMS + 1 },
		"latency expansion": func(policy *MatchPolicy) { policy.LatencyExpansionPerSecond = -1 },
		"role delay":        func(policy *MatchPolicy) { policy.RoleRelaxationAfter = -time.Second },
		"role score":        func(policy *MatchPolicy) { policy.MaxNonPreferredRoleScore = 101 },
	} {
		t.Run(name, func(t *testing.T) {
			policy := DefaultPolicy()
			mutate(&policy)
			if err := policy.Validate(); !errors.Is(err, ErrInvalidPolicy) {
				t.Fatalf("Validate() error = %v, want ErrInvalidPolicy", err)
			}
		})
	}
}

func TestPolicyRejectsInvalidBeamWidth(t *testing.T) {
	policy := BeamPolicy()
	policy.BeamWidth = 0
	if err := policy.Validate(); !errors.Is(err, ErrInvalidPolicy) {
		t.Fatalf("Validate() error = %v, want ErrInvalidPolicy", err)
	}
}

func TestPolicyRejectsInvalidWeights(t *testing.T) {
	policy := DefaultPolicy()
	policy.SkillWeight = 0.5
	if err := policy.Validate(); !errors.Is(err, ErrInvalidPolicy) {
		t.Fatalf("Validate() error = %v, want ErrInvalidPolicy", err)
	}
}
