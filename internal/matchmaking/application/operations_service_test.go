package application

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/ccKccK-JF/MatchMind/internal/matchmaking/domain"
)

type queueSizerStub struct{ size int }

func (q queueSizerStub) QueueSize(context.Context) (int, error) { return q.size, nil }

func TestPolicyOperationsRequiresTokenAndApproval(t *testing.T) {
	manager, err := NewPolicyManager([]domain.MatchPolicy{domain.DefaultPolicy()}, domain.DefaultPolicy().Version)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 8, 8, 12, 0, 0, 0, time.UTC)
	service, err := NewPolicyOperationsService(queueSizerStub{size: 42}, manager, "secret-token", func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := service.Snapshot(context.Background())
	if err != nil || snapshot.QueueSize != 42 || len(snapshot.Policies) != 1 {
		t.Fatalf("snapshot = %#v, %v", snapshot, err)
	}
	candidate := domain.BeamPolicy()
	candidate.Version = "proposal-1"
	if _, err := service.ActivateApprovedPolicy(context.Background(), "wrong", "proposal-1", candidate, 1000, "salt"); !errors.Is(err, ErrOperationsUnauthorized) {
		t.Fatalf("unauthorized activation error = %v", err)
	}
	experiment, err := service.ActivateApprovedPolicy(context.Background(), "secret-token", "proposal-1", candidate, 1000, "salt")
	if err != nil || experiment.ID != "approved-proposal-1" {
		t.Fatalf("experiment = %#v, %v", experiment, err)
	}
	if err := service.RollbackExperiment(context.Background(), "wrong", experiment.ID); !errors.Is(err, ErrOperationsUnauthorized) {
		t.Fatalf("unauthorized rollback error = %v", err)
	}
	if err := service.RollbackExperiment(context.Background(), "secret-token", experiment.ID); err != nil {
		t.Fatal(err)
	}
}
