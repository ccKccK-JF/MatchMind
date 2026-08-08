package playergrpc

import (
	"context"
	"errors"

	playerv1 "github.com/ccKccK-JF/MatchMind/gen/go/matchmind/player/v1"
	"github.com/ccKccK-JF/MatchMind/internal/player/application"
	"github.com/ccKccK-JF/MatchMind/internal/player/domain"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type Server struct {
	playerv1.UnimplementedPlayerServiceServer
	service       *application.Service
	ratingService *application.RatingService
}

func NewServer(service *application.Service, ratingService *application.RatingService) *Server {
	return &Server{service: service, ratingService: ratingService}
}

func (s *Server) CreatePlayer(ctx context.Context, request *playerv1.CreatePlayerRequest) (*playerv1.CreatePlayerResponse, error) {
	if request == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}

	roles, err := rolesFromProto(request.GetPreferredRoles())
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}

	player, err := s.service.CreatePlayer(ctx, application.CreatePlayerCommand{
		ID:             request.GetId(),
		Name:           request.GetName(),
		InitialRating:  request.GetInitialRating(),
		PreferredRoles: roles,
		HomeRegion:     request.GetHomeRegion(),
		RegionLatency:  latencyFromProto(request.GetRegionLatencyMs()),
		BehaviorScore:  request.GetBehaviorScore(),
	})
	if err != nil {
		return nil, playerError(err)
	}

	return &playerv1.CreatePlayerResponse{Player: playerToProto(player)}, nil
}

func (s *Server) GetPlayer(ctx context.Context, request *playerv1.GetPlayerRequest) (*playerv1.GetPlayerResponse, error) {
	if request == nil || request.GetPlayerId() == "" {
		return nil, status.Error(codes.InvalidArgument, "player_id is required")
	}

	player, err := s.service.GetPlayer(ctx, request.GetPlayerId())
	if err != nil {
		return nil, playerError(err)
	}
	return &playerv1.GetPlayerResponse{Player: playerToProto(player)}, nil
}

func (s *Server) UpdateRegionLatency(
	ctx context.Context,
	request *playerv1.UpdateRegionLatencyRequest,
) (*playerv1.UpdateRegionLatencyResponse, error) {
	if request == nil || request.GetPlayerId() == "" {
		return nil, status.Error(codes.InvalidArgument, "player_id is required")
	}
	player, err := s.service.UpdateRegionLatency(ctx, application.UpdateRegionLatencyCommand{
		PlayerID: request.GetPlayerId(), Latency: latencyFromProto(request.GetRegionLatencyMs()),
	})
	if err != nil {
		return nil, playerError(err)
	}
	return &playerv1.UpdateRegionLatencyResponse{Player: playerToProto(player)}, nil
}

func (s *Server) ApplyMatchResult(ctx context.Context, request *playerv1.ApplyMatchResultRequest) (*playerv1.ApplyMatchResultResponse, error) {
	if request == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}
	if s.ratingService == nil {
		return nil, status.Error(codes.Unavailable, "rating service is unavailable")
	}

	outcome, err := outcomeFromProto(request.GetOutcome())
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}
	changes, err := s.ratingService.RecordMatchResult(ctx, application.RecordMatchResultCommand{
		MatchID:        request.GetMatchId(),
		TeamAPlayerIDs: append([]string(nil), request.GetTeamAPlayerIds()...),
		TeamBPlayerIDs: append([]string(nil), request.GetTeamBPlayerIds()...),
		Outcome:        outcome,
		Reason:         request.GetReason(),
	})
	if err != nil {
		return nil, playerError(err)
	}
	return &playerv1.ApplyMatchResultResponse{Changes: ratingChangesToProto(changes)}, nil
}

func (s *Server) GetRatingHistory(ctx context.Context, request *playerv1.GetRatingHistoryRequest) (*playerv1.GetRatingHistoryResponse, error) {
	if request == nil || request.GetPlayerId() == "" {
		return nil, status.Error(codes.InvalidArgument, "player_id is required")
	}
	if s.ratingService == nil {
		return nil, status.Error(codes.Unavailable, "rating service is unavailable")
	}

	changes, err := s.ratingService.History(ctx, request.GetPlayerId())
	if err != nil {
		return nil, playerError(err)
	}
	return &playerv1.GetRatingHistoryResponse{Changes: ratingChangesToProto(changes)}, nil
}

