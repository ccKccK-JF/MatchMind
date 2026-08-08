package httptransport

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	agentv1 "github.com/ccKccK-JF/MatchMind/gen/go/matchmind/agent/v1"
	matchmakingv1 "github.com/ccKccK-JF/MatchMind/gen/go/matchmind/matchmaking/v1"
	playerv1 "github.com/ccKccK-JF/MatchMind/gen/go/matchmind/player/v1"
	simulationv1 "github.com/ccKccK-JF/MatchMind/gen/go/matchmind/simulation/v1"
	platformmetrics "github.com/ccKccK-JF/MatchMind/internal/platform/metrics"
	"github.com/ccKccK-JF/MatchMind/internal/platform/tracing"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

const maxRequestBodyBytes = 8 << 20
const downstreamTimeout = 3 * time.Second
const batchDownstreamTimeout = 30 * time.Second
const agentDownstreamTimeout = 2 * time.Minute

type Server struct {
	players     playerv1.PlayerServiceClient
	matchmaking matchmakingv1.MatchmakingServiceClient
	simulation  simulationv1.SimulationServiceClient
	agent       agentv1.AgentServiceClient
	metrics     *APIMetrics
	handler     http.Handler
}

type APIMetrics struct {
	requests *platformmetrics.Counter
	errors   *platformmetrics.Counter
	duration *platformmetrics.Histogram
}

func NewAPIMetrics(registry *platformmetrics.Registry) *APIMetrics {
	return &APIMetrics{
		requests: registry.NewCounter("api_http_request_total", "Total external HTTP API requests."),
		errors:   registry.NewCounter("api_http_error_total", "Total external HTTP API responses with status 4xx or 5xx."),
		duration: registry.NewHistogram("api_http_request_duration_seconds", "External HTTP API request duration.", []float64{0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5}),
	}
}

func NewServer(
	players playerv1.PlayerServiceClient,
	matchmaking matchmakingv1.MatchmakingServiceClient,
	simulation simulationv1.SimulationServiceClient,
	metrics *APIMetrics,
	agent ...agentv1.AgentServiceClient,
) *Server {
	server := &Server{players: players, matchmaking: matchmaking, simulation: simulation, metrics: metrics}
	if len(agent) > 0 {
		server.agent = agent[0]
	}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/v1/players", server.createPlayer)
	mux.HandleFunc("GET /api/v1/players/{player_id}/rating", server.getPlayerRating)
	mux.HandleFunc("PATCH /api/v1/players/{player_id}/latency", server.updatePlayerLatency)
	mux.HandleFunc("PATCH /api/v1/players/{player_id}/ban", server.setPlayerBan)
	mux.HandleFunc("POST /api/v1/tickets", server.createTicket)
	mux.HandleFunc("GET /api/v1/tickets/{ticket_id}", server.getTicket)
	mux.HandleFunc("DELETE /api/v1/tickets/{ticket_id}", server.cancelTicket)
	mux.HandleFunc("GET /api/v1/matches/{match_id}", server.getMatch)
	mux.HandleFunc("POST /api/v1/matches/{match_id}/simulate", server.simulateMatch)
	mux.HandleFunc("POST /api/v1/matches/{match_id}/replay", server.replayHistoricalMatch)
	mux.HandleFunc("POST /api/v1/simulations/batch", server.simulateBatch)
	mux.HandleFunc("GET /api/v1/analytics/match-quality", server.analyzeMatchQuality)
	mux.HandleFunc("POST /api/v1/agent/runs", server.runAgentAnalysis)
	mux.HandleFunc("GET /api/v1/agent/runs", server.listAgentRuns)
	mux.HandleFunc("GET /api/v1/agent/runs/{run_id}", server.getAgentRun)
	mux.HandleFunc("GET /api/v1/agent/proposals", server.listPolicyProposals)
	mux.HandleFunc("GET /api/v1/agent/proposals/{proposal_id}", server.getPolicyProposal)
	mux.HandleFunc("POST /api/v1/agent/proposals/{proposal_id}/review", server.reviewPolicyProposal)
	mux.HandleFunc("POST /api/v1/agent/proposals/{proposal_id}/activate", server.activatePolicyProposal)
	mux.HandleFunc("POST /api/v1/agent/proposals/{proposal_id}/rollback", server.rollbackPolicyProposal)
	server.handler = mux
	return server
}

