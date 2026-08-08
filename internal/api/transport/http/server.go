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

	matchmakingv1 "github.com/ccKccK-JF/MatchMind/gen/go/matchmind/matchmaking/v1"
	playerv1 "github.com/ccKccK-JF/MatchMind/gen/go/matchmind/player/v1"
	simulationv1 "github.com/ccKccK-JF/MatchMind/gen/go/matchmind/simulation/v1"
	platformid "github.com/ccKccK-JF/MatchMind/internal/platform/id"
	platformmetrics "github.com/ccKccK-JF/MatchMind/internal/platform/metrics"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

const maxRequestBodyBytes = 8 << 20
const downstreamTimeout = 3 * time.Second
const batchDownstreamTimeout = 30 * time.Second

type Server struct {
	players     playerv1.PlayerServiceClient
	matchmaking matchmakingv1.MatchmakingServiceClient
	simulation  simulationv1.SimulationServiceClient
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
) *Server {
	server := &Server{players: players, matchmaking: matchmaking, simulation: simulation, metrics: metrics}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/v1/players", server.createPlayer)
	mux.HandleFunc("GET /api/v1/players/{player_id}/rating", server.getPlayerRating)
	mux.HandleFunc("POST /api/v1/tickets", server.createTicket)
	mux.HandleFunc("GET /api/v1/tickets/{ticket_id}", server.getTicket)
	mux.HandleFunc("DELETE /api/v1/tickets/{ticket_id}", server.cancelTicket)
	mux.HandleFunc("GET /api/v1/matches/{match_id}", server.getMatch)
	mux.HandleFunc("POST /api/v1/matches/{match_id}/simulate", server.simulateMatch)
	mux.HandleFunc("POST /api/v1/matches/{match_id}/replay", server.replayHistoricalMatch)
	mux.HandleFunc("POST /api/v1/simulations/batch", server.simulateBatch)
	mux.HandleFunc("GET /api/v1/analytics/match-quality", server.analyzeMatchQuality)
	server.handler = mux
	return server
}

func (s *Server) ServeHTTP(response http.ResponseWriter, request *http.Request) {
	startedAt := time.Now()
	timeout := downstreamTimeout
	if request.URL.Path == "/api/v1/simulations/batch" || strings.HasSuffix(request.URL.Path, "/replay") {
		timeout = batchDownstreamTimeout
	}
	ctx, cancel := context.WithTimeout(request.Context(), timeout)
	defer cancel()
	request = request.WithContext(ctx)
	traceID := strings.TrimSpace(request.Header.Get("X-Trace-ID"))
	if traceID == "" {
		traceID, _ = platformid.UUID()
	}
	response.Header().Set("X-Trace-ID", traceID)
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
	slog.Info("HTTP API request", "method", request.Method, "path", request.URL.Path, "trace_id", traceID)
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
	result, err := s.matchmaking.GetTicket(request.Context(), &matchmakingv1.GetTicketRequest{TicketId: request.PathValue("ticket_id")})
	if err != nil {
		writeError(response, err)
		return
	}
	writeProto(response, http.StatusOK, result)
}

func (s *Server) cancelTicket(response http.ResponseWriter, request *http.Request) {
	playerID := request.Header.Get("X-Player-ID")
	if playerID == "" {
		playerID = request.URL.Query().Get("player_id")
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
	PlayerID        string                   `json:"player_id"`
	Rating          float64                  `json:"rating"`
	RatingDeviation float64                  `json:"rating_deviation"`
	History         []*playerv1.RatingChange `json:"history"`
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
	writeJSON(response, http.StatusOK, playerRatingResponse{
		PlayerID: playerResult.Player.Id, Rating: playerResult.Player.Rating,
		RatingDeviation: playerResult.Player.RatingDeviation, History: historyResult.Changes,
	})
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
