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

func TestPolicyDerivesGameModeRules(t *testing.T) {
	base := BeamPolicy()
	ranked, err := base.ForMode("ranked_5v5")
	if err != nil || ranked != base {
		t.Fatalf("ranked policy = %+v, %v", ranked, err)
	}
	normal, err := base.ForMode("normal_5v5")
	if err != nil {
		t.Fatal(err)
	}
	if normal.InitialRatingRange <= base.InitialRatingRange ||
		normal.RatingExpansionPerSecond <= base.RatingExpansionPerSecond ||
		normal.RoleRelaxationAfter >= base.RoleRelaxationAfter ||
		normal.MinQualityScore >= base.MinQualityScore || normal.Version != base.Version {
		t.Fatalf("normal policy was not speed-oriented: %+v", normal)
	}
	training, err := base.ForMode("training_5v5")
	if err != nil {
		t.Fatal(err)
	}
	if training.InitialRatingRange != training.MaxRatingRange || training.MinQualityScore != 0 ||
		training.MaxLatencyMS != 1000 || training.RoleRelaxationAfter != 0 || training.Version != base.Version {
		t.Fatalf("training policy was not sandbox-oriented: %+v", training)
	}
	if _, err := base.ForMode("custom"); !errors.Is(err, ErrInvalidPolicy) {
		t.Fatalf("unsupported mode policy error = %v", err)
	}
}
