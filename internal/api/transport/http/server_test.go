package httptransport

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	agentv1 "github.com/ccKccK-JF/MatchMind/gen/go/matchmind/agent/v1"
	matchmakingv1 "github.com/ccKccK-JF/MatchMind/gen/go/matchmind/matchmaking/v1"
	playerv1 "github.com/ccKccK-JF/MatchMind/gen/go/matchmind/player/v1"
	simulationv1 "github.com/ccKccK-JF/MatchMind/gen/go/matchmind/simulation/v1"
	platformmetrics "github.com/ccKccK-JF/MatchMind/internal/platform/metrics"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type fakeMatchmakingClient struct {
	matchmakingv1.MatchmakingServiceClient
	create  func(context.Context, *matchmakingv1.CreateTicketRequest) (*matchmakingv1.CreateTicketResponse, error)
	get     func(context.Context, *matchmakingv1.GetTicketRequest) (*matchmakingv1.GetTicketResponse, error)
	analyze func(context.Context, *matchmakingv1.AnalyzeMatchQualityRequest) (*matchmakingv1.AnalyzeMatchQualityResponse, error)
	replay  func(context.Context, *matchmakingv1.ReplayHistoricalMatchRequest) (*matchmakingv1.ReplayHistoricalMatchResponse, error)
}

func (f fakeMatchmakingClient) CreateTicket(ctx context.Context, request *matchmakingv1.CreateTicketRequest, _ ...grpc.CallOption) (*matchmakingv1.CreateTicketResponse, error) {
	return f.create(ctx, request)
}

func (f fakeMatchmakingClient) GetTicket(ctx context.Context, request *matchmakingv1.GetTicketRequest, _ ...grpc.CallOption) (*matchmakingv1.GetTicketResponse, error) {
	return f.get(ctx, request)
}

func (f fakeMatchmakingClient) AnalyzeMatchQuality(ctx context.Context, request *matchmakingv1.AnalyzeMatchQualityRequest, _ ...grpc.CallOption) (*matchmakingv1.AnalyzeMatchQualityResponse, error) {
	return f.analyze(ctx, request)
}

func (f fakeMatchmakingClient) ReplayHistoricalMatch(ctx context.Context, request *matchmakingv1.ReplayHistoricalMatchRequest, _ ...grpc.CallOption) (*matchmakingv1.ReplayHistoricalMatchResponse, error) {
	return f.replay(ctx, request)
}

type fakePlayerClient struct {
	playerv1.PlayerServiceClient
	get     func(context.Context, *playerv1.GetPlayerRequest) (*playerv1.GetPlayerResponse, error)
	history func(context.Context, *playerv1.GetRatingHistoryRequest) (*playerv1.GetRatingHistoryResponse, error)
}

type fakeSimulationClient struct {
	simulationv1.SimulationServiceClient
	batch func(context.Context, *simulationv1.SimulateBatchRequest) (*simulationv1.SimulateBatchResponse, error)
}

type fakeAgentClient struct {
	agentv1.AgentServiceClient
	run      func(context.Context, *agentv1.RunAnalysisRequest) (*agentv1.RunAnalysisResponse, error)
	review   func(context.Context, *agentv1.ReviewProposalRequest) (*agentv1.ReviewProposalResponse, error)
	activate func(context.Context, *agentv1.ActivateProposalRequest) (*agentv1.ActivateProposalResponse, error)
}

func (f fakeAgentClient) RunAnalysis(ctx context.Context, request *agentv1.RunAnalysisRequest, _ ...grpc.CallOption) (*agentv1.RunAnalysisResponse, error) {
	return f.run(ctx, request)
}

func (f fakeAgentClient) ReviewProposal(ctx context.Context, request *agentv1.ReviewProposalRequest, _ ...grpc.CallOption) (*agentv1.ReviewProposalResponse, error) {
	return f.review(ctx, request)
}

func (f fakeAgentClient) ActivateProposal(ctx context.Context, request *agentv1.ActivateProposalRequest, _ ...grpc.CallOption) (*agentv1.ActivateProposalResponse, error) {
	return f.activate(ctx, request)
}

func (f fakeSimulationClient) SimulateBatch(ctx context.Context, request *simulationv1.SimulateBatchRequest, _ ...grpc.CallOption) (*simulationv1.SimulateBatchResponse, error) {
	return f.batch(ctx, request)
}

