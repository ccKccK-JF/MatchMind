package domain

import (
	"encoding/json"
	"errors"
	"strings"
	"time"

	matchdomain "github.com/ccKccK-JF/MatchMind/internal/matchmaking/domain"
)

var (
	ErrInvalidAgentRun      = errors.New("invalid agent run")
	ErrInvalidProposal      = errors.New("invalid policy proposal")
	ErrIllegalProposalState = errors.New("illegal policy proposal state transition")
	ErrSeparationOfDuties   = errors.New("proposal requester cannot approve their own proposal")
	ErrRiskReviewBlocked    = errors.New("proposal has blocking risk findings")
)

type RunStatus string

const (
	RunStatusRunning   RunStatus = "RUNNING"
	RunStatusSucceeded RunStatus = "SUCCEEDED"
	RunStatusFailed    RunStatus = "FAILED"
)

type ToolCallStatus string

const (
	ToolCallSucceeded ToolCallStatus = "SUCCEEDED"
	ToolCallFailed    ToolCallStatus = "FAILED"
)

type ToolCall struct {
	Name       string         `json:"name"`
	InputJSON  string         `json:"input_json"`
	OutputJSON string         `json:"output_json"`
	Status     ToolCallStatus `json:"status"`
	StartedAt  time.Time      `json:"started_at"`
	FinishedAt time.Time      `json:"finished_at"`
}

type AuditRun struct {
	ID            string     `json:"id"`
	AgentName     string     `json:"agent_name"`
	Model         string     `json:"model"`
	PromptVersion string     `json:"prompt_version"`
	RequestedBy   string     `json:"requested_by"`
	InputJSON     string     `json:"input_json"`
	OutputJSON    string     `json:"output_json"`
	PolicyVersion string     `json:"policy_version"`
	Status        RunStatus  `json:"status"`
	ErrorMessage  string     `json:"error_message"`
	ToolCalls     []ToolCall `json:"tool_calls"`
	StartedAt     time.Time  `json:"started_at"`
	FinishedAt    time.Time  `json:"finished_at"`
}

func NewAuditRun(id, agentName, model, promptVersion, requestedBy, inputJSON string, now time.Time) (*AuditRun, error) {
	run := &AuditRun{
		ID: strings.TrimSpace(id), AgentName: strings.TrimSpace(agentName), Model: strings.TrimSpace(model),
		PromptVersion: strings.TrimSpace(promptVersion), RequestedBy: strings.TrimSpace(requestedBy),
		InputJSON: strings.TrimSpace(inputJSON), Status: RunStatusRunning, StartedAt: now.UTC(),
	}
	if run.ID == "" || run.AgentName == "" || run.Model == "" || run.PromptVersion == "" ||
		run.RequestedBy == "" || now.IsZero() || !json.Valid([]byte(run.InputJSON)) {
		return nil, ErrInvalidAgentRun
	}
	return run, nil
}

func (r *AuditRun) Succeed(outputJSON, policyVersion string, calls []ToolCall, now time.Time) error {
	if r.Status != RunStatusRunning || now.Before(r.StartedAt) || !json.Valid([]byte(outputJSON)) ||
		strings.TrimSpace(policyVersion) == "" || validateToolCalls(calls) != nil {
		return ErrInvalidAgentRun
	}
	r.OutputJSON = outputJSON
	r.PolicyVersion = strings.TrimSpace(policyVersion)
	r.ToolCalls = cloneToolCalls(calls)
	r.Status = RunStatusSucceeded
	r.FinishedAt = now.UTC()
	return nil
}

func (r *AuditRun) Fail(message string, calls []ToolCall, now time.Time) error {
	message = strings.TrimSpace(message)
	if r.Status != RunStatusRunning || message == "" || now.Before(r.StartedAt) || validateToolCalls(calls) != nil {
		return ErrInvalidAgentRun
	}
	r.ErrorMessage = message
	r.ToolCalls = cloneToolCalls(calls)
	r.Status = RunStatusFailed
	r.FinishedAt = now.UTC()
	return nil
}

func (r AuditRun) Clone() AuditRun {
	r.ToolCalls = cloneToolCalls(r.ToolCalls)
	return r
}

type RiskStatus string