func (s *Server) ServeHTTP(response http.ResponseWriter, request *http.Request) {
	startedAt := time.Now()
	timeout := downstreamTimeout
	if request.URL.Path == "/api/v1/simulations/batch" || strings.HasSuffix(request.URL.Path, "/replay") {
		timeout = batchDownstreamTimeout
	}
	if strings.HasPrefix(request.URL.Path, "/api/v1/agent/") {
		timeout = agentDownstreamTimeout
	}
	ctx, cancel := context.WithTimeout(request.Context(), timeout)
	defer cancel()
	ctx, traceID := tracing.WithTraceID(ctx, request.Header.Get(tracing.HeaderName))
	request = request.WithContext(ctx)
	response.Header().Set(tracing.HeaderName, traceID)
	tracked := &statusWriter{ResponseWriter: response, statusCode: http.StatusOK}
	if s.metrics != nil {
		s.metrics.requests.Inc()
		defer func() {
			s.metrics.duration.Observe(time.Since(startedAt).Seconds())
			if tracked.statusCode >= http.StatusBadRequest {
				s.metrics.errors.Inc()
			}
		}()
	}
	slog.InfoContext(ctx, "HTTP API request", "method", request.Method, "path", request.URL.Path, "trace_id", traceID)
	s.handler.ServeHTTP(tracked, request)
}

type createPlayerRequest struct {
	ID             string           `json:"id"`
	Name           string           `json:"name"`
	InitialRating  float64          `json:"initial_rating"`
	PreferredRoles []string         `json:"preferred_roles"`
	HomeRegion     string           `json:"home_region"`
	RegionLatency  map[string]int32 `json:"region_latency"`
	BehaviorScore  float64          `json:"behavior_score"`
}

func (s *Server) createPlayer(response http.ResponseWriter, request *http.Request) {
	var body createPlayerRequest
	if err := decodeJSON(response, request, &body); err != nil {
		writeError(response, err)
		return
	}
	roles, err := parseRoles(body.PreferredRoles)
	if err != nil {
		writeError(response, err)
		return
	}
	result, err := s.players.CreatePlayer(request.Context(), &playerv1.CreatePlayerRequest{
		Id: body.ID, Name: body.Name, InitialRating: body.InitialRating,
		PreferredRoles: roles, HomeRegion: body.HomeRegion,
		RegionLatencyMs: body.RegionLatency, BehaviorScore: body.BehaviorScore,
	})
	if err != nil {
		writeError(response, err)
		return
	}
	writeProto(response, http.StatusCreated, result)
}

type createTicketRequest struct {
	PlayerID       string           `json:"player_id"`
	PartyID        string           `json:"party_id"`
	Mode           string           `json:"mode"`
	ClientVersion  string           `json:"client_version"`
	PreferredRoles []string         `json:"preferred_roles"`
	RegionLatency  map[string]int32 `json:"region_latency"`
	IdempotencyKey string           `json:"idempotency_key"`
}

