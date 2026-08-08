package simulationgrpc

import (
	"context"
	"errors"

	simulationv1 "github.com/ccKccK-JF/MatchMind/gen/go/matchmind/simulation/v1"
	"github.com/ccKccK-JF/MatchMind/internal/simulation/application"
	simulationdomain "github.com/ccKccK-JF/MatchMind/internal/simulation/domain"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type Server struct {
	simulationv1.UnimplementedSimulationServiceServer
	service *application.Service
}

func NewServer(service *application.Service) *Server {
	return &Server{service: service}
}

func (s *Server) SimulateMatch(ctx context.Context, request *simulationv1.SimulateMatchRequest) (*simulationv1.SimulateMatchResponse, error) {
	if request == nil || request.GetMatchId() == "" {
		return nil, status.Error(codes.InvalidArgument, "match_id is required")
	}
	result, err := s.service.SimulateMatch(ctx, request.GetMatchId(), request.GetRandomSeed())
	if err != nil {
		return nil, simulationError(err)
	}
	return resultToProto(result), nil
}

func (s *Server) SimulateBatch(ctx context.Context, request *simulationv1.SimulateBatchRequest) (*simulationv1.SimulateBatchResponse, error) {
	if request == nil || len(request.GetInputs()) == 0 || len(request.GetInputs()) > application.MaxBatchSimulationCases {
		return nil, status.Error(codes.InvalidArgument, "between 1 and 10000 simulation inputs are required")
	}
	inputs := make([]simulationdomain.Input, len(request.GetInputs()))
	for index, input := range request.GetInputs() {
		if input == nil {
			return nil, status.Error(codes.InvalidArgument, "simulation input is required")
		}
		inputs[index] = simulationdomain.Input{
			MatchID: input.GetCaseId(), RandomSeed: input.GetRandomSeed(),
			RatingA: input.GetRatingA(), RatingB: input.GetRatingB(),
			PredictedWinRateA: input.GetPredictedWinRateA(), RoleScore: input.GetRoleScore(),
			LatencyScore: input.GetLatencyScore(), PartyScore: input.GetPartyScore(),
		}
	}
	report, err := s.service.SimulateBatch(ctx, inputs, int(request.GetConcurrency()))
	if err != nil {
		return nil, simulationError(err)
	}
	results := make([]*simulationv1.SimulateMatchResponse, len(report.Results))
	for index, result := range report.Results {
		results[index] = resultToProto(result)
	}
	return &simulationv1.SimulateBatchResponse{
		Results: results, SimulationCount: int32(report.SimulationCount),
		TeamAWinRate: report.TeamAWinRate, AverageDurationSeconds: report.AverageDuration,
		AverageActualQualityScore: report.AverageActualQuality, OneSidedRate: report.OneSidedRate,
		AfkRate: report.AFKRate, SurrenderRate: report.SurrenderRate,
	}, nil
}

func resultToProto(result simulationdomain.Result) *simulationv1.SimulateMatchResponse {
	winningTeam := simulationv1.WinningTeam_WINNING_TEAM_UNSPECIFIED
	if result.WinningTeam == simulationdomain.WinningTeamA {
		winningTeam = simulationv1.WinningTeam_WINNING_TEAM_A
	} else if result.WinningTeam == simulationdomain.WinningTeamB {
		winningTeam = simulationv1.WinningTeam_WINNING_TEAM_B
	}
	return &simulationv1.SimulateMatchResponse{
		MatchId: result.MatchID, WinningTeam: winningTeam, DurationSeconds: int32(result.DurationSeconds),
		ScoreA: int32(result.ScoreA), ScoreB: int32(result.ScoreB), MaxAdvantage: result.MaxAdvantage,
		HasAfk: result.HasAFK, Surrendered: result.Surrendered, OneSided: result.OneSided,
		ActualQualityScore: result.ActualQualityScore, RandomSeed: result.RandomSeed,
	}
}

func simulationError(err error) error {
	switch {
	case errors.Is(err, simulationdomain.ErrInvalidSimulation):
		return status.Error(codes.InvalidArgument, err.Error())
	case errors.Is(err, application.ErrMatchNotFound):
		return status.Error(codes.NotFound, err.Error())
	case errors.Is(err, application.ErrMatchNotReady):
		return status.Error(codes.FailedPrecondition, err.Error())
	case errors.Is(err, context.Canceled):
		return status.Error(codes.Canceled, err.Error())
	case errors.Is(err, context.DeadlineExceeded):
		return status.Error(codes.DeadlineExceeded, err.Error())
	default:
		return status.Error(codes.Internal, "internal simulation service error")
	}
}
