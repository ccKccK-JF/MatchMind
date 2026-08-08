package grpc

import (
	"context"
	"errors"
	"time"

	agentv1 "github.com/ccKccK-JF/MatchMind/gen/go/matchmind/agent/v1"
	matchmakingv1 "github.com/ccKccK-JF/MatchMind/gen/go/matchmind/matchmaking/v1"
	agentapp "github.com/ccKccK-JF/MatchMind/internal/agent/application"
	agentdomain "github.com/ccKccK-JF/MatchMind/internal/agent/domain"
	matchapp "github.com/ccKccK-JF/MatchMind/internal/matchmaking/application"
	matchdomain "github.com/ccKccK-JF/MatchMind/internal/matchmaking/domain"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type Server struct {
	agentv1.UnimplementedAgentServiceServer
	service *agentapp.Service
}

func NewServer(service *agentapp.Service) *Server { return &Server{service: service} }

func (s *Server) RunAnalysis(ctx context.Context, request *agentv1.RunAnalysisRequest) (*agentv1.RunAnalysisResponse, error) {
	if request == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}
	command := agentapp.RunCommand{
		RequestedBy: request.GetRequestedBy(), BasePolicyVersion: request.GetBasePolicyVersion(),
		Mode: request.GetMode(), ServerRegion: request.GetServerRegion(), HistoricalLimit: int(request.GetHistoricalLimit()),
	}
	var err error
	command.From, err = optionalTime(request.GetFrom(), "from")
	if err != nil {
		return nil, err
	}
	command.To, err = optionalTime(request.GetTo(), "to")
	if err != nil {
		return nil, err
	}
	run, proposal, err := s.service.RunAnalysis(ctx, command)
	if err != nil {
		return nil, agentError(err)
	}
	return &agentv1.RunAnalysisResponse{Run: runToProto(run), Proposal: proposalToProto(proposal)}, nil
}

func (s *Server) GetRun(ctx context.Context, request *agentv1.GetRunRequest) (*agentv1.GetRunResponse, error) {
	if request == nil || request.GetRunId() == "" {
		return nil, status.Error(codes.InvalidArgument, "run_id is required")
	}
	run, err := s.service.GetRun(ctx, request.GetRunId())
	if err != nil {
		return nil, agentError(err)
	}
	return &agentv1.GetRunResponse{Run: runToProto(run)}, nil
}

func (s *Server) ListRuns(ctx context.Context, request *agentv1.ListRunsRequest) (*agentv1.ListRunsResponse, error) {
	if request == nil {
		request = &agentv1.ListRunsRequest{}
	}
	runs, err := s.service.ListRuns(ctx, int(request.GetLimit()))
	if err != nil {
		return nil, agentError(err)
	}
	response := &agentv1.ListRunsResponse{}
	for _, run := range runs {
		response.Runs = append(response.Runs, runToProto(run))
	}
	return response, nil
}

func (s *Server) GetProposal(ctx context.Context, request *agentv1.GetProposalRequest) (*agentv1.GetProposalResponse, error) {
	if request == nil || request.GetProposalId() == "" {
		return nil, status.Error(codes.InvalidArgument, "proposal_id is required")
	}
	proposal, err := s.service.GetProposal(ctx, request.GetProposalId())
	if err != nil {
		return nil, agentError(err)
	}
	return &agentv1.GetProposalResponse{Proposal: proposalToProto(proposal)}, nil
}

func (s *Server) ListProposals(ctx context.Context, request *agentv1.ListProposalsRequest) (*agentv1.ListProposalsResponse, error) {
	if request == nil {
		request = &agentv1.ListProposalsRequest{}
	}
	proposals, err := s.service.ListProposals(ctx, int(request.GetLimit()))
	if err != nil {
		return nil, agentError(err)
	}
	response := &agentv1.ListProposalsResponse{}
	for _, proposal := range proposals {
		response.Proposals = append(response.Proposals, proposalToProto(proposal))
	}
	return response, nil
}

func (s *Server) ReviewProposal(ctx context.Context, request *agentv1.ReviewProposalRequest) (*agentv1.ReviewProposalResponse, error) {
	if request == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}
	proposal, err := s.service.ReviewProposal(ctx, agentapp.ReviewCommand{
		ProposalID: request.GetProposalId(), ReviewerID: request.GetReviewerId(),
		Reason: request.GetReason(), Approve: request.GetApprove(),
	})
	if err != nil {
		return nil, agentError(err)
	}
	return &agentv1.ReviewProposalResponse{Proposal: proposalToProto(proposal)}, nil
}

func (s *Server) ActivateProposal(ctx context.Context, request *agentv1.ActivateProposalRequest) (*agentv1.ActivateProposalResponse, error) {
	if request == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}
	proposal, err := s.service.ActivateProposal(ctx, agentapp.ActivateCommand{
		ProposalID: request.GetProposalId(), OperatorID: request.GetOperatorId(),
		TreatmentBasisPoints: int(request.GetTreatmentBasisPoints()), AssignmentSalt: request.GetAssignmentSalt(),
	})
	if err != nil {
		return nil, agentError(err)
	}
	return &agentv1.ActivateProposalResponse{Proposal: proposalToProto(proposal)}, nil
}

