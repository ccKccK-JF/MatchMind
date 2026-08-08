package matchmakinggrpc

import (
	"context"
	"time"

	matchmakingv1 "github.com/ccKccK-JF/MatchMind/gen/go/matchmind/matchmaking/v1"
	matchapp "github.com/ccKccK-JF/MatchMind/internal/matchmaking/application"
	matchdomain "github.com/ccKccK-JF/MatchMind/internal/matchmaking/domain"
	"google.golang.org/grpc/metadata"
	"google.golang.org/protobuf/types/known/timestamppb"
)

const defaultToolTimeout = 10 * time.Second

type Client struct {
	rpc          matchmakingv1.MatchmakingServiceClient
	controlToken string
	timeout      time.Duration
}

func NewClient(rpc matchmakingv1.MatchmakingServiceClient, controlToken string) *Client {
	return &Client{rpc: rpc, controlToken: controlToken, timeout: defaultToolTimeout}
}

func (c *Client) OperationalSnapshot(ctx context.Context) (matchapp.OperationalSnapshot, error) {
	callContext, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()
	response, err := c.rpc.GetOperationalSnapshot(callContext, &matchmakingv1.GetOperationalSnapshotRequest{})
	if err != nil {
		return matchapp.OperationalSnapshot{}, err
	}
	result := matchapp.OperationalSnapshot{QueueSize: int(response.GetQueueSize())}
	for _, definition := range response.GetPolicies() {
		policy, mapErr := policyFromProto(definition)
		if mapErr != nil {
			return matchapp.OperationalSnapshot{}, mapErr
		}
		result.Policies = append(result.Policies, policy)
	}
	if experiment := response.GetActiveExperiment(); experiment != nil {
		mapped := experimentFromProto(experiment)
		result.ActiveExperiment = &mapped
	}
	return result, nil
}

func (c *Client) AnalyzeMatchQuality(ctx context.Context, filter matchapp.MatchHistoryFilter) (matchapp.QualityAnalysis, error) {
	request := &matchmakingv1.AnalyzeMatchQualityRequest{
		PolicyVersion: filter.PolicyVersion, Mode: filter.Mode, ServerRegion: filter.ServerRegion,
		Limit: int32(filter.Limit),
	}
	if !filter.From.IsZero() {
		request.From = timestamppb.New(filter.From)
	}
	if !filter.To.IsZero() {
		request.To = timestamppb.New(filter.To)
	}
	callContext, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()
	response, err := c.rpc.AnalyzeMatchQuality(callContext, request)
	if err != nil {
		return matchapp.QualityAnalysis{}, err
	}
	result := matchapp.QualityAnalysis{}
	for _, observation := range response.GetObservations() {
		mapped := matchapp.MatchQualityObservation{
			MatchID: observation.GetMatchId(), PolicyVersion: observation.GetPolicyVersion(),
			Mode: observation.GetMode(), ServerRegion: observation.GetServerRegion(),
			PredictedQuality: observation.GetPredictedQuality(), ActualQuality: observation.GetActualQuality(),
			SkillScore: observation.GetSkillScore(), RoleScore: observation.GetRoleScore(),
			LatencyScore: observation.GetLatencyScore(), PartyScore: observation.GetPartyScore(),
			WaitScore: observation.GetWaitScore(), AverageRating: observation.GetAverageRating(),
			SignedQualityError: observation.GetSignedQualityError(), AbsoluteQualityError: observation.GetAbsoluteQualityError(),
			PredictedWinRateA: observation.GetPredictedWinRateA(), TeamAOutcome: observation.GetTeamAOutcome(),
			WinProbabilityBrier: observation.GetWinProbabilityBrier(), DurationSeconds: int(observation.GetDurationSeconds()),
			OneSided: observation.GetOneSided(), HasAFK: observation.GetHasAfk(), Surrendered: observation.GetSurrendered(),
		}
		if observation.GetCreatedAt() != nil {
			mapped.CreatedAt = observation.GetCreatedAt().AsTime()
		}
		result.Observations = append(result.Observations, mapped)
	}
	for _, summary := range response.GetSummaries() {
		result.Summaries = append(result.Summaries, matchapp.PolicyQualitySummary{
			PolicyVersion: summary.GetPolicyVersion(), MatchCount: int(summary.GetMatchCount()),
			AveragePredictedQuality: summary.GetAveragePredictedQuality(), AverageActualQuality: summary.GetAverageActualQuality(),
			MeanSignedQualityError: summary.GetMeanSignedQualityError(), MeanAbsoluteQualityError: summary.GetMeanAbsoluteQualityError(),
			WinProbabilityBrierScore: summary.GetWinProbabilityBrierScore(), TeamAWinRate: summary.GetTeamAWinRate(),
			AverageDurationSeconds: summary.GetAverageDurationSeconds(), OneSidedRate: summary.GetOneSidedRate(),
			AFKRate: summary.GetAfkRate(), SurrenderRate: summary.GetSurrenderRate(),
		})
	}
	return result, nil
}

