package memory

import (
	"context"
	"testing"
	"time"

	agentdomain "github.com/ccKccK-JF/MatchMind/internal/agent/domain"
)

func TestRecoverIncompleteRunsMarksOnlyRunningAuditsFailed(t *testing.T) {
	repository := NewRepository()
	startedAt := time.Date(2026, 8, 8, 12, 0, 0, 0, time.UTC)
	run, err := agentdomain.NewAuditRun(
		"run-1", "matchmind-advisor", "rules-v1", "prompt-v1", "analyst-1", `{}`, startedAt,
	)
	if err != nil {
		t.Fatal(err)
	}
	if err := repository.CreateRun(context.Background(), run.Clone()); err != nil {
		t.Fatal(err)
	}
	recovered, err := repository.RecoverIncompleteRuns(context.Background(), startedAt.Add(time.Minute))
	if err != nil || recovered != 1 {
		t.Fatalf("recovered = %d, error = %v", recovered, err)
	}
	stored, err := repository.GetRun(context.Background(), run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.Status != agentdomain.RunStatusFailed || stored.FinishedAt.IsZero() || stored.ErrorMessage == "" {
		t.Fatalf("recovered run = %#v", stored)
	}
	recovered, err = repository.RecoverIncompleteRuns(context.Background(), startedAt.Add(2*time.Minute))
	if err != nil || recovered != 0 {
		t.Fatalf("second recovery = %d, error = %v", recovered, err)
	}
}
