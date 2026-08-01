package matchmakinggrpc

import (
	"context"
	"errors"

	matchmakingv1 "github.com/ccKccK-JF/MatchMind/gen/go/matchmind/matchmaking/v1"
	playerv1 "github.com/ccKccK-JF/MatchMind/gen/go/matchmind/player/v1"
	"github.com/ccKccK-JF/MatchMind/internal/matchmaking/application"
	"github.com/ccKccK-JF/MatchMind/internal/matchmaking/domain"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type Server struct {
	matchmakingv1.UnimplementedMatchmakingServiceServer
	service      *application.TicketService
	matchService *application.MatchService
}

func NewServer(service *application.TicketService, matchService *application.MatchService) *Server {
	return &Server{service: service, matchService: matchService}
}

func (s *Server) CreateTicket(ctx context.Context, request *matchmakingv1.CreateTicketRequest) (*matchmakingv1.CreateTicketResponse, error) {
	if request == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}
	roles, err := rolesFromProto(request.GetPreferredRoles())
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}
	ticket, err := s.service.CreateTicket(ctx, application.CreateTicketCommand{
		PlayerID:       request.GetPlayerId(),
		PartyID:        request.GetPartyId(),
		Mode:           request.GetMode(),
		ClientVersion:  request.GetClientVersion(),
		PreferredRoles: roles,
		RegionLatency:  latencyFromProto(request.GetRegionLatencyMs()),
		IdempotencyKey: request.GetIdempotencyKey(),
	})
	if err != nil {
		return nil, ticketError(err)
	}
	return &matchmakingv1.CreateTicketResponse{Ticket: ticketToProto(ticket)}, nil
}

func (s *Server) GetTicket(ctx context.Context, request *matchmakingv1.GetTicketRequest) (*matchmakingv1.GetTicketResponse, error) {
	if request == nil || request.GetTicketId() == "" {
		return nil, status.Error(codes.InvalidArgument, "ticket_id is required")
	}
	ticket, err := s.service.GetTicket(ctx, request.GetTicketId())
	if err != nil {
		return nil, ticketError(err)
	}
	return &matchmakingv1.GetTicketResponse{Ticket: ticketToProto(ticket)}, nil
}

func (s *Server) CancelTicket(ctx context.Context, request *matchmakingv1.CancelTicketRequest) (*matchmakingv1.CancelTicketResponse, error) {
	if request == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}
	ticket, err := s.service.CancelTicket(
		ctx, request.GetTicketId(), request.GetPlayerId(), request.GetIdempotencyKey(),
	)
	if err != nil {
		return nil, ticketError(err)
	}
	return &matchmakingv1.CancelTicketResponse{Ticket: ticketToProto(ticket)}, nil
}

func (s *Server) GetMatch(ctx context.Context, request *matchmakingv1.GetMatchRequest) (*matchmakingv1.GetMatchResponse, error) {
	if request == nil || request.GetMatchId() == "" {
		return nil, status.Error(codes.InvalidArgument, "match_id is required")
	}
	if s.matchService == nil {
		return nil, status.Error(codes.Unavailable, "match service is unavailable")
	}
	match, err := s.matchService.GetMatch(ctx, request.GetMatchId())
	if err != nil {
		return nil, ticketError(err)
	}
	return &matchmakingv1.GetMatchResponse{Match: matchToProto(match)}, nil
}

func (s *Server) StartMatch(ctx context.Context, request *matchmakingv1.StartMatchRequest) (*matchmakingv1.StartMatchResponse, error) {
	if request == nil || request.GetMatchId() == "" {
		return nil, status.Error(codes.InvalidArgument, "match_id is required")
	}
	if s.matchService == nil {
		return nil, status.Error(codes.Unavailable, "match service is unavailable")
	}
	match, err := s.matchService.StartMatch(ctx, request.GetMatchId())
	if err != nil {
		return nil, ticketError(err)
	}
	return &matchmakingv1.StartMatchResponse{Match: matchToProto(match)}, nil
}

func (s *Server) CompleteMatch(ctx context.Context, request *matchmakingv1.CompleteMatchRequest) (*matchmakingv1.CompleteMatchResponse, error) {
	if request == nil || request.GetMatchId() == "" || request.GetResult() == nil {
		return nil, status.Error(codes.InvalidArgument, "match_id and result are required")
	}
	if s.matchService == nil {
		return nil, status.Error(codes.Unavailable, "match service is unavailable")
	}
	result, err := matchResultFromProto(request.GetResult())
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}
	match, err := s.matchService.CompleteMatch(ctx, request.GetMatchId(), result)
	if err != nil {
		return nil, ticketError(err)
	}
	return &matchmakingv1.CompleteMatchResponse{Match: matchToProto(match)}, nil
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