func (f fakePlayerClient) GetPlayer(ctx context.Context, request *playerv1.GetPlayerRequest, _ ...grpc.CallOption) (*playerv1.GetPlayerResponse, error) {
	return f.get(ctx, request)
}

func (f fakePlayerClient) GetRatingHistory(ctx context.Context, request *playerv1.GetRatingHistoryRequest, _ ...grpc.CallOption) (*playerv1.GetRatingHistoryResponse, error) {
	return f.history(ctx, request)
}

func TestCreateTicketMapsJSONToGRPC(t *testing.T) {
	registry := platformmetrics.NewRegistry()
	var captured *matchmakingv1.CreateTicketRequest
	client := fakeMatchmakingClient{create: func(_ context.Context, request *matchmakingv1.CreateTicketRequest) (*matchmakingv1.CreateTicketResponse, error) {
		captured = request
		return &matchmakingv1.CreateTicketResponse{Ticket: &matchmakingv1.MatchTicket{Id: "ticket-1", State: matchmakingv1.TicketState_TICKET_STATE_QUEUED}}, nil
	}}
	server := NewServer(nil, client, nil, NewAPIMetrics(registry))
	request := httptest.NewRequest(http.MethodPost, "/api/v1/tickets", strings.NewReader(`{
		"player_id":"player-1","mode":"ranked_5v5","client_version":"1.0.0",
		"preferred_roles":["core","support"],"region_latency":{"singapore":32}
	}`))
	request.Header.Set("Idempotency-Key", "create-1")
	response := httptest.NewRecorder()

	server.ServeHTTP(response, request)

	if response.Code != http.StatusCreated {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
	if captured == nil || captured.IdempotencyKey != "create-1" || captured.PreferredRoles[0] != playerv1.Role_ROLE_CORE {
		t.Fatalf("unexpected gRPC request: %+v", captured)
	}
	if response.Header().Get("X-Trace-ID") == "" {
		t.Fatal("missing trace id")
	}
	if !strings.Contains(response.Body.String(), `"id":"ticket-1"`) {
		t.Fatalf("unexpected body: %s", response.Body.String())
	}
}

func TestGRPCNotFoundMapsToHTTP(t *testing.T) {
	client := fakeMatchmakingClient{get: func(context.Context, *matchmakingv1.GetTicketRequest) (*matchmakingv1.GetTicketResponse, error) {
		return nil, status.Error(codes.NotFound, "ticket not found")
	}}
	server := NewServer(nil, client, nil, nil)
	response := httptest.NewRecorder()
	server.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/v1/tickets/missing", nil))
	if response.Code != http.StatusNotFound {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
}

func TestGetPlayerRatingIncludesHistory(t *testing.T) {
	client := fakePlayerClient{
		get: func(context.Context, *playerv1.GetPlayerRequest) (*playerv1.GetPlayerResponse, error) {
			return &playerv1.GetPlayerResponse{Player: &playerv1.Player{Id: "player-1", Rating: 1516}}, nil
		},
		history: func(context.Context, *playerv1.GetRatingHistoryRequest) (*playerv1.GetRatingHistoryResponse, error) {
			return &playerv1.GetRatingHistoryResponse{Changes: []*playerv1.RatingChange{{MatchId: "match-1", Delta: 16}}}, nil
		},
	}
	server := NewServer(client, nil, nil, nil)
	response := httptest.NewRecorder()
	server.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/v1/players/player-1/rating", nil))
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"match_id":"match-1"`) {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
}

func TestBatchSimulationMapsJSONToGRPC(t *testing.T) {
	var captured *simulationv1.SimulateBatchRequest
	client := fakeSimulationClient{batch: func(ctx context.Context, request *simulationv1.SimulateBatchRequest) (*simulationv1.SimulateBatchResponse, error) {
		deadline, exists := ctx.Deadline()
		if !exists || time.Until(deadline) < 20*time.Second {
			t.Fatalf("batch deadline = %v, exists = %v", deadline, exists)
		}
		captured = request
		return &simulationv1.SimulateBatchResponse{SimulationCount: int32(len(request.Inputs)), TeamAWinRate: .6}, nil
	}}
	server := NewServer(nil, nil, client, nil)
	request := httptest.NewRequest(http.MethodPost, "/api/v1/simulations/batch", strings.NewReader(`{
		"concurrency":4,"inputs":[{"case_id":"case-1","random_seed":42,
		"rating_a":1520,"rating_b":1480,"predicted_win_rate_a":0.557,
		"role_score":90,"latency_score":85,"party_score":100}]
	}`))
	response := httptest.NewRecorder()
	server.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
	if captured == nil || captured.Concurrency != 4 || len(captured.Inputs) != 1 || captured.Inputs[0].CaseId != "case-1" {
		t.Fatalf("captured request = %#v", captured)
	}
	if !strings.Contains(response.Body.String(), `"simulation_count":1`) {
		t.Fatalf("response = %s", response.Body.String())
	}
}

func TestAnalyzeMatchQualityMapsQueryToGRPC(t *testing.T) {
	var captured *matchmakingv1.AnalyzeMatchQualityRequest
	client := fakeMatchmakingClient{analyze: func(_ context.Context, request *matchmakingv1.AnalyzeMatchQualityRequest) (*matchmakingv1.AnalyzeMatchQualityResponse, error) {
		captured = request
		return &matchmakingv1.AnalyzeMatchQualityResponse{Summaries: []*matchmakingv1.PolicyQualitySummary{{
			PolicyVersion: "v2-beam", MatchCount: 20, MeanAbsoluteQualityError: 4.5,
		}}}, nil
	}}
	server := NewServer(nil, client, nil, nil)
	request := httptest.NewRequest(http.MethodGet,
		"/api/v1/analytics/match-quality?policy_version=v2-beam&mode=ranked_5v5&server_region=hongkong&from=2026-08-01T00%3A00%3A00Z&to=2026-08-08T00%3A00%3A00Z&limit=50", nil)
	response := httptest.NewRecorder()
	server.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
	if captured == nil || captured.PolicyVersion != "v2-beam" || captured.Mode != "ranked_5v5" ||
		captured.ServerRegion != "hongkong" || captured.Limit != 50 || captured.From == nil || captured.To == nil {
		t.Fatalf("captured request = %#v", captured)
	}
	if !strings.Contains(response.Body.String(), `"mean_absolute_quality_error":4.5`) {
		t.Fatalf("response = %s", response.Body.String())
	}
}

func TestReplayHistoricalMatchMapsJSONAndUsesLongTimeout(t *testing.T) {
	var captured *matchmakingv1.ReplayHistoricalMatchRequest
	client := fakeMatchmakingClient{replay: func(ctx context.Context, request *matchmakingv1.ReplayHistoricalMatchRequest) (*matchmakingv1.ReplayHistoricalMatchResponse, error) {
		deadline, exists := ctx.Deadline()
		if !exists || time.Until(deadline) < 20*time.Second {
			t.Fatalf("replay deadline = %v, exists = %v", deadline, exists)
		}
		captured = request
		return &matchmakingv1.ReplayHistoricalMatchResponse{SourceMatchId: request.MatchId, TicketCount: int32(len(request.TicketIds))}, nil
	}}
	server := NewServer(nil, client, nil, nil)
	request := httptest.NewRequest(http.MethodPost, "/api/v1/matches/match-1/replay", strings.NewReader(`{
		"policy_versions":["v1-greedy","v2-beam"],"ticket_ids":["ticket-1","ticket-2"]
	}`))
	response := httptest.NewRecorder()
	server.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
	if captured == nil || captured.MatchId != "match-1" || len(captured.PolicyVersions) != 2 || len(captured.TicketIds) != 2 {
		t.Fatalf("captured request = %#v", captured)
	}
}

func TestAnalyzeMatchQualityRejectsInvalidTimestamp(t *testing.T) {
	server := NewServer(nil, fakeMatchmakingClient{}, nil, nil)
	response := httptest.NewRecorder()
	server.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/v1/analytics/match-quality?from=not-a-time", nil))
	if response.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
}

func TestAgentRunRequiresOperatorAndMapsIdentity(t *testing.T) {
	var captured *agentv1.RunAnalysisRequest
	client := fakeAgentClient{run: func(ctx context.Context, request *agentv1.RunAnalysisRequest) (*agentv1.RunAnalysisResponse, error) {
		deadline, exists := ctx.Deadline()
		if !exists || time.Until(deadline) < time.Minute {
			t.Fatalf("Agent deadline = %v, exists = %v", deadline, exists)
		}
		captured = request
		return &agentv1.RunAnalysisResponse{Run: &agentv1.AuditRun{Id: "run-1"}, Proposal: &agentv1.PolicyProposal{Id: "proposal-1"}}, nil
	}}
	server := NewServer(nil, nil, nil, nil, client)
	body := `{"base_policy_version":"v2-beam","mode":"ranked_5v5","server_region":"hongkong","historical_limit":20}`
	unauthorized := httptest.NewRecorder()
	server.ServeHTTP(unauthorized, httptest.NewRequest(http.MethodPost, "/api/v1/agent/runs", strings.NewReader(body)))
	if unauthorized.Code != http.StatusUnauthorized {
		t.Fatalf("unauthorized status = %d, body = %s", unauthorized.Code, unauthorized.Body.String())
	}
	request := httptest.NewRequest(http.MethodPost, "/api/v1/agent/runs", strings.NewReader(body))
	request.Header.Set("X-Operator-ID", "analyst-1")
	request.Header.Set("X-Operator-Role", "analyst")
	response := httptest.NewRecorder()
	server.ServeHTTP(response, request)
	if response.Code != http.StatusCreated || captured == nil || captured.RequestedBy != "analyst-1" || captured.HistoricalLimit != 20 {
		t.Fatalf("status = %d, captured = %#v, body = %s", response.Code, captured, response.Body.String())
	}
}

func TestAgentReviewAndActivationEnforceRoles(t *testing.T) {
	var review *agentv1.ReviewProposalRequest
	var activation *agentv1.ActivateProposalRequest
	client := fakeAgentClient{
		review: func(_ context.Context, request *agentv1.ReviewProposalRequest) (*agentv1.ReviewProposalResponse, error) {
			review = request
			return &agentv1.ReviewProposalResponse{Proposal: &agentv1.PolicyProposal{Id: request.ProposalId}}, nil
		},
		activate: func(_ context.Context, request *agentv1.ActivateProposalRequest) (*agentv1.ActivateProposalResponse, error) {
			activation = request
			return &agentv1.ActivateProposalResponse{Proposal: &agentv1.PolicyProposal{Id: request.ProposalId}}, nil
		},
	}
	server := NewServer(nil, nil, nil, nil, client)
	forbiddenRequest := httptest.NewRequest(http.MethodPost, "/api/v1/agent/proposals/proposal-1/review", strings.NewReader(`{"decision":"approve","reason":"ok"}`))
	forbiddenRequest.Header.Set("X-Operator-ID", "analyst-1")
	forbiddenRequest.Header.Set("X-Operator-Role", "analyst")
	forbidden := httptest.NewRecorder()
	server.ServeHTTP(forbidden, forbiddenRequest)
	if forbidden.Code != http.StatusForbidden {
		t.Fatalf("forbidden status = %d", forbidden.Code)
	}
	reviewRequest := httptest.NewRequest(http.MethodPost, "/api/v1/agent/proposals/proposal-1/review", strings.NewReader(`{"decision":"approve","reason":"risk passed"}`))
	reviewRequest.Header.Set("X-Operator-ID", "reviewer-1")
	reviewRequest.Header.Set("X-Operator-Role", "reviewer")
	reviewResponse := httptest.NewRecorder()
	server.ServeHTTP(reviewResponse, reviewRequest)
	if reviewResponse.Code != http.StatusOK || review == nil || review.ReviewerId != "reviewer-1" || !review.Approve {
		t.Fatalf("review status = %d, request = %#v", reviewResponse.Code, review)
	}
	activateRequest := httptest.NewRequest(http.MethodPost, "/api/v1/agent/proposals/proposal-1/activate", strings.NewReader(`{"treatment_basis_points":1000,"assignment_salt":"guarded"}`))
	activateRequest.Header.Set("X-Operator-ID", "admin-1")
	activateRequest.Header.Set("X-Operator-Role", "admin")
	activateResponse := httptest.NewRecorder()
	server.ServeHTTP(activateResponse, activateRequest)
	if activateResponse.Code != http.StatusOK || activation == nil || activation.OperatorId != "admin-1" || activation.TreatmentBasisPoints != 1000 {
		t.Fatalf("activation status = %d, request = %#v", activateResponse.Code, activation)
	}
}
