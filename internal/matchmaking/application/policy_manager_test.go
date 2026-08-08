package application

import (
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/ccKccK-JF/MatchMind/internal/matchmaking/domain"
)

func TestPolicyManagerAssignsExperimentDeterministically(t *testing.T) {
	manager, err := NewPolicyManager([]domain.MatchPolicy{domain.DefaultPolicy(), domain.BeamPolicy()}, domain.DefaultPolicy().Version)
	if err != nil {
		t.Fatal(err)
	}
	experiment := PolicyExperiment{
		ID: "beam-v2", ControlVersion: domain.DefaultPolicy().Version,
		TreatmentVersion: domain.BeamPolicy().Version, TreatmentBasisPoints: 3000,
		AssignmentSalt: "stable-salt", StartedAt: time.Now(),
	}
	if err := manager.StartExperiment(experiment); err != nil {
		t.Fatal(err)
	}
	first := manager.SelectPolicy("player-1001")
	for range 100 {
		if next := manager.SelectPolicy("player-1001"); next != first {
			t.Fatalf("assignment changed: %#v -> %#v", first, next)
		}
	}
	treatments := 0
	for index := range 10000 {
		if manager.SelectPolicy(fmt.Sprintf("player-%d", index)).Variant == "treatment" {
			treatments++
		}
	}
	if treatments < 2800 || treatments > 3200 {
		t.Fatalf("treatment assignments = %d, want approximately 3000", treatments)
	}
	if err := manager.StopExperiment(experiment.ID); err != nil {
		t.Fatal(err)
	}
	selection := manager.SelectPolicy("player-1001")
	if selection.Variant != "default" || selection.Policy.Version != domain.DefaultPolicy().Version {
		t.Fatalf("selection after stop = %#v", selection)
	}
}

func TestPolicyManagerRejectsInvalidExperiment(t *testing.T) {
	manager, err := NewPolicyManager([]domain.MatchPolicy{domain.DefaultPolicy(), domain.BeamPolicy()}, domain.DefaultPolicy().Version)
	if err != nil {
		t.Fatal(err)
	}
	err = manager.StartExperiment(PolicyExperiment{
		ID: "bad", ControlVersion: domain.DefaultPolicy().Version,
		TreatmentVersion: "missing", TreatmentBasisPoints: 5000,
		AssignmentSalt: "salt", StartedAt: time.Now(),
	})
	if !errors.Is(err, ErrPolicyNotFound) {
		t.Fatalf("StartExperiment() error = %v", err)
	}
}