const (
	RiskStatusPass    RiskStatus = "PASS"
	RiskStatusWarning RiskStatus = "WARNING"
	RiskStatusBlock   RiskStatus = "BLOCK"
)

type RiskFinding struct {
	Category  string     `json:"category"`
	Status    RiskStatus `json:"status"`
	Observed  float64    `json:"observed"`
	Threshold float64    `json:"threshold"`
	Message   string     `json:"message"`
}

type RiskReport struct {
	Passed      bool          `json:"passed"`
	SampleCount int           `json:"sample_count"`
	Findings    []RiskFinding `json:"findings"`
}

type ProposalState string

const (
	ProposalPendingApproval ProposalState = "PENDING_APPROVAL"
	ProposalApproved        ProposalState = "APPROVED"
	ProposalRejected        ProposalState = "REJECTED"
	ProposalActivating      ProposalState = "ACTIVATING"
	ProposalActive          ProposalState = "ACTIVE"
	ProposalRollingBack     ProposalState = "ROLLING_BACK"
	ProposalRolledBack      ProposalState = "ROLLED_BACK"
)

type PolicyProposal struct {
	ID                   string                  `json:"id"`
	RunID                string                  `json:"run_id"`
	RequestedBy          string                  `json:"requested_by"`
	BasePolicyVersion    string                  `json:"base_policy_version"`
	CandidatePolicy      matchdomain.MatchPolicy `json:"candidate_policy"`
	Rationale            []string                `json:"rationale"`
	RiskReport           RiskReport              `json:"risk_report"`
	State                ProposalState           `json:"state"`
	ReviewerID           string                  `json:"reviewer_id"`
	ReviewReason         string                  `json:"review_reason"`
	ReviewedAt           time.Time               `json:"reviewed_at"`
	ExperimentID         string                  `json:"experiment_id"`
	ActivatedBy          string                  `json:"activated_by"`
	TreatmentBasisPoints int                     `json:"treatment_basis_points"`
	AssignmentSalt       string                  `json:"assignment_salt"`
	ActivatedAt          time.Time               `json:"activated_at"`
	RolledBackBy         string                  `json:"rolled_back_by"`
	RolledBackAt         time.Time               `json:"rolled_back_at"`
	CreatedAt            time.Time               `json:"created_at"`
	UpdatedAt            time.Time               `json:"updated_at"`
}

func NewPolicyProposal(
	id, runID, requestedBy, basePolicyVersion string,
	candidate matchdomain.MatchPolicy,
	rationale []string,
	risk RiskReport,
	now time.Time,
) (*PolicyProposal, error) {
	proposal := &PolicyProposal{
		ID: strings.TrimSpace(id), RunID: strings.TrimSpace(runID), RequestedBy: strings.TrimSpace(requestedBy),
		BasePolicyVersion: strings.TrimSpace(basePolicyVersion), CandidatePolicy: candidate,
		Rationale: append([]string(nil), rationale...), RiskReport: cloneRiskReport(risk),
		State: ProposalPendingApproval, CreatedAt: now.UTC(), UpdatedAt: now.UTC(),
	}
	if proposal.ID == "" || proposal.RunID == "" || proposal.RequestedBy == "" ||
		proposal.BasePolicyVersion == "" || len(proposal.Rationale) == 0 || now.IsZero() ||
		candidate.Validate() != nil || validateRiskReport(risk) != nil {
		return nil, ErrInvalidProposal
	}
	return proposal, nil
}

func (p *PolicyProposal) Review(reviewerID, reason string, approve bool, now time.Time) error {
	reviewerID = strings.TrimSpace(reviewerID)
	reason = strings.TrimSpace(reason)
	if p.State != ProposalPendingApproval {
		return ErrIllegalProposalState
	}
	if reviewerID == "" || reason == "" || now.Before(p.CreatedAt) {
		return ErrInvalidProposal
	}
	if reviewerID == p.RequestedBy {
		return ErrSeparationOfDuties
	}
	if approve && !p.RiskReport.Passed {
		return ErrRiskReviewBlocked
	}
	p.ReviewerID = reviewerID
	p.ReviewReason = reason
	p.ReviewedAt = now.UTC()
	if approve {
		p.State = ProposalApproved
	} else {
		p.State = ProposalRejected
	}
	p.UpdatedAt = now.UTC()
	return nil
}

