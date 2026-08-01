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
	}, nil
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