func (s *Server) createTicket(response http.ResponseWriter, request *http.Request) {
	var body createTicketRequest
	if err := decodeJSON(response, request, &body); err != nil {
		writeError(response, err)
		return
	}
	if _, err := requirePlayer(request, body.PlayerID); err != nil {
		writeError(response, err)
		return
	}
	roles, err := parseRoles(body.PreferredRoles)
	if err != nil {
		writeError(response, err)
		return
	}
	if body.IdempotencyKey == "" {
		body.IdempotencyKey = request.Header.Get("Idempotency-Key")
	}
	result, err := s.matchmaking.CreateTicket(request.Context(), &matchmakingv1.CreateTicketRequest{
		PlayerId: body.PlayerID, PartyId: body.PartyID, Mode: body.Mode,
		ClientVersion: body.ClientVersion, PreferredRoles: roles,
		RegionLatencyMs: body.RegionLatency, IdempotencyKey: body.IdempotencyKey,
	})
	if err != nil {
		writeError(response, err)
		return
	}
	writeProto(response, http.StatusCreated, result)
}

func (s *Server) getTicket(response http.ResponseWriter, request *http.Request) {
	if strings.TrimSpace(request.Header.Get("X-Player-ID")) == "" {
		writeError(response, status.Error(codes.Unauthenticated, "X-Player-ID is required"))
		return
	}
	result, err := s.matchmaking.GetTicket(request.Context(), &matchmakingv1.GetTicketRequest{TicketId: request.PathValue("ticket_id")})
	if err != nil {
		writeError(response, err)
		return
	}
	if _, err := requirePlayer(request, result.GetTicket().GetPlayerId()); err != nil {
		writeError(response, err)
		return
	}
	writeProto(response, http.StatusOK, result)
}

func (s *Server) cancelTicket(response http.ResponseWriter, request *http.Request) {
	playerID := strings.TrimSpace(request.Header.Get("X-Player-ID"))
	if playerID == "" {
		writeError(response, status.Error(codes.Unauthenticated, "X-Player-ID is required"))
		return
	}
	idempotencyKey := request.Header.Get("Idempotency-Key")
	if idempotencyKey == "" {
		idempotencyKey = request.URL.Query().Get("idempotency_key")
	}
	result, err := s.matchmaking.CancelTicket(request.Context(), &matchmakingv1.CancelTicketRequest{
		TicketId: request.PathValue("ticket_id"), PlayerId: playerID, IdempotencyKey: idempotencyKey,
	})
	if err != nil {
		writeError(response, err)
		return
	}
	writeProto(response, http.StatusOK, result)
}

func (s *Server) getMatch(response http.ResponseWriter, request *http.Request) {
	result, err := s.matchmaking.GetMatch(request.Context(), &matchmakingv1.GetMatchRequest{MatchId: request.PathValue("match_id")})
	if err != nil {
		writeError(response, err)
		return
	}
	writeProto(response, http.StatusOK, result)
}

type simulateMatchRequest struct {
	RandomSeed int64 `json:"random_seed"`
}

func (s *Server) simulateMatch(response http.ResponseWriter, request *http.Request) {
	body := simulateMatchRequest{}
	if request.ContentLength != 0 {
		if err := decodeJSON(response, request, &body); err != nil {
			writeError(response, err)
			return
		}
	}
	if body.RandomSeed == 0 {
		body.RandomSeed = time.Now().UnixNano()
	}
	result, err := s.simulation.SimulateMatch(request.Context(), &simulationv1.SimulateMatchRequest{
		MatchId: request.PathValue("match_id"), RandomSeed: body.RandomSeed,
	})
	if err != nil {
		writeError(response, err)
		return
	}
	writeProto(response, http.StatusOK, result)
}

type batchSimulationInput struct {
	CaseID            string  `json:"case_id"`
	RandomSeed        int64   `json:"random_seed"`
	RatingA           float64 `json:"rating_a"`
	RatingB           float64 `json:"rating_b"`
	PredictedWinRateA float64 `json:"predicted_win_rate_a"`
	RoleScore         float64 `json:"role_score"`
	LatencyScore      float64 `json:"latency_score"`
	PartyScore        float64 `json:"party_score"`
}

type batchSimulationRequest struct {
	Inputs      []batchSimulationInput `json:"inputs"`
	Concurrency int32                  `json:"concurrency"`
}