func (p *PolicyProposal) BeginActivation(operatorID string, treatmentBasisPoints int, assignmentSalt string, now time.Time) error {
	operatorID = strings.TrimSpace(operatorID)
	assignmentSalt = strings.TrimSpace(assignmentSalt)
	if p.State != ProposalApproved {
		return ErrIllegalProposalState
	}
	if operatorID == "" || treatmentBasisPoints < 1 || treatmentBasisPoints > 10000 || assignmentSalt == "" || now.Before(p.ReviewedAt) {
		return ErrInvalidProposal
	}
	p.State = ProposalActivating
	p.ActivatedBy = operatorID
	p.TreatmentBasisPoints = treatmentBasisPoints
	p.AssignmentSalt = assignmentSalt
	p.ActivatedAt = now.UTC()
	p.UpdatedAt = now.UTC()
	return nil
}

func (p *PolicyProposal) CompleteActivation(experimentID string, now time.Time) error {
	experimentID = strings.TrimSpace(experimentID)
	if p.State != ProposalActivating {
		return ErrIllegalProposalState
	}
	if experimentID == "" || now.Before(p.ActivatedAt) {
		return ErrInvalidProposal
	}
	p.State = ProposalActive
	p.ExperimentID = experimentID
	p.UpdatedAt = now.UTC()
	return nil
}

func (p *PolicyProposal) BeginRollback(operatorID string, now time.Time) error {
	operatorID = strings.TrimSpace(operatorID)
	if p.State != ProposalActive {
		return ErrIllegalProposalState
	}
	if operatorID == "" || now.Before(p.ActivatedAt) {
		return ErrInvalidProposal
	}
	p.State = ProposalRollingBack
	p.RolledBackBy = operatorID
	p.RolledBackAt = now.UTC()
	p.UpdatedAt = now.UTC()
	return nil
}

func (p *PolicyProposal) CompleteRollback(now time.Time) error {
	if p.State != ProposalRollingBack {
		return ErrIllegalProposalState
	}
	if now.Before(p.RolledBackAt) {
		return ErrInvalidProposal
	}
	p.State = ProposalRolledBack
	p.UpdatedAt = now.UTC()
	return nil
}

func (p PolicyProposal) Clone() PolicyProposal {
	p.Rationale = append([]string(nil), p.Rationale...)
	p.RiskReport = cloneRiskReport(p.RiskReport)
	return p
}

func validateRiskReport(report RiskReport) error {
	if report.SampleCount < 0 || len(report.Findings) != 5 {
		return ErrInvalidProposal
	}
	passed := true
	seen := make(map[string]struct{}, len(report.Findings))
	for _, finding := range report.Findings {
		finding.Category = strings.TrimSpace(finding.Category)
		if finding.Category == "" || strings.TrimSpace(finding.Message) == "" {
			return ErrInvalidProposal
		}
		if _, duplicate := seen[finding.Category]; duplicate {
			return ErrInvalidProposal
		}
		seen[finding.Category] = struct{}{}
		switch finding.Status {
		case RiskStatusPass, RiskStatusWarning:
		case RiskStatusBlock:
			passed = false
		default:
			return ErrInvalidProposal
		}
	}
	if report.Passed != passed {
		return ErrInvalidProposal
	}
	return nil
}

func validateToolCalls(calls []ToolCall) error {
	for _, call := range calls {
		if strings.TrimSpace(call.Name) == "" || !json.Valid([]byte(call.InputJSON)) ||
			!json.Valid([]byte(call.OutputJSON)) || call.StartedAt.IsZero() ||
			call.FinishedAt.Before(call.StartedAt) ||
			(call.Status != ToolCallSucceeded && call.Status != ToolCallFailed) {
			return ErrInvalidAgentRun
		}
	}
	return nil
}

func cloneToolCalls(calls []ToolCall) []ToolCall { return append([]ToolCall(nil), calls...) }

func cloneRiskReport(report RiskReport) RiskReport {
	report.Findings = append([]RiskFinding(nil), report.Findings...)
	return report
}