func (s *Server) RollbackProposal(ctx context.Context, request *agentv1.RollbackProposalRequest) (*agentv1.RollbackProposalResponse, error) {
	if request == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}
	proposal, err := s.service.RollbackProposal(ctx, agentapp.RollbackCommand{
		ProposalID: request.GetProposalId(), OperatorID: request.GetOperatorId(),
	})
	if err != nil {
		return nil, agentError(err)
	}
	return &agentv1.RollbackProposalResponse{Proposal: proposalToProto(proposal)}, nil
}

func runToProto(run agentdomain.AuditRun) *agentv1.AuditRun {
	result := &agentv1.AuditRun{
		Id: run.ID, AgentName: run.AgentName, Model: run.Model, PromptVersion: run.PromptVersion,
		RequestedBy: run.RequestedBy, InputJson: run.InputJSON, OutputJson: run.OutputJSON,
		PolicyVersion: run.PolicyVersion, Status: runStatusToProto(run.Status), ErrorMessage: run.ErrorMessage,
		StartedAt: timestamppb.New(run.StartedAt),
	}
	if !run.FinishedAt.IsZero() {
		result.FinishedAt = timestamppb.New(run.FinishedAt)
	}
	for _, call := range run.ToolCalls {
		result.ToolCalls = append(result.ToolCalls, &agentv1.ToolCall{
			Name: call.Name, InputJson: call.InputJSON, OutputJson: call.OutputJSON,
			Status: toolStatusToProto(call.Status), StartedAt: timestamppb.New(call.StartedAt),
			FinishedAt: timestamppb.New(call.FinishedAt),
		})
	}
	return result
}

func proposalToProto(proposal agentdomain.PolicyProposal) *agentv1.PolicyProposal {
	result := &agentv1.PolicyProposal{
		Id: proposal.ID, RunId: proposal.RunID, RequestedBy: proposal.RequestedBy,
		BasePolicyVersion: proposal.BasePolicyVersion, CandidatePolicy: policyToProto(proposal.CandidatePolicy),
		Rationale: append([]string(nil), proposal.Rationale...), RiskReport: riskToProto(proposal.RiskReport),
		State: proposalStateToProto(proposal.State), ReviewerId: proposal.ReviewerID,
		ReviewReason: proposal.ReviewReason, ExperimentId: proposal.ExperimentID,
		ActivatedBy: proposal.ActivatedBy, RolledBackBy: proposal.RolledBackBy,
		TreatmentBasisPoints: int32(proposal.TreatmentBasisPoints), AssignmentSalt: proposal.AssignmentSalt,
		CreatedAt: timestamppb.New(proposal.CreatedAt), UpdatedAt: timestamppb.New(proposal.UpdatedAt),
	}
	if !proposal.ReviewedAt.IsZero() {
		result.ReviewedAt = timestamppb.New(proposal.ReviewedAt)
	}
	if !proposal.ActivatedAt.IsZero() {
		result.ActivatedAt = timestamppb.New(proposal.ActivatedAt)
	}
	if !proposal.RolledBackAt.IsZero() {
		result.RolledBackAt = timestamppb.New(proposal.RolledBackAt)
	}
	return result
}

func riskToProto(report agentdomain.RiskReport) *agentv1.RiskReport {
	result := &agentv1.RiskReport{Passed: report.Passed, SampleCount: int32(report.SampleCount)}
	for _, finding := range report.Findings {
		result.Findings = append(result.Findings, &agentv1.RiskFinding{
			Category: finding.Category, Status: riskStatusToProto(finding.Status),
			Observed: finding.Observed, Threshold: finding.Threshold, Message: finding.Message,
		})
	}
	return result
}

func policyToProto(policy matchdomain.MatchPolicy) *matchmakingv1.MatchPolicyDefinition {
	return &matchmakingv1.MatchPolicyDefinition{
		Version: policy.Version, TeamSize: int32(policy.TeamSize), CandidateLimit: int32(policy.CandidateLimit),
		TeamAlgorithm: string(policy.TeamAlgorithm), BeamWidth: int32(policy.BeamWidth),
		SkillWeight: policy.SkillWeight, RoleWeight: policy.RoleWeight, LatencyWeight: policy.LatencyWeight,
		PartyWeight: policy.PartyWeight, WaitWeight: policy.WaitWeight,
		InitialRatingRange: policy.InitialRatingRange, MaxRatingRange: policy.MaxRatingRange,
		RatingExpansionPerSecond: policy.RatingExpansionPerSecond, MaxLatencyMs: int32(policy.MaxLatencyMS),
		InitialLatencyMs: int32(policy.InitialLatencyMS), LatencyExpansionPerSecond: policy.LatencyExpansionPerSecond,
		RoleRelaxationAfterMs:    policy.RoleRelaxationAfter.Milliseconds(),
		RoleRelaxationPerSecond:  policy.RoleRelaxationPerSecond,
		MaxNonPreferredRoleScore: policy.MaxNonPreferredRoleScore,
		MinQualityScore:          policy.MinQualityScore, ReservationTtlMs: policy.ReservationTTL.Milliseconds(),
		TicketTtlMs: policy.TicketTTL.Milliseconds(),
	}
}