func latencyFromProto(latency map[string]int32) map[string]int {
	result := make(map[string]int, len(latency))
	for region, milliseconds := range latency {
		result[region] = int(milliseconds)
	}
	return result
}

func ticketToProto(ticket *domain.MatchTicket) *matchmakingv1.MatchTicket {
	roles := make([]playerv1.Role, 0, len(ticket.PreferredRoles()))
	for _, role := range ticket.PreferredRoles() {
		roles = append(roles, roleToProto(role))
	}
	latency := make(map[string]int32, len(ticket.RegionLatency()))
	for region, milliseconds := range ticket.RegionLatency() {
		latency[region] = int32(milliseconds)
	}
	result := &matchmakingv1.MatchTicket{
		Id:              ticket.ID(),
		PlayerId:        ticket.PlayerID(),
		PartyId:         ticket.PartyID(),
		Mode:            ticket.Mode(),
		ClientVersion:   ticket.ClientVersion(),
		Region:          ticket.Region(),
		Rating:          ticket.Rating(),
		State:           stateToProto(ticket.State()),
		PreferredRoles:  roles,
		RegionLatencyMs: latency,
		CreatedAt:       timestamppb.New(ticket.CreatedAt()),
		ReservationId:   ticket.ReservationID(),
		MatchId:         ticket.MatchID(),
	}
	if !ticket.ReservationExpiresAt().IsZero() {
		result.ReservationExpiresAt = timestamppb.New(ticket.ReservationExpiresAt())
	}
	return result
}

func matchToProto(match *domain.Match) *matchmakingv1.Match {
	quality := match.Quality()
	response := &matchmakingv1.Match{
		Id:                match.ID(),
		Mode:              match.Mode(),
		TeamA:             matchTeamToProto(match.TeamA()),
		TeamB:             matchTeamToProto(match.TeamB()),
		State:             matchStateToProto(match.State()),
		ServerRegion:      match.ServerRegion(),
		PolicyVersion:     match.PolicyVersion(),
		PredictedWinRateA: quality.PredictedWinRateA,
		QualityScore:      quality.TotalScore,
		CreatedAt:         timestamppb.New(match.CreatedAt()),
		ServerAddress:     match.ServerAddress(),
		ConnectionToken:   match.ConnectionToken(),
		SkillScore:        quality.SkillScore,
		RoleScore:         quality.RoleScore,
		LatencyScore:      quality.LatencyScore,
		PartyScore:        quality.PartyScore,
		WaitScore:         quality.WaitScore,
		QualityReasons:    append([]string(nil), quality.Reasons...),
	}
	if result, exists := match.Result(); exists {
		response.Result = matchResultToProto(result)
	}
	return response
}

func matchResultFromProto(result *matchmakingv1.MatchResult) (domain.MatchResult, error) {
	winningTeam := domain.WinningTeam("")
	switch result.GetWinningTeam() {
	case matchmakingv1.WinningTeam_WINNING_TEAM_A:
		winningTeam = domain.WinningTeamA
	case matchmakingv1.WinningTeam_WINNING_TEAM_B:
		winningTeam = domain.WinningTeamB
	default:
		return domain.MatchResult{}, errors.New("winning_team is required")
	}
	return domain.MatchResult{
		WinningTeam: winningTeam, RandomSeed: result.GetRandomSeed(), DurationSeconds: int(result.GetDurationSeconds()),
		ScoreA: int(result.GetScoreA()), ScoreB: int(result.GetScoreB()), MaxAdvantage: result.GetMaxAdvantage(),
		HasAFK: result.GetHasAfk(), Surrendered: result.GetSurrendered(), OneSided: result.GetOneSided(),
		ActualQualityScore: result.GetActualQualityScore(),
	}, nil
}

func matchResultToProto(result domain.MatchResult) *matchmakingv1.MatchResult {
	winningTeam := matchmakingv1.WinningTeam_WINNING_TEAM_UNSPECIFIED
	if result.WinningTeam == domain.WinningTeamA {
		winningTeam = matchmakingv1.WinningTeam_WINNING_TEAM_A
	} else if result.WinningTeam == domain.WinningTeamB {
		winningTeam = matchmakingv1.WinningTeam_WINNING_TEAM_B
	}
	return &matchmakingv1.MatchResult{
		WinningTeam: winningTeam, RandomSeed: result.RandomSeed, DurationSeconds: int32(result.DurationSeconds),
		ScoreA: int32(result.ScoreA), ScoreB: int32(result.ScoreB), MaxAdvantage: result.MaxAdvantage,
		HasAfk: result.HasAFK, Surrendered: result.Surrendered, OneSided: result.OneSided,
		ActualQualityScore: result.ActualQualityScore,
	}
}