func (s *Server) simulateBatch(response http.ResponseWriter, request *http.Request) {
	var body batchSimulationRequest
	if err := decodeJSON(response, request, &body); err != nil {
		writeError(response, err)
		return
	}
	inputs := make([]*simulationv1.BatchSimulationInput, len(body.Inputs))
	for index, input := range body.Inputs {
		inputs[index] = &simulationv1.BatchSimulationInput{
			CaseId: input.CaseID, RandomSeed: input.RandomSeed,
			RatingA: input.RatingA, RatingB: input.RatingB, PredictedWinRateA: input.PredictedWinRateA,
			RoleScore: input.RoleScore, LatencyScore: input.LatencyScore, PartyScore: input.PartyScore,
		}
	}
	result, err := s.simulation.SimulateBatch(request.Context(), &simulationv1.SimulateBatchRequest{
		Inputs: inputs, Concurrency: body.Concurrency,
	})
	if err != nil {
		writeError(response, err)
		return
	}
	writeProto(response, http.StatusOK, result)
}

type replayHistoricalMatchRequest struct {
	PolicyVersions []string `json:"policy_versions"`
	TicketIDs      []string `json:"ticket_ids"`
}

func (s *Server) replayHistoricalMatch(response http.ResponseWriter, request *http.Request) {
	var body replayHistoricalMatchRequest
	if err := decodeJSON(response, request, &body); err != nil {
		writeError(response, err)
		return
	}
	result, err := s.matchmaking.ReplayHistoricalMatch(request.Context(), &matchmakingv1.ReplayHistoricalMatchRequest{
		MatchId: request.PathValue("match_id"), PolicyVersions: body.PolicyVersions, TicketIds: body.TicketIDs,
	})
	if err != nil {
		writeError(response, err)
		return
	}
	writeProto(response, http.StatusOK, result)
}

func (s *Server) analyzeMatchQuality(response http.ResponseWriter, request *http.Request) {
	query := request.URL.Query()
	limit := int32(0)
	if value := strings.TrimSpace(query.Get("limit")); value != "" {
		parsed, err := strconv.ParseInt(value, 10, 32)
		if err != nil {
			writeError(response, status.Error(codes.InvalidArgument, "limit must be an integer"))
			return
		}
		limit = int32(parsed)
	}
	from, err := queryTimestamp(query.Get("from"), "from")
	if err != nil {
		writeError(response, err)
		return
	}
	to, err := queryTimestamp(query.Get("to"), "to")
	if err != nil {
		writeError(response, err)
		return
	}
	result, err := s.matchmaking.AnalyzeMatchQuality(request.Context(), &matchmakingv1.AnalyzeMatchQualityRequest{
		PolicyVersion: query.Get("policy_version"), Mode: query.Get("mode"),
		ServerRegion: query.Get("server_region"), From: from, To: to, Limit: limit,
	})
	if err != nil {
		writeError(response, err)
		return
	}
	writeProto(response, http.StatusOK, result)
}

func queryTimestamp(value, name string) (*timestamppb.Timestamp, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil, nil
	}
	parsed, err := time.Parse(time.RFC3339, value)
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "%s must use RFC3339 format", name)
	}
	return timestamppb.New(parsed), nil
}

type playerRatingResponse struct {
	PlayerID          string                     `json:"player_id"`
	Rating            float64                    `json:"rating"`
	RatingDeviation   float64                    `json:"rating_deviation"`
	RatingVolatility  float64                    `json:"rating_volatility"`
	Banned            bool                       `json:"banned"`
	History           []*playerv1.RatingChange   `json:"history"`
	RecentMatchIDs    []string                   `json:"recent_match_ids"`
	MatchmakingStatus string                     `json:"matchmaking_status"`
	CurrentTicket     *matchmakingv1.MatchTicket `json:"current_ticket,omitempty"`
}

type updatePlayerLatencyRequest struct {
	RegionLatency map[string]int32 `json:"region_latency"`
}