func rolesFromProto(roles []playerv1.Role) ([]domain.Role, error) {
	result := make([]domain.Role, 0, len(roles))
	for _, role := range roles {
		switch role {
		case playerv1.Role_ROLE_VANGUARD:
			result = append(result, domain.RoleVanguard)
		case playerv1.Role_ROLE_ROAMER:
			result = append(result, domain.RoleRoamer)
		case playerv1.Role_ROLE_CORE:
			result = append(result, domain.RoleCore)
		case playerv1.Role_ROLE_RANGED:
			result = append(result, domain.RoleRanged)
		case playerv1.Role_ROLE_SUPPORT:
			result = append(result, domain.RoleSupport)
		default:
			return nil, errors.New("preferred_roles contains an unsupported role")
		}
	}
	return result, nil
}

func roleToProto(role domain.Role) playerv1.Role {
	switch role {
	case domain.RoleVanguard:
		return playerv1.Role_ROLE_VANGUARD
	case domain.RoleRoamer:
		return playerv1.Role_ROLE_ROAMER
	case domain.RoleCore:
		return playerv1.Role_ROLE_CORE
	case domain.RoleRanged:
		return playerv1.Role_ROLE_RANGED
	case domain.RoleSupport:
		return playerv1.Role_ROLE_SUPPORT
	default:
		return playerv1.Role_ROLE_UNSPECIFIED
	}
}

func outcomeFromProto(outcome playerv1.MatchOutcome) (application.MatchOutcome, error) {
	switch outcome {
	case playerv1.MatchOutcome_MATCH_OUTCOME_TEAM_A_WIN:
		return application.MatchOutcomeTeamAWin, nil
	case playerv1.MatchOutcome_MATCH_OUTCOME_TEAM_B_WIN:
		return application.MatchOutcomeTeamBWin, nil
	case playerv1.MatchOutcome_MATCH_OUTCOME_DRAW:
		return application.MatchOutcomeDraw, nil
	default:
		return application.MatchOutcomeUnspecified, errors.New("outcome is required")
	}
}

func playerToProto(player *domain.Player) *playerv1.Player {
	roles := player.PreferredRoles()
	protoRoles := make([]playerv1.Role, 0, len(roles))
	for _, role := range roles {
		protoRoles = append(protoRoles, roleToProto(role))
	}

	latency := make(map[string]int32, len(player.RegionLatency()))
	for region, milliseconds := range player.RegionLatency() {
		latency[region] = int32(milliseconds)
	}

	return &playerv1.Player{
		Id:              player.ID(),
		Name:            player.Name(),
		Rating:          player.Rating(),
		RatingDeviation: player.RatingDeviation(),
		PreferredRoles:  protoRoles,
		HomeRegion:      player.HomeRegion(),
		RegionLatencyMs: latency,
		BehaviorScore:   player.BehaviorScore(),
		CreatedAt:       timestamppb.New(player.CreatedAt()),
	}
}

func latencyFromProto(latency map[string]int32) map[string]int {
	result := make(map[string]int, len(latency))
	for region, milliseconds := range latency {
		result[region] = int(milliseconds)
	}
	return result
}

func ratingChangesToProto(changes []*domain.RatingChange) []*playerv1.RatingChange {
	result := make([]*playerv1.RatingChange, 0, len(changes))
	for _, change := range changes {
		result = append(result, &playerv1.RatingChange{
			PlayerId:  change.PlayerID(),
			MatchId:   change.MatchID(),
			Before:    change.Before(),
			After:     change.After(),
			Delta:     change.Delta(),
			Reason:    change.Reason(),
			CreatedAt: timestamppb.New(change.CreatedAt()),
		})
	}
	return result
}

func playerError(err error) error {
	switch {
	case errors.Is(err, domain.ErrInvalidPlayer):
		return status.Error(codes.InvalidArgument, err.Error())
	case errors.Is(err, application.ErrPlayerAlreadyExists):
		return status.Error(codes.AlreadyExists, err.Error())
	case errors.Is(err, application.ErrPlayerNotFound):
		return status.Error(codes.NotFound, err.Error())
	case errors.Is(err, application.ErrInvalidMatchResult):
		return status.Error(codes.InvalidArgument, err.Error())
	case errors.Is(err, application.ErrRatingConflict):
		return status.Error(codes.Aborted, err.Error())
	case errors.Is(err, context.Canceled):
		return status.Error(codes.Canceled, err.Error())
	case errors.Is(err, context.DeadlineExceeded):
		return status.Error(codes.DeadlineExceeded, err.Error())
	default:
		return status.Error(codes.Internal, "internal player service error")
	}
}