func optionalTime(timestamp *timestamppb.Timestamp, name string) (time.Time, error) {
	if timestamp == nil {
		return time.Time{}, nil
	}
	if err := timestamp.CheckValid(); err != nil {
		return time.Time{}, status.Errorf(codes.InvalidArgument, "%s must be a valid timestamp", name)
	}
	return timestamp.AsTime(), nil
}

func runStatusToProto(value agentdomain.RunStatus) agentv1.RunStatus {
	switch value {
	case agentdomain.RunStatusRunning:
		return agentv1.RunStatus_RUN_STATUS_RUNNING
	case agentdomain.RunStatusSucceeded:
		return agentv1.RunStatus_RUN_STATUS_SUCCEEDED
	case agentdomain.RunStatusFailed:
		return agentv1.RunStatus_RUN_STATUS_FAILED
	default:
		return agentv1.RunStatus_RUN_STATUS_UNSPECIFIED
	}
}

func toolStatusToProto(value agentdomain.ToolCallStatus) agentv1.ToolCallStatus {
	if value == agentdomain.ToolCallSucceeded {
		return agentv1.ToolCallStatus_TOOL_CALL_STATUS_SUCCEEDED
	}
	if value == agentdomain.ToolCallFailed {
		return agentv1.ToolCallStatus_TOOL_CALL_STATUS_FAILED
	}
	return agentv1.ToolCallStatus_TOOL_CALL_STATUS_UNSPECIFIED
}

func proposalStateToProto(value agentdomain.ProposalState) agentv1.ProposalState {
	values := map[agentdomain.ProposalState]agentv1.ProposalState{
		agentdomain.ProposalPendingApproval: agentv1.ProposalState_PROPOSAL_STATE_PENDING_APPROVAL,
		agentdomain.ProposalApproved:        agentv1.ProposalState_PROPOSAL_STATE_APPROVED,
		agentdomain.ProposalRejected:        agentv1.ProposalState_PROPOSAL_STATE_REJECTED,
		agentdomain.ProposalActivating:      agentv1.ProposalState_PROPOSAL_STATE_ACTIVATING,
		agentdomain.ProposalActive:          agentv1.ProposalState_PROPOSAL_STATE_ACTIVE,
		agentdomain.ProposalRollingBack:     agentv1.ProposalState_PROPOSAL_STATE_ROLLING_BACK,
		agentdomain.ProposalRolledBack:      agentv1.ProposalState_PROPOSAL_STATE_ROLLED_BACK,
	}
	return values[value]
}

func riskStatusToProto(value agentdomain.RiskStatus) agentv1.RiskStatus {
	switch value {
	case agentdomain.RiskStatusPass:
		return agentv1.RiskStatus_RISK_STATUS_PASS
	case agentdomain.RiskStatusWarning:
		return agentv1.RiskStatus_RISK_STATUS_WARNING
	case agentdomain.RiskStatusBlock:
		return agentv1.RiskStatus_RISK_STATUS_BLOCK
	default:
		return agentv1.RiskStatus_RISK_STATUS_UNSPECIFIED
	}
}

func agentError(err error) error {
	if code := status.Code(err); code != codes.Unknown {
		return err
	}
	switch {
	case errors.Is(err, agentapp.ErrInvalidCommand), errors.Is(err, agentdomain.ErrInvalidAgentRun), errors.Is(err, agentdomain.ErrInvalidProposal):
		return status.Error(codes.InvalidArgument, err.Error())
	case errors.Is(err, agentapp.ErrRunNotFound), errors.Is(err, agentapp.ErrProposalNotFound), errors.Is(err, matchapp.ErrPolicyNotFound):
		return status.Error(codes.NotFound, err.Error())
	case errors.Is(err, agentapp.ErrRepositoryConflict):
		return status.Error(codes.Aborted, err.Error())
	case errors.Is(err, agentdomain.ErrSeparationOfDuties):
		return status.Error(codes.PermissionDenied, err.Error())
	case errors.Is(err, agentdomain.ErrRiskReviewBlocked), errors.Is(err, agentdomain.ErrIllegalProposalState):
		return status.Error(codes.FailedPrecondition, err.Error())
	case errors.Is(err, agentapp.ErrToolUnavailable):
		return status.Error(codes.Unavailable, err.Error())
	case errors.Is(err, context.Canceled):
		return status.Error(codes.Canceled, err.Error())
	case errors.Is(err, context.DeadlineExceeded):
		return status.Error(codes.DeadlineExceeded, err.Error())
	default:
		return status.Error(codes.Internal, "internal agent service error")
	}
}