func (c *Client) ReplayHistoricalMatch(ctx context.Context, request matchapp.ReplayRequest) (matchapp.ReplayReport, error) {
	rpcRequest := &matchmakingv1.ReplayHistoricalMatchRequest{
		MatchId: request.MatchID, PolicyVersions: request.PolicyVersions, TicketIds: request.TicketIDs,
	}
	for _, policy := range request.CandidatePolicies {
		rpcRequest.CandidatePolicies = append(rpcRequest.CandidatePolicies, policyToProto(policy))
	}
	callContext, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()
	response, err := c.rpc.ReplayHistoricalMatch(callContext, rpcRequest)
	if err != nil {
		return matchapp.ReplayReport{}, err
	}
	result := matchapp.ReplayReport{
		SourceMatchID: response.GetSourceMatchId(), SourcePolicyVersion: response.GetSourcePolicyVersion(),
		SourcePredictedQuality: response.GetSourcePredictedQuality(), SourceActualQuality: response.GetSourceActualQuality(),
		SourceAbsoluteError: response.GetSourceAbsoluteError(), TicketCount: int(response.GetTicketCount()),
	}
	for _, outcome := range response.GetOutcomes() {
		quality := outcome.GetQuality()
		mapped := matchapp.ReplayOutcome{
			PolicyVersion: outcome.GetPolicyVersion(), Algorithm: matchdomain.TeamAlgorithm(outcome.GetAlgorithm()),
			Matched: outcome.GetMatched(), FailureReason: outcome.GetFailureReason(),
			AcceptedTickets: int(outcome.GetAcceptedTickets()), RejectedTickets: int(outcome.GetRejectedTickets()),
			CandidateSets: int(outcome.GetCandidateSets()), FormationsEvaluated: int(outcome.GetFormationsEvaluated()),
			QualityDelta: outcome.GetQualityDelta(), SameTeamSplit: outcome.GetSameTeamSplit(),
			SameRoleAssignments: outcome.GetSameRoleAssignments(),
		}
		if quality != nil {
			mapped.Quality = matchdomain.MatchQuality{
				TotalScore: quality.GetTotalScore(), SkillScore: quality.GetSkillScore(), RoleScore: quality.GetRoleScore(),
				LatencyScore: quality.GetLatencyScore(), PartyScore: quality.GetPartyScore(), WaitScore: quality.GetWaitScore(),
				PredictedWinRateA: quality.GetPredictedWinRateA(), PredictedWinRateB: quality.GetPredictedWinRateB(),
				Reasons: append([]string(nil), quality.GetReasons()...),
			}
		}
		result.Outcomes = append(result.Outcomes, mapped)
	}
	return result, nil
}

func (c *Client) ActivateApprovedPolicy(
	ctx context.Context,
	approvalID string,
	policy matchdomain.MatchPolicy,
	treatmentBasisPoints int,
	assignmentSalt string,
) (matchapp.PolicyExperiment, error) {
	callContext, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()
	callContext = metadata.AppendToOutgoingContext(callContext, "x-agent-control-token", c.controlToken)
	response, err := c.rpc.ActivateApprovedPolicy(callContext, &matchmakingv1.ActivateApprovedPolicyRequest{
		ApprovalId: approvalID, Policy: policyToProto(policy), TreatmentBasisPoints: int32(treatmentBasisPoints),
		AssignmentSalt: assignmentSalt,
	})
	if err != nil {
		return matchapp.PolicyExperiment{}, err
	}
	return experimentFromProto(response.GetExperiment()), nil
}