type setPlayerBanRequest struct {
	Banned bool   `json:"banned"`
	Reason string `json:"reason"`
}

func (s *Server) setPlayerBan(response http.ResponseWriter, request *http.Request) {
	operatorID, err := requireOperator(request, "admin")
	if err != nil {
		writeError(response, err)
		return
	}
	var body setPlayerBanRequest
	if err := decodeJSON(response, request, &body); err != nil {
		writeError(response, err)
		return
	}
	result, err := s.players.SetPlayerBan(request.Context(), &playerv1.SetPlayerBanRequest{
		PlayerId: request.PathValue("player_id"), Banned: body.Banned,
		Reason: body.Reason, OperatorId: operatorID,
	})
	if err != nil {
		writeError(response, err)
		return
	}
	writeProto(response, http.StatusOK, result)
}

func (s *Server) updatePlayerLatency(response http.ResponseWriter, request *http.Request) {
	playerID := request.PathValue("player_id")
	if _, err := requirePlayer(request, playerID); err != nil {
		writeError(response, err)
		return
	}
	var body updatePlayerLatencyRequest
	if err := decodeJSON(response, request, &body); err != nil {
		writeError(response, err)
		return
	}
	result, err := s.players.UpdateRegionLatency(request.Context(), &playerv1.UpdateRegionLatencyRequest{
		PlayerId: playerID, RegionLatencyMs: body.RegionLatency,
	})
	if err != nil {
		writeError(response, err)
		return
	}
	writeProto(response, http.StatusOK, result)
}

func (s *Server) getPlayerRating(response http.ResponseWriter, request *http.Request) {
	playerID := request.PathValue("player_id")
	playerResult, err := s.players.GetPlayer(request.Context(), &playerv1.GetPlayerRequest{PlayerId: playerID})
	if err != nil {
		writeError(response, err)
		return
	}
	historyResult, err := s.players.GetRatingHistory(request.Context(), &playerv1.GetRatingHistoryRequest{PlayerId: playerID})
	if err != nil {
		writeError(response, err)
		return
	}
	activeTicket, err := s.matchmaking.GetActiveTicketForPlayer(request.Context(), &matchmakingv1.GetActiveTicketForPlayerRequest{PlayerId: playerID})
	if err != nil {
		writeError(response, err)
		return
	}
	matchmakingStatus := "IDLE"
	if activeTicket.GetFound() && activeTicket.GetTicket() != nil {
		matchmakingStatus = strings.TrimPrefix(activeTicket.GetTicket().GetState().String(), "TICKET_STATE_")
	}
	writeJSON(response, http.StatusOK, playerRatingResponse{
		PlayerID: playerResult.Player.Id, Rating: playerResult.Player.Rating,
		RatingDeviation:  playerResult.Player.RatingDeviation,
		RatingVolatility: playerResult.Player.RatingVolatility, Banned: playerResult.Player.Banned,
		History:        historyResult.Changes,
		RecentMatchIDs: recentMatchIDs(historyResult.Changes, 10), MatchmakingStatus: matchmakingStatus,
		CurrentTicket: activeTicket.GetTicket(),
	})
}

func recentMatchIDs(changes []*playerv1.RatingChange, limit int) []string {
	if limit <= 0 || limit > len(changes) {
		limit = len(changes)
	}
	result := make([]string, 0, limit)
	seen := make(map[string]struct{}, limit)
	for index := len(changes) - 1; index >= 0 && len(result) < limit; index-- {
		matchID := strings.TrimSpace(changes[index].GetMatchId())
		if matchID == "" {
			continue
		}
		if _, duplicate := seen[matchID]; duplicate {
			continue
		}
		seen[matchID] = struct{}{}
		result = append(result, matchID)
	}
	return result
}

