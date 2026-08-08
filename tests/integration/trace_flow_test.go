package integration

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	playerv1 "github.com/ccKccK-JF/MatchMind/gen/go/matchmind/player/v1"
	apihttp "github.com/ccKccK-JF/MatchMind/internal/api/transport/http"
	"github.com/ccKccK-JF/MatchMind/internal/platform/tracing"
	playerapp "github.com/ccKccK-JF/MatchMind/internal/player/application"
	playermemory "github.com/ccKccK-JF/MatchMind/internal/player/repository/memory"
	playergrpc "github.com/ccKccK-JF/MatchMind/internal/player/transport/grpc"
	"github.com/ccKccK-JF/MatchMind/internal/rating/elo"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/test/bufconn"
)

func TestHTTPTraceIDReachesGRPCServiceContext(t *testing.T) {
	repository := playermemory.NewRepository()
	calculator, err := elo.NewCalculator(32)
	if err != nil {
		t.Fatal(err)
	}
	transport := playergrpc.NewServer(
		playerapp.NewService(repository, nil),
		playerapp.NewRatingService(repository, calculator, nil),
	)
	captured := make(chan string, 1)
	listener := bufconn.Listen(1024 * 1024)
	server := grpc.NewServer(grpc.ChainUnaryInterceptor(
		tracing.UnaryServerInterceptor(),
		func(ctx context.Context, request any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
			captured <- tracing.FromContext(ctx)
			return handler(ctx, request)
		},
	))
	playerv1.RegisterPlayerServiceServer(server, transport)
	errors := make(chan error, 1)
	go func() { errors <- server.Serve(listener) }()
	t.Cleanup(func() {
		server.Stop()
		_ = listener.Close()
		if err := <-errors; err != nil && err != grpc.ErrServerStopped {
			t.Errorf("gRPC server error = %v", err)
		}
	})
	connection, err := grpc.NewClient(
		"passthrough:///trace-flow",
		grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) { return listener.Dial() }),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithChainUnaryInterceptor(tracing.UnaryClientInterceptor()),
	)
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Close()

	api := apihttp.NewServer(playerv1.NewPlayerServiceClient(connection), nil, nil, nil)
	request := httptest.NewRequest(http.MethodPost, "/api/v1/players", strings.NewReader(`{
        "id":"player-trace","name":"Trace Player","initial_rating":1500,
        "preferred_roles":["core"],"home_region":"hongkong",
        "region_latency":{"hongkong":30},"behavior_score":95
    }`))
	request.Header.Set(tracing.HeaderName, "integration-trace-1")
	response := httptest.NewRecorder()
	api.ServeHTTP(response, request)
	if response.Code != http.StatusCreated {
		t.Fatalf("HTTP status/body = %d/%s", response.Code, response.Body.String())
	}
	if response.Header().Get(tracing.HeaderName) != "integration-trace-1" {
		t.Fatalf("HTTP response trace = %q", response.Header().Get(tracing.HeaderName))
	}
	if traceID := <-captured; traceID != "integration-trace-1" {
		t.Fatalf("gRPC service trace = %q", traceID)
	}
}
