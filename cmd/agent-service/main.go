package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	agentv1 "github.com/ccKccK-JF/MatchMind/gen/go/matchmind/agent/v1"
	matchmakingv1 "github.com/ccKccK-JF/MatchMind/gen/go/matchmind/matchmaking/v1"
	agentapp "github.com/ccKccK-JF/MatchMind/internal/agent/application"
	agentgateway "github.com/ccKccK-JF/MatchMind/internal/agent/gateway/matchmakinggrpc"
	"github.com/ccKccK-JF/MatchMind/internal/agent/observability"
	agentmemory "github.com/ccKccK-JF/MatchMind/internal/agent/repository/memory"
	agentpostgres "github.com/ccKccK-JF/MatchMind/internal/agent/repository/postgres"
	agentgrpc "github.com/ccKccK-JF/MatchMind/internal/agent/transport/grpc"
	"github.com/ccKccK-JF/MatchMind/internal/config"
	"github.com/ccKccK-JF/MatchMind/internal/platform/grpcserver"
	"github.com/ccKccK-JF/MatchMind/internal/platform/httpserver"
	"github.com/ccKccK-JF/MatchMind/internal/platform/logging"
	platformmetrics "github.com/ccKccK-JF/MatchMind/internal/platform/metrics"
	"github.com/jackc/pgx/v5/pgxpool"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/health/grpc_health_v1"
)

func main() {
	logging.Configure()
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	matchmakingTarget := config.String("MATCHMAKING_GRPC_TARGET", "localhost:50052")
	matchmakingConnection, err := grpc.NewClient(matchmakingTarget, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		slog.Error("create matchmaking client for Agent", "error", err)
		os.Exit(1)
	}
	defer matchmakingConnection.Close()

	var repository agentapp.Repository
	storageBackend := strings.ToLower(config.String("AGENT_STORAGE_BACKEND", "memory"))
	var postgresPool *pgxpool.Pool
	switch storageBackend {
	case "memory":
		repository = agentmemory.NewRepository()
	case "postgres":
		connectContext, cancel := context.WithTimeout(ctx, 5*time.Second)
		postgresPool, err = pgxpool.New(connectContext, config.String(
			"POSTGRES_DSN", "postgres://matchmind:matchmind@localhost:5432/matchmind?sslmode=disable",
		))
		if err == nil {
			err = postgresPool.Ping(connectContext)
		}
		cancel()
		if err != nil {
			slog.Error("connect Agent PostgreSQL repository", "error", err)
			os.Exit(1)
		}
		defer postgresPool.Close()
		repository = agentpostgres.NewRepository(postgresPool)
	default:
		slog.Error("unsupported Agent storage backend", "backend", storageBackend)
		os.Exit(1)
	}

	tools := agentgateway.NewClient(
		matchmakingv1.NewMatchmakingServiceClient(matchmakingConnection),
		config.String("AGENT_CONTROL_TOKEN", "matchmind-local-agent-control"),
	)
	service, err := agentapp.NewService(
		repository, tools,
		config.String("AGENT_NAME", "matchmind-policy-advisor"),
		config.String("AGENT_MODEL", "deterministic-policy-advisor-v1"),
		config.String("AGENT_PROMPT_VERSION", "matchmind-agent-v1"),
		config.String("AGENT_DEFAULT_BASE_POLICY", "v2-beam"),
		nil, nil,
	)
	if err != nil {
		slog.Error("create Agent service", "error", err)
		os.Exit(1)
	}
	recoveredRuns, err := service.Recover(ctx)
	if err != nil {
		slog.Error("recover incomplete Agent runs", "error", err)
		os.Exit(1)
	}
	if recoveredRuns > 0 {
		slog.Warn("recovered incomplete Agent runs", "count", recoveredRuns)
	}
	transport := agentgrpc.NewServer(service)
	registry := platformmetrics.NewRegistry()
	service.SetMetrics(observability.NewMetrics(registry))

	errCh := make(chan error, 2)
	go func() {
		address := config.String("AGENT_GRPC_ADDRESS", ":50054")
		errCh <- grpcserver.Run(ctx, "matchmind.agent.v1.AgentService", address, func(server *grpc.Server) {
			agentv1.RegisterAgentServiceServer(server, transport)
		})
	}()
	go func() {
		address := config.String("AGENT_HTTP_ADDRESS", ":8084")
		health := grpc_health_v1.NewHealthClient(matchmakingConnection)
		ready := func(ctx context.Context) error {
			response, err := health.Check(ctx, &grpc_health_v1.HealthCheckRequest{Service: "matchmind.matchmaking.v1.MatchmakingService"})
			if err != nil {
				return err
			}
			if response.GetStatus() != grpc_health_v1.HealthCheckResponse_SERVING {
				return fmt.Errorf("matchmaking service is %s", response.GetStatus())
			}
			return nil
		}
		errCh <- httpserver.Run(ctx, "matchmind-agent-operations", address, httpserver.NewHandler(nil, registry, ready))
	}()

	err = <-errCh
	stop()
	if err != nil {
		slog.Error("Agent service stopped with error", "error", err)
		os.Exit(1)
	}
}