type runAgentAnalysisRequest struct {
	BasePolicyVersion string `json:"base_policy_version"`
	Mode              string `json:"mode"`
	ServerRegion      string `json:"server_region"`
	From              string `json:"from"`
	To                string `json:"to"`
	HistoricalLimit   int32  `json:"historical_limit"`
}

func (s *Server) runAgentAnalysis(response http.ResponseWriter, request *http.Request) {
	operatorID, err := requireOperator(request, "analyst", "admin")
	if err != nil {
		writeError(response, err)
		return
	}
	if s.agent == nil {
		writeError(response, status.Error(codes.Unavailable, "Agent service is unavailable"))
		return
	}
	var body runAgentAnalysisRequest
	if err := decodeJSON(response, request, &body); err != nil {
		writeError(response, err)
		return
	}
	from, err := queryTimestamp(body.From, "from")
	if err != nil {
		writeError(response, err)
		return
	}
	to, err := queryTimestamp(body.To, "to")
	if err != nil {
		writeError(response, err)
		return
	}
	result, err := s.agent.RunAnalysis(request.Context(), &agentv1.RunAnalysisRequest{
		RequestedBy: operatorID, BasePolicyVersion: body.BasePolicyVersion,
		Mode: body.Mode, ServerRegion: body.ServerRegion, From: from, To: to,
		HistoricalLimit: body.HistoricalLimit,
	})
	if err != nil {
		writeError(response, err)
		return
	}
	writeProto(response, http.StatusCreated, result)
}

func (s *Server) getAgentRun(response http.ResponseWriter, request *http.Request) {
	if _, err := requireOperator(request, "analyst", "reviewer", "admin"); err != nil {
		writeError(response, err)
		return
	}
	if s.agent == nil {
		writeError(response, status.Error(codes.Unavailable, "Agent service is unavailable"))
		return
	}
	result, err := s.agent.GetRun(request.Context(), &agentv1.GetRunRequest{RunId: request.PathValue("run_id")})
	if err != nil {
		writeError(response, err)
		return
	}
	writeProto(response, http.StatusOK, result)
}

func (s *Server) listAgentRuns(response http.ResponseWriter, request *http.Request) {
	if _, err := requireOperator(request, "analyst", "reviewer", "admin"); err != nil {
		writeError(response, err)
		return
	}
	if s.agent == nil {
		writeError(response, status.Error(codes.Unavailable, "Agent service is unavailable"))
		return
	}
	limit, err := queryLimit(request, 1000)
	if err != nil {
		writeError(response, err)
		return
	}
	result, err := s.agent.ListRuns(request.Context(), &agentv1.ListRunsRequest{Limit: limit})
	if err != nil {
		writeError(response, err)
		return
	}
	writeProto(response, http.StatusOK, result)
}

func (s *Server) getPolicyProposal(response http.ResponseWriter, request *http.Request) {
	if _, err := requireOperator(request, "analyst", "reviewer", "admin"); err != nil {
		writeError(response, err)
		return
	}
	if s.agent == nil {
		writeError(response, status.Error(codes.Unavailable, "Agent service is unavailable"))
		return
	}
	result, err := s.agent.GetProposal(request.Context(), &agentv1.GetProposalRequest{ProposalId: request.PathValue("proposal_id")})
	if err != nil {
		writeError(response, err)
		return
	}
	writeProto(response, http.StatusOK, result)
}

func (s *Server) listPolicyProposals(response http.ResponseWriter, request *http.Request) {
	if _, err := requireOperator(request, "analyst", "reviewer", "admin"); err != nil {
		writeError(response, err)
		return
	}
	if s.agent == nil {
		writeError(response, status.Error(codes.Unavailable, "Agent service is unavailable"))
		return
	}
	limit, err := queryLimit(request, 1000)
	if err != nil {
		writeError(response, err)
		return
	}
	result, err := s.agent.ListProposals(request.Context(), &agentv1.ListProposalsRequest{Limit: limit})
	if err != nil {
		writeError(response, err)
		return
	}
	writeProto(response, http.StatusOK, result)
}

