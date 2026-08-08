package application_test

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	agentapp "github.com/ccKccK-JF/MatchMind/internal/agent/application"
	agentdomain "github.com/ccKccK-JF/MatchMind/internal/agent/domain"
	agentmemory "github.com/ccKccK-JF/MatchMind/internal/agent/repository/memory"
	matchapp "github.com/ccKccK-JF/MatchMind/internal/matchmaking/application"
	matchdomain "github.com/ccKccK-JF/MatchMind/internal/matchmaking/domain"
)

type toolStub struct {
	snapshot        matchapp.OperationalSnapshot
	analysis        matchapp.QualityAnalysis
	snapshotErr     error
	activateErr     error
	activated       int
	rolledBack      int
	lastBasisPoints int
	lastSalt        string
}

func (t *toolStub) OperationalSnapshot(context.Context) (matchapp.OperationalSnapshot, error) {
	return t.snapshot, t.snapshotErr
}

func (t *toolStub) AnalyzeMatchQuality(context.Context, matchapp.MatchHistoryFilter) (matchapp.QualityAnalysis, error) {
	return t.analysis, nil
}

func (t *toolStub) ReplayHistoricalMatch(_ context.Context, request matchapp.ReplayRequest) (matchapp.ReplayReport, error) {
	policy := request.CandidatePolicies[0]
	for _, observation := range t.analysis.Observations {
		if observation.MatchID != request.MatchID {
			continue
		}
		return matchapp.ReplayReport{
			SourceMatchID: request.MatchID, SourcePredictedQuality: observation.PredictedQuality,
			Outcomes: []matchapp.ReplayOutcome{{
				PolicyVersion: policy.Version, Algorithm: policy.TeamAlgorithm, Matched: true,
				Quality: matchdomain.MatchQuality{
					TotalScore: observation.PredictedQuality + 1, SkillScore: observation.SkillScore + 1,
					RoleScore: observation.RoleScore, LatencyScore: observation.LatencyScore,
				},
			}},
		}, nil
	}
	return matchapp.ReplayReport{}, matchapp.ErrMatchNotFound
}

func (t *toolStub) ActivateApprovedPolicy(_ context.Context, approvalID string, policy matchdomain.MatchPolicy, basisPoints int, salt string) (matchapp.PolicyExperiment, error) {
	t.activated++
	t.lastBasisPoints = basisPoints
	t.lastSalt = salt
	if t.activateErr != nil {
		err := t.activateErr
		t.activateErr = nil
		return matchapp.PolicyExperiment{}, err
	}
	return matchapp.PolicyExperiment{
		ID: "approved-" + approvalID, ControlVersion: matchdomain.DefaultPolicy().Version,
		TreatmentVersion: policy.Version, TreatmentBasisPoints: basisPoints,
		AssignmentSalt: salt, StartedAt: time.Now(),
	}, nil
}