func matchTeamToProto(team domain.MatchTeam) *matchmakingv1.Team {
	result := &matchmakingv1.Team{Id: team.ID, AverageRating: team.AverageRating}
	for _, player := range team.Players {
		result.PlayerIds = append(result.PlayerIds, player.PlayerID)
		result.PlayerDetails = append(result.PlayerDetails, &matchmakingv1.TeamPlayer{
			PlayerId: player.PlayerID,
			TicketId: player.TicketID,
			PartyId:  player.PartyID,
			Role:     roleToProto(player.Role),
			Rating:   player.Rating,
		})
	}
	return result
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

func stateToProto(state domain.TicketState) matchmakingv1.TicketState {
	switch state {
	case domain.TicketStateCreated:
		return matchmakingv1.TicketState_TICKET_STATE_CREATED
	case domain.TicketStateQueued:
		return matchmakingv1.TicketState_TICKET_STATE_QUEUED
	case domain.TicketStateReserved:
		return matchmakingv1.TicketState_TICKET_STATE_RESERVED
	case domain.TicketStateAssigned:
		return matchmakingv1.TicketState_TICKET_STATE_ASSIGNED
	case domain.TicketStateCancelled:
		return matchmakingv1.TicketState_TICKET_STATE_CANCELLED
	case domain.TicketStateExpired:
		return matchmakingv1.TicketState_TICKET_STATE_EXPIRED
	case domain.TicketStateFailed:
		return matchmakingv1.TicketState_TICKET_STATE_FAILED
	default:
		return matchmakingv1.TicketState_TICKET_STATE_UNSPECIFIED
	}
}

func matchStateToProto(state domain.MatchState) matchmakingv1.MatchState {
	switch state {
	case domain.MatchStateCreated:
		return matchmakingv1.MatchState_MATCH_STATE_CREATED
	case domain.MatchStateAllocating:
		return matchmakingv1.MatchState_MATCH_STATE_ALLOCATING
	case domain.MatchStateReady:
		return matchmakingv1.MatchState_MATCH_STATE_READY
	case domain.MatchStateRunning:
		return matchmakingv1.MatchState_MATCH_STATE_RUNNING
	case domain.MatchStateFinished:
		return matchmakingv1.MatchState_MATCH_STATE_FINISHED
	case domain.MatchStateFailed:
		return matchmakingv1.MatchState_MATCH_STATE_FAILED
	default:
		return matchmakingv1.MatchState_MATCH_STATE_UNSPECIFIED
	}
}

func ticketError(err error) error {
	switch {
	case errors.Is(err, domain.ErrInvalidTicket), errors.Is(err, application.ErrIdempotencyKeyRequired):
		return status.Error(codes.InvalidArgument, err.Error())
	case errors.Is(err, application.ErrTicketNotFound), errors.Is(err, application.ErrPlayerNotFound):
		return status.Error(codes.NotFound, err.Error())
	case errors.Is(err, application.ErrMatchNotFound):
		return status.Error(codes.NotFound, err.Error())
	case errors.Is(err, application.ErrActiveTicketExists):
		return status.Error(codes.AlreadyExists, err.Error())
	case errors.Is(err, application.ErrTicketForbidden):
		return status.Error(codes.PermissionDenied, err.Error())
	case errors.Is(err, domain.ErrIllegalStateTransition):
		return status.Error(codes.FailedPrecondition, err.Error())
	case errors.Is(err, domain.ErrInvalidMatch):
		return status.Error(codes.InvalidArgument, err.Error())
	case errors.Is(err, domain.ErrIllegalMatchTransition):
		return status.Error(codes.FailedPrecondition, err.Error())
	case errors.Is(err, application.ErrPlayerServiceUnavailable):
		return status.Error(codes.Unavailable, err.Error())
	case errors.Is(err, context.Canceled):
		return status.Error(codes.Canceled, err.Error())
	case errors.Is(err, context.DeadlineExceeded):
		return status.Error(codes.DeadlineExceeded, err.Error())
	default:
		return status.Error(codes.Internal, "internal matchmaking service error")
	}
}