type reviewPolicyProposalRequest struct {
	Decision string `json:"decision"`
	Reason   string `json:"reason"`
}

func (s *Server) reviewPolicyProposal(response http.ResponseWriter, request *http.Request) {
	reviewerID, err := requireOperator(request, "reviewer", "admin")
	if err != nil {
		writeError(response, err)
		return
	}
	if s.agent == nil {
		writeError(response, status.Error(codes.Unavailable, "Agent service is unavailable"))
		return
	}
	var body reviewPolicyProposalRequest
	if err := decodeJSON(response, request, &body); err != nil {
		writeError(response, err)
		return
	}
	decision := strings.ToLower(strings.TrimSpace(body.Decision))
	if decision != "approve" && decision != "reject" {
		writeError(response, status.Error(codes.InvalidArgument, "decision must be approve or reject"))
		return
	}
	result, err := s.agent.ReviewProposal(request.Context(), &agentv1.ReviewProposalRequest{
		ProposalId: request.PathValue("proposal_id"), ReviewerId: reviewerID,
		Reason: body.Reason, Approve: decision == "approve",
	})
	if err != nil {
		writeError(response, err)
		return
	}
	writeProto(response, http.StatusOK, result)
}

type activatePolicyProposalRequest struct {
	TreatmentBasisPoints int32  `json:"treatment_basis_points"`
	AssignmentSalt       string `json:"assignment_salt"`
}

func (s *Server) activatePolicyProposal(response http.ResponseWriter, request *http.Request) {
	operatorID, err := requireOperator(request, "admin")
	if err != nil {
		writeError(response, err)
		return
	}
	if s.agent == nil {
		writeError(response, status.Error(codes.Unavailable, "Agent service is unavailable"))
		return
	}
	var body activatePolicyProposalRequest
	if err := decodeJSON(response, request, &body); err != nil {
		writeError(response, err)
		return
	}
	result, err := s.agent.ActivateProposal(request.Context(), &agentv1.ActivateProposalRequest{
		ProposalId: request.PathValue("proposal_id"), OperatorId: operatorID,
		TreatmentBasisPoints: body.TreatmentBasisPoints, AssignmentSalt: body.AssignmentSalt,
	})
	if err != nil {
		writeError(response, err)
		return
	}
	writeProto(response, http.StatusOK, result)
}

func (s *Server) rollbackPolicyProposal(response http.ResponseWriter, request *http.Request) {
	operatorID, err := requireOperator(request, "admin")
	if err != nil {
		writeError(response, err)
		return
	}
	if s.agent == nil {
		writeError(response, status.Error(codes.Unavailable, "Agent service is unavailable"))
		return
	}
	result, err := s.agent.RollbackProposal(request.Context(), &agentv1.RollbackProposalRequest{
		ProposalId: request.PathValue("proposal_id"), OperatorId: operatorID,
	})
	if err != nil {
		writeError(response, err)
		return
	}
	writeProto(response, http.StatusOK, result)
}

func requireOperator(request *http.Request, allowedRoles ...string) (string, error) {
	operatorID := strings.TrimSpace(request.Header.Get("X-Operator-ID"))
	role := strings.ToLower(strings.TrimSpace(request.Header.Get("X-Operator-Role")))
	if operatorID == "" || role == "" {
		return "", status.Error(codes.Unauthenticated, "X-Operator-ID and X-Operator-Role are required")
	}
	for _, allowed := range allowedRoles {
		if role == allowed {
			return operatorID, nil
		}
	}
	return "", status.Error(codes.PermissionDenied, "operator role is not permitted")
}

func requirePlayer(request *http.Request, expectedPlayerID string) (string, error) {
	playerID := strings.TrimSpace(request.Header.Get("X-Player-ID"))
	if playerID == "" {
		return "", status.Error(codes.Unauthenticated, "X-Player-ID is required")
	}
	if playerID != strings.TrimSpace(expectedPlayerID) {
		return "", status.Error(codes.PermissionDenied, "player is not permitted to access this resource")
	}
	return playerID, nil
}