func TestActivationRetryUsesPersistedRolloutParameters(t *testing.T) {
	repository := agentmemory.NewRepository()
	tools := healthyTools(20)
	service := newTestService(t, repository, tools)
	_, proposal, err := service.RunAnalysis(context.Background(), agentapp.RunCommand{
		RequestedBy: "analyst-1", BasePolicyVersion: "v1-greedy", HistoricalLimit: 20,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = service.ReviewProposal(context.Background(), agentapp.ReviewCommand{
		ProposalID: proposal.ID, ReviewerID: "reviewer-1", Reason: "checks passed", Approve: true,
	}); err != nil {
		t.Fatal(err)
	}
	tools.activateErr = agentapp.ErrToolUnavailable
	_, err = service.ActivateProposal(context.Background(), agentapp.ActivateCommand{
		ProposalID: proposal.ID, OperatorID: "admin-1", TreatmentBasisPoints: 1000, AssignmentSalt: "original-salt",
	})
	if !errors.Is(err, agentapp.ErrToolUnavailable) {
		t.Fatalf("first activation error = %v", err)
	}
	active, err := service.ActivateProposal(context.Background(), agentapp.ActivateCommand{
		ProposalID: proposal.ID, OperatorID: "admin-2", TreatmentBasisPoints: 9000, AssignmentSalt: "changed-salt",
	})
	if err != nil {
		t.Fatal(err)
	}
	if active.State != agentdomain.ProposalActive || active.TreatmentBasisPoints != 1000 ||
		active.AssignmentSalt != "original-salt" || tools.lastBasisPoints != 1000 || tools.lastSalt != "original-salt" {
		t.Fatalf("retry changed rollout parameters: proposal=%#v tools=%d/%q", active, tools.lastBasisPoints, tools.lastSalt)
	}
}

func (t *toolStub) RollbackPolicyExperiment(context.Context, string) error {
	t.rolledBack++
	return nil
}

func TestAgentRunProducesAuditedProposalAndApprovalGate(t *testing.T) {
	repository := agentmemory.NewRepository()
	tools := healthyTools(20)
	service := newTestService(t, repository, tools)
	run, proposal, err := service.RunAnalysis(context.Background(), agentapp.RunCommand{
		RequestedBy: "analyst-1", BasePolicyVersion: "v1-greedy",
		Mode: "ranked_5v5", ServerRegion: "HONGKONG", HistoricalLimit: 20,
	})
	if err != nil {
		t.Fatal(err)
	}
	if run.Status != agentdomain.RunStatusSucceeded || len(run.ToolCalls) != 22 ||
		proposal.State != agentdomain.ProposalPendingApproval || !proposal.RiskReport.Passed {
		t.Fatalf("run = %#v\nproposal = %#v", run, proposal)
	}
	if proposal.CandidatePolicy.Version == "v1-greedy" || proposal.CandidatePolicy.TeamAlgorithm != matchdomain.TeamAlgorithmBeam {
		t.Fatalf("candidate policy = %#v", proposal.CandidatePolicy)
	}
	storedRun, err := service.GetRun(context.Background(), run.ID)
	if err != nil || storedRun.OutputJSON == "" || storedRun.PolicyVersion != proposal.CandidatePolicy.Version {
		t.Fatalf("stored run = %#v, %v", storedRun, err)
	}
	approved, err := service.ReviewProposal(context.Background(), agentapp.ReviewCommand{
		ProposalID: proposal.ID, ReviewerID: "reviewer-1", Reason: "all five checks passed", Approve: true,
	})
	if err != nil || approved.State != agentdomain.ProposalApproved {
		t.Fatalf("approved proposal = %#v, %v", approved, err)
	}
	active, err := service.ActivateProposal(context.Background(), agentapp.ActivateCommand{
		ProposalID: proposal.ID, OperatorID: "admin-1", TreatmentBasisPoints: 1000, AssignmentSalt: "guarded-rollout",
	})
	if err != nil || active.State != agentdomain.ProposalActive || tools.activated != 1 {
		t.Fatalf("active proposal = %#v, activated = %d, error = %v", active, tools.activated, err)
	}
	rolledBack, err := service.RollbackProposal(context.Background(), agentapp.RollbackCommand{
		ProposalID: proposal.ID, OperatorID: "admin-1",
	})
	if err != nil || rolledBack.State != agentdomain.ProposalRolledBack || tools.rolledBack != 1 {
		t.Fatalf("rolled back proposal = %#v, calls = %d, error = %v", rolledBack, tools.rolledBack, err)
	}
}

func TestAgentBlocksApprovalWhenSampleIsInsufficient(t *testing.T) {
	repository := agentmemory.NewRepository()
	tools := healthyTools(3)
	service := newTestService(t, repository, tools)
	_, proposal, err := service.RunAnalysis(context.Background(), agentapp.RunCommand{
		RequestedBy: "analyst-1", BasePolicyVersion: "v1-greedy", HistoricalLimit: 3,
	})
	if err != nil {
		t.Fatal(err)
	}
	if proposal.RiskReport.Passed {
		t.Fatalf("risk report unexpectedly passed: %#v", proposal.RiskReport)
	}
	_, err = service.ReviewProposal(context.Background(), agentapp.ReviewCommand{
		ProposalID: proposal.ID, ReviewerID: "reviewer-1", Reason: "ignore sample risk", Approve: true,
	})
	if !errors.Is(err, agentdomain.ErrRiskReviewBlocked) {
		t.Fatalf("approval error = %v", err)
	}
}

func TestAgentToolFailureIsPersistedInAudit(t *testing.T) {
	repository := agentmemory.NewRepository()
	tools := healthyTools(20)
	tools.snapshotErr = agentapp.ErrToolUnavailable
	service := newTestService(t, repository, tools)
	run, _, err := service.RunAnalysis(context.Background(), agentapp.RunCommand{
		RequestedBy: "analyst-1", BasePolicyVersion: "v1-greedy", HistoricalLimit: 20,
	})
	if !errors.Is(err, agentapp.ErrToolUnavailable) {
		t.Fatalf("run error = %v", err)
	}
	stored, getErr := service.GetRun(context.Background(), run.ID)
	if getErr != nil || stored.Status != agentdomain.RunStatusFailed || len(stored.ToolCalls) != 1 || stored.ErrorMessage == "" {
		t.Fatalf("failed audit = %#v, %v", stored, getErr)
	}
}

func healthyTools(sampleCount int) *toolStub {
	policy := matchdomain.DefaultPolicy()
	tools := &toolStub{snapshot: matchapp.OperationalSnapshot{QueueSize: 7, Policies: []matchdomain.MatchPolicy{policy, matchdomain.BeamPolicy()}}}
	for index := 0; index < sampleCount; index++ {
		tools.analysis.Observations = append(tools.analysis.Observations, matchapp.MatchQualityObservation{
			MatchID: fmt.Sprintf("match-%02d", index), PolicyVersion: policy.Version,
			PredictedQuality: 85, ActualQuality: 84, SkillScore: 90, RoleScore: 90,
			LatencyScore: 90, AverageRating: 1900, PredictedWinRateA: .5,
			WinProbabilityBrier: .25,
		})
	}
	return tools
}

func newTestService(t *testing.T, repository agentapp.Repository, tools agentapp.ToolClient) *agentapp.Service {
	t.Helper()
	ids := []string{"run-1", "proposal-1"}
	index := 0
	service, err := agentapp.NewService(
		repository, tools, "matchmind-advisor", "rules-v1", "agent-policy-v1", "v1-greedy",
		func() (string, error) { id := ids[index]; index++; return id, nil },
		func() time.Time { return time.Date(2026, 8, 8, 12, 0, 0, 0, time.UTC) },
	)
	if err != nil {
		t.Fatal(err)
	}
	return service
}
