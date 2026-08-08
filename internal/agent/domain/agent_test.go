package domain

import (
	"errors"
	"testing"
	"time"

	matchdomain "github.com/ccKccK-JF/MatchMind/internal/matchmaking/domain"
)

func TestProposalRequiresIndependentApprovalAndPassedRisk(t *testing.T) {
	now := time.Now().UTC()
	risk := passingRiskReport()
	candidate := matchdomain.BeamPolicy()
	candidate.Version = "proposal-1"
	proposal, err := NewPolicyProposal("proposal-1", "run-1", "analyst-1", "v1-greedy", candidate, []string{"improve fairness"}, risk, now)
	if err != nil {
		t.Fatal(err)
	}
	if err := proposal.Review("analyst-1", "self approve", true, now.Add(time.Second)); !errors.Is(err, ErrSeparationOfDuties) {
		t.Fatalf("self review error = %v", err)
	}
	if err := proposal.Review("reviewer-1", "risk checks passed", true, now.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	if err := proposal.BeginActivation("admin-1", 1000, "guarded", now.Add(2*time.Second)); err != nil {
		t.Fatal(err)
	}
	if err := proposal.CompleteActivation("approved-proposal-1", now.Add(3*time.Second)); err != nil {
		t.Fatal(err)
	}
	if err := proposal.BeginRollback("admin-1", now.Add(4*time.Second)); err != nil {
		t.Fatal(err)
	}
	if err := proposal.CompleteRollback(now.Add(5 * time.Second)); err != nil {
		t.Fatal(err)
	}
	if proposal.State != ProposalRolledBack {
		t.Fatalf("proposal state = %s", proposal.State)
	}
}

func TestProposalCannotApproveBlockingRisk(t *testing.T) {
	risk := passingRiskReport()
	risk.Passed = false
	risk.Findings[0].Status = RiskStatusBlock
	candidate := matchdomain.BeamPolicy()
	candidate.Version = "proposal-blocked"
	proposal, err := NewPolicyProposal("proposal-blocked", "run-1", "analyst-1", "v1-greedy", candidate, []string{"test"}, risk, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if err := proposal.Review("reviewer-1", "approve anyway", true, time.Now().Add(time.Second)); !errors.Is(err, ErrRiskReviewBlocked) {
		t.Fatalf("blocked review error = %v", err)
	}
}

func TestAuditRunRequiresStructuredInputOutputAndToolCalls(t *testing.T) {
	now := time.Now().UTC()
	run, err := NewAuditRun("run-1", "matchmind-advisor", "rules-v1", "prompt-v1", "analyst-1", `{"region":"hongkong"}`, now)
	if err != nil {
		t.Fatal(err)
	}
	call := ToolCall{Name: "match_quality", InputJSON: `{}`, OutputJSON: `{"count":20}`, Status: ToolCallSucceeded, StartedAt: now, FinishedAt: now.Add(time.Second)}
	if err := run.Succeed(`{"proposal_id":"proposal-1"}`, "proposal-1", []ToolCall{call}, now.Add(2*time.Second)); err != nil {
		t.Fatal(err)
	}
	if run.Status != RunStatusSucceeded || len(run.ToolCalls) != 1 {
		t.Fatalf("run = %#v", run)
	}
}

func passingRiskReport() RiskReport {
	categories := []string{"fairness", "latency", "role_fill", "sample_size", "high_rating"}
	report := RiskReport{Passed: true, SampleCount: 20}
	for _, category := range categories {
		report.Findings = append(report.Findings, RiskFinding{Category: category, Status: RiskStatusPass, Message: "passed"})
	}
	return report
}
