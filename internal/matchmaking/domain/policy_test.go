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
	if got := policy.RatingRange(30 * time.Second); got != 160 {
		t.Fatalf("RatingRange(30s) = %v, want 160", got)
	}
	if got := policy.RatingRange(time.Hour); got != policy.MaxRatingRange {
		t.Fatalf("RatingRange(1h) = %v, want max %v", got, policy.MaxRatingRange)
	}
}

func TestPolicyRejectsInvalidWeights(t *testing.T) {
	policy := DefaultPolicy()
	policy.SkillWeight = 0.5
	if err := policy.Validate(); !errors.Is(err, ErrInvalidPolicy) {
		t.Fatalf("Validate() error = %v, want ErrInvalidPolicy", err)
	}
}