func (c *Client) RollbackPolicyExperiment(ctx context.Context, experimentID string) error {
	callContext, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()
	callContext = metadata.AppendToOutgoingContext(callContext, "x-agent-control-token", c.controlToken)
	_, err := c.rpc.RollbackPolicyExperiment(callContext, &matchmakingv1.RollbackPolicyExperimentRequest{ExperimentId: experimentID})
	return err
}

func policyFromProto(definition *matchmakingv1.MatchPolicyDefinition) (matchdomain.MatchPolicy, error) {
	policy := matchdomain.MatchPolicy{
		Version: definition.GetVersion(), TeamSize: int(definition.GetTeamSize()), CandidateLimit: int(definition.GetCandidateLimit()),
		TeamAlgorithm: matchdomain.TeamAlgorithm(definition.GetTeamAlgorithm()), BeamWidth: int(definition.GetBeamWidth()),
		SkillWeight: definition.GetSkillWeight(), RoleWeight: definition.GetRoleWeight(), LatencyWeight: definition.GetLatencyWeight(),
		PartyWeight: definition.GetPartyWeight(), WaitWeight: definition.GetWaitWeight(),
		InitialRatingRange: definition.GetInitialRatingRange(), MaxRatingRange: definition.GetMaxRatingRange(),
		RatingExpansionPerSecond: definition.GetRatingExpansionPerSecond(), MaxLatencyMS: int(definition.GetMaxLatencyMs()),
		MinQualityScore: definition.GetMinQualityScore(),
		ReservationTTL:  time.Duration(definition.GetReservationTtlMs()) * time.Millisecond,
		TicketTTL:       time.Duration(definition.GetTicketTtlMs()) * time.Millisecond,
	}
	return policy, policy.Validate()
}

func policyToProto(policy matchdomain.MatchPolicy) *matchmakingv1.MatchPolicyDefinition {
	return &matchmakingv1.MatchPolicyDefinition{
		Version: policy.Version, TeamSize: int32(policy.TeamSize), CandidateLimit: int32(policy.CandidateLimit),
		TeamAlgorithm: string(policy.TeamAlgorithm), BeamWidth: int32(policy.BeamWidth),
		SkillWeight: policy.SkillWeight, RoleWeight: policy.RoleWeight, LatencyWeight: policy.LatencyWeight,
		PartyWeight: policy.PartyWeight, WaitWeight: policy.WaitWeight,
		InitialRatingRange: policy.InitialRatingRange, MaxRatingRange: policy.MaxRatingRange,
		RatingExpansionPerSecond: policy.RatingExpansionPerSecond, MaxLatencyMs: int32(policy.MaxLatencyMS),
		MinQualityScore: policy.MinQualityScore, ReservationTtlMs: policy.ReservationTTL.Milliseconds(),
		TicketTtlMs: policy.TicketTTL.Milliseconds(),
	}
}

func experimentFromProto(experiment *matchmakingv1.PolicyExperiment) matchapp.PolicyExperiment {
	if experiment == nil {
		return matchapp.PolicyExperiment{}
	}
	result := matchapp.PolicyExperiment{
		ID: experiment.GetId(), ControlVersion: experiment.GetControlVersion(), TreatmentVersion: experiment.GetTreatmentVersion(),
		TreatmentBasisPoints: int(experiment.GetTreatmentBasisPoints()), AssignmentSalt: experiment.GetAssignmentSalt(),
	}
	if experiment.GetStartedAt() != nil {
		result.StartedAt = experiment.GetStartedAt().AsTime()
	}
	return result
}

var _ interface {
	OperationalSnapshot(context.Context) (matchapp.OperationalSnapshot, error)
} = (*Client)(nil)
