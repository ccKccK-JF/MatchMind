package httptransport

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

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
	create func(context.Context, *matchmakingv1.CreateTicketRequest) (*matchmakingv1.CreateTicketResponse, error)
	get    func(context.Context, *matchmakingv1.GetTicketRequest) (*matchmakingv1.GetTicketResponse, error)
}

func (f fakeMatchmakingClient) CreateTicket(ctx context.Context, request *matchmakingv1.CreateTicketRequest, _ ...grpc.CallOption) (*matchmakingv1.CreateTicketResponse, error) {
	return f.create(ctx, request)
}

func (f fakeMatchmakingClient) GetTicket(ctx context.Context, request *matchmakingv1.GetTicketRequest, _ ...grpc.CallOption) (*matchmakingv1.GetTicketResponse, error) {
	return f.get(ctx, request)
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