func queryLimit(request *http.Request, maximum int32) (int32, error) {
	value := strings.TrimSpace(request.URL.Query().Get("limit"))
	if value == "" {
		return 0, nil
	}
	parsed, err := strconv.ParseInt(value, 10, 32)
	if err != nil || parsed < 1 || parsed > int64(maximum) {
		return 0, status.Errorf(codes.InvalidArgument, "limit must be between 1 and %d", maximum)
	}
	return int32(parsed), nil
}

func parseRoles(values []string) ([]playerv1.Role, error) {
	roles := make([]playerv1.Role, 0, len(values))
	for _, value := range values {
		normalized := strings.ToUpper(strings.TrimSpace(value))
		normalized = strings.TrimPrefix(normalized, "ROLE_")
		role, exists := playerv1.Role_value["ROLE_"+normalized]
		if !exists || role == int32(playerv1.Role_ROLE_UNSPECIFIED) {
			return nil, status.Errorf(codes.InvalidArgument, "unknown preferred role %q", value)
		}
		roles = append(roles, playerv1.Role(role))
	}
	return roles, nil
}

func decodeJSON(response http.ResponseWriter, request *http.Request, target any) error {
	request.Body = http.MaxBytesReader(response, request.Body, maxRequestBodyBytes)
	decoder := json.NewDecoder(request.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return status.Errorf(codes.InvalidArgument, "invalid JSON body: %v", err)
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return status.Error(codes.InvalidArgument, "JSON body must contain one object")
	}
	return nil
}

func writeProto(response http.ResponseWriter, statusCode int, message proto.Message) {
	data, err := (protojson.MarshalOptions{UseProtoNames: true, EmitUnpopulated: true}).Marshal(message)
	if err != nil {
		writeError(response, status.Error(codes.Internal, "encode response"))
		return
	}
	response.Header().Set("Content-Type", "application/json")
	response.WriteHeader(statusCode)
	_, _ = response.Write(append(data, '\n'))
}

func writeJSON(response http.ResponseWriter, statusCode int, value any) {
	response.Header().Set("Content-Type", "application/json")
	response.WriteHeader(statusCode)
	_ = json.NewEncoder(response).Encode(value)
}

func writeError(response http.ResponseWriter, err error) {
	code := status.Code(err)
	statusCode := http.StatusInternalServerError
	switch code {
	case codes.InvalidArgument, codes.FailedPrecondition, codes.OutOfRange:
		statusCode = http.StatusBadRequest
	case codes.Unauthenticated:
		statusCode = http.StatusUnauthorized
	case codes.PermissionDenied:
		statusCode = http.StatusForbidden
	case codes.NotFound:
		statusCode = http.StatusNotFound
	case codes.AlreadyExists, codes.Aborted:
		statusCode = http.StatusConflict
	case codes.ResourceExhausted:
		statusCode = http.StatusTooManyRequests
	case codes.Unavailable:
		statusCode = http.StatusServiceUnavailable
	case codes.DeadlineExceeded:
		statusCode = http.StatusGatewayTimeout
	}
	message := status.Convert(err).Message()
	if code == codes.Unknown {
		message = "internal server error"
	}
	writeJSON(response, statusCode, map[string]any{
		"error": map[string]string{"code": strings.ToLower(code.String()), "message": message},
	})
}

type statusWriter struct {
	http.ResponseWriter
	statusCode int
}

func (w *statusWriter) WriteHeader(statusCode int) {
	w.statusCode = statusCode
	w.ResponseWriter.WriteHeader(statusCode)
}

func (w *statusWriter) Write(data []byte) (int, error) {
	if w.statusCode == 0 {
		w.statusCode = http.StatusOK
	}
	return w.ResponseWriter.Write(data)
}
