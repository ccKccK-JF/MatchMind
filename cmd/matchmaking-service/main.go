package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	matchmakingv1 "github.com/ccKccK-JF/MatchMind/gen/go/matchmind/matchmaking/v1"
	playerv1 "github.com/ccKccK-JF/MatchMind/gen/go/matchmind/player/v1"
	"github.com/ccKccK-JF/MatchMind/internal/config"
	"github.com/ccKccK-JF/MatchMind/internal/matchmaking/application"
	"github.com/ccKccK-JF/MatchMind/internal/matchmaking/domain"
	agonesgateway "github.com/ccKccK-JF/MatchMind/internal/matchmaking/gateway/agones"
	playergateway "github.com/ccKccK-JF/MatchMind/internal/matchmaking/gateway/playergrpc"
	"github.com/ccKccK-JF/MatchMind/internal/matchmaking/observability"
	"github.com/ccKccK-JF/MatchMind/internal/matchmaking/repository/memory"
	matchpostgres "github.com/ccKccK-JF/MatchMind/internal/matchmaking/repository/postgres"
	matchredis "github.com/ccKccK-JF/MatchMind/internal/matchmaking/repository/redis"
	matchmakinggrpc "github.com/ccKccK-JF/MatchMind/internal/matchmaking/transport/grpc"
	"github.com/ccKccK-JF/MatchMind/internal/platform/grpcserver"
	"github.com/ccKccK-JF/MatchMind/internal/platform/httpserver"
	"github.com/ccKccK-JF/MatchMind/internal/platform/logging"
	platformmetrics "github.com/ccKccK-JF/MatchMind/internal/platform/metrics"
	"github.com/ccKccK-JF/MatchMind/internal/platform/tracing"
	"github.com/jackc/pgx/v5/pgxpool"
	redisclient "github.com/redis/go-redis/v9"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

type ticketRepository interface {
	application.TicketStore
	application.MatchQueue
	application.AssignedTicketCompleter
	application.QueueSizer
}

type matchRepository interface {
	application.MatchRepository
	application.MatchHistoryReader
}

func main() {
	logging.Configure()
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	playerTarget := config.String("PLAYER_GRPC_TARGET", "localhost:50051")
	playerConnection, err := grpc.NewClient(
		playerTarget,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithChainUnaryInterceptor(tracing.UnaryClientInterceptor()),
	)
	if err != nil {
		slog.Error("create player service client", "error", err)
		os.Exit(1)
	}
	defer playerConnection.Close()

	ticketBackend := strings.ToLower(config.String("MATCHMAKING_TICKET_STORAGE_BACKEND", "memory"))
	defaultMatchBackend := ticketBackend
	if ticketBackend == "redis" {
		defaultMatchBackend = "postgres"
	}
	matchBackend := strings.ToLower(config.String("MATCHMAKING_MATCH_STORAGE_BACKEND", defaultMatchBackend))
	var postgresPool *pgxpool.Pool
	if ticketBackend == "postgres" || ticketBackend == "redis" || matchBackend == "postgres" {
		connectContext, cancel := context.WithTimeout(ctx, 5*time.Second)
		postgresPool, err = pgxpool.New(connectContext, config.String(
			"POSTGRES_DSN",
			"postgres://matchmind:matchmind@localhost:5432/matchmind?sslmode=disable",
		))
		if err == nil {
			err = postgresPool.Ping(connectContext)
		}
		cancel()
		if err != nil {
			slog.Error("connect matchmaking PostgreSQL repositories", "error", err)
			os.Exit(1)
		}
		defer postgresPool.Close()
	}

	var store ticketRepository
	switch ticketBackend {
	case "memory":
		store = memory.NewTicketStore()
	case "postgres":
		store = matchpostgres.NewTicketStore(postgresPool)
	case "redis":
		redisDB, redisConfigErr := config.Int("REDIS_DB", 0)
		if redisConfigErr != nil || redisDB < 0 {
			slog.Error("invalid Redis database", "error", redisConfigErr, "database", redisDB)
			os.Exit(1)
		}
		client := redisclient.NewClient(&redisclient.Options{
			Addr:     config.String("REDIS_ADDRESS", "localhost:6379"),
			Password: config.String("REDIS_PASSWORD", ""),
			DB:       redisDB,
		})
		defer client.Close()
		queue := matchredis.NewQueue(client, config.String("REDIS_KEY_PREFIX", "matchmind"))
		redisStore := matchredis.NewStore(matchpostgres.NewTicketStore(postgresPool), queue)
		rebuildContext, rebuildCancel := context.WithTimeout(ctx, 15*time.Second)
		rebuild, rebuildErr := redisStore.Rebuild(rebuildContext, time.Now())
		rebuildCancel()
		if rebuildErr != nil {
			slog.Error("rebuild Redis matchmaking queue", "error", rebuildErr)
			os.Exit(1)
		}
		slog.Info("Redis matchmaking queue rebuilt",
			"restored_tickets", rebuild.RestoredTickets,
			"recovered_reservations", rebuild.RecoveredReservations,
		)
		store = redisStore
	default:
		slog.Error("unsupported matchmaking Ticket storage backend", "backend", ticketBackend)
		os.Exit(1)
	}
	var matchStore matchRepository
	switch matchBackend {
	case "memory":
		matchStore = memory.NewMatchStore()
	case "postgres":
		matchStore = matchpostgres.NewMatchStore(postgresPool)
	default:
		slog.Error("unsupported matchmaking Match storage backend", "backend", matchBackend)
		os.Exit(1)
	}
	players := playergateway.NewClient(playerv1.NewPlayerServiceClient(playerConnection))
	service := application.NewTicketService(store, players, nil, nil)
	matchService := application.NewMatchService(matchStore, store, nil)
	allocatorBackend := strings.ToLower(config.String("MATCHMAKING_ALLOCATOR_BACKEND", "local"))
	var allocator application.ServerAllocator
	switch allocatorBackend {
	case "local":
		capacities, capacityErr := application.ParseRegionCapacities(config.String(
			"MATCHMAKING_LOCAL_REGION_CAPACITIES",
			"hongkong=100,singapore=100,tokyo=100",
		))
		if capacityErr != nil {
			slog.Error("invalid local game server capacities", "error", capacityErr)
			os.Exit(1)
		}
		allocator, err = application.NewLocalAllocatorWithCapacities(capacities, nil)
	case "agones":
		timeoutSeconds, configErr := config.Int("AGONES_HTTP_TIMEOUT_SECONDS", 5)
		if configErr != nil || timeoutSeconds <= 0 {
			slog.Error("invalid Agones HTTP timeout", "error", configErr, "seconds", timeoutSeconds)
			os.Exit(1)
		}
		insecureSkipVerify, configErr := config.Bool("AGONES_INSECURE_SKIP_TLS_VERIFY", false)
		if configErr != nil {
			slog.Error("invalid Agones TLS configuration", "error", configErr)
			os.Exit(1)
		}
		httpClient, clientErr := agonesgateway.NewHTTPClient(
			config.String("AGONES_CA_FILE", "/var/run/secrets/kubernetes.io/serviceaccount/ca.crt"),
			insecureSkipVerify,
			time.Duration(timeoutSeconds)*time.Second,
		)
		if clientErr != nil {
			slog.Error("create Agones HTTP client", "error", clientErr)
			os.Exit(1)
		}
		bearerToken, tokenErr := agonesgateway.LoadBearerToken(
			config.String("AGONES_BEARER_TOKEN", ""),
			config.String("AGONES_BEARER_TOKEN_FILE", "/var/run/secrets/kubernetes.io/serviceaccount/token"),
		)
		if tokenErr != nil {
			slog.Error("load Agones bearer token", "error", tokenErr)
			os.Exit(1)
		}
		allocator, err = agonesgateway.NewAllocator(agonesgateway.Config{
			APIURL:    config.String("AGONES_API_URL", "https://kubernetes.default.svc"),
			Namespace: config.String("AGONES_NAMESPACE", "matchmind"), BearerToken: bearerToken,
			GameLabelKey:   config.String("AGONES_GAME_LABEL_KEY", "matchmind.dev/game"),
			GameLabelValue: config.String("AGONES_GAME_LABEL_VALUE", "matchmind"),
			RegionLabelKey: config.String("AGONES_REGION_LABEL_KEY", "matchmind.dev/region"),
			HTTPClient:     httpClient,
		})
	default:
		slog.Error("unsupported game server allocator backend", "backend", allocatorBackend)
		os.Exit(1)
	}
	if err != nil {
		slog.Error("configure game server allocator", "backend", allocatorBackend, "error", err)
		os.Exit(1)
	}
	if releaser, ok := allocator.(application.ServerReleaser); ok {
		matchService.SetServerReleaser(releaser)
	}
	greedyPolicy := domain.DefaultPolicy()
	beamPolicy := domain.BeamPolicy()
	beamWidth, err := config.Int("MATCHMAKING_BEAM_WIDTH", beamPolicy.BeamWidth)
	if err != nil {
		slog.Error("invalid Beam Search width", "error", err)
		os.Exit(1)
	}
	beamPolicy.BeamWidth = beamWidth
	if err := beamPolicy.Validate(); err != nil {
		slog.Error("invalid Beam Search policy", "error", err)
		os.Exit(1)
	}
	policyMode := strings.ToLower(config.String("MATCHMAKING_POLICY_MODE", "beam"))
	policy := beamPolicy
	var initialExperiment *application.PolicyExperiment
	switch policyMode {
	case "greedy":
		policy = greedyPolicy
	case "beam":
		policy = beamPolicy
	case "ab":
		treatmentBasisPoints, configErr := config.Int("MATCHMAKING_AB_TREATMENT_BPS", 5000)
		if configErr != nil {
			slog.Error("invalid A/B treatment allocation", "error", configErr)
			os.Exit(1)
		}
		policy = greedyPolicy
		initialExperiment = &application.PolicyExperiment{
			ID: "team-formation-v2", ControlVersion: greedyPolicy.Version,
			TreatmentVersion: beamPolicy.Version, TreatmentBasisPoints: treatmentBasisPoints,
			AssignmentSalt: config.String("MATCHMAKING_AB_SALT", "matchmind-team-formation-v2"),
			StartedAt:      time.Now(),
		}
	default:
		slog.Error("unsupported matchmaking policy mode", "mode", policyMode)
		os.Exit(1)
	}
	policyManager, err := application.NewPolicyManager(
		[]domain.MatchPolicy{greedyPolicy, beamPolicy}, policy.Version,
	)
	if err == nil && initialExperiment != nil {
		err = policyManager.StartExperiment(*initialExperiment)
	}
	if err != nil {
		slog.Error("configure matchmaking policy registry", "error", err)
		os.Exit(1)
	}
	slog.Info("matchmaking policy configured", "mode", policyMode, "default_version", policy.Version, "beam_width", beamPolicy.BeamWidth)
	workerCount, err := config.Int("MATCHMAKING_WORKER_COUNT", 1)
	if err != nil {
		slog.Error("invalid matchmaking worker count", "error", err)
		os.Exit(1)
	}
	if workerCount < 0 {
		slog.Error("matchmaking worker count cannot be negative", "worker_count", workerCount)
		os.Exit(1)
	}
	registry := platformmetrics.NewRegistry()
	workerMetrics := observability.NewMatchmakingMetrics(registry)
	for workerIndex := range workerCount {
		worker, workerErr := application.NewWorker(
			store, matchStore, allocator, policy, nil, nil,
		)
		if workerErr != nil {
			slog.Error("create matchmaking worker", "worker_index", workerIndex, "error", workerErr)
			os.Exit(1)
		}
		worker.SetPolicySelector(policyManager)
		worker.SetMetrics(workerMetrics)
		go worker.Run(ctx, 250*time.Millisecond)
	}
	analysisService, err := application.NewAnalysisService(
		matchStore, store, []domain.MatchPolicy{greedyPolicy, beamPolicy},
	)
	if err != nil {
		slog.Error("create match analysis service", "error", err)
		os.Exit(1)
	}
	analysisService.SetPolicyCatalog(policyManager)
	transport := matchmakinggrpc.NewServer(service, matchService, analysisService)
	operationsService, err := application.NewPolicyOperationsService(
		store, policyManager, config.String("AGENT_CONTROL_TOKEN", "matchmind-local-agent-control"), nil,
	)
	if err != nil {
		slog.Error("create policy operations service", "error", err)
		os.Exit(1)
	}
	transport.SetPolicyOperations(operationsService)

	address := config.String("MATCHMAKING_GRPC_ADDRESS", ":50052")
	errCh := make(chan error, 2)
	go func() {
		errCh <- grpcserver.Run(ctx, "matchmind.matchmaking.v1.MatchmakingService", address, func(server *grpc.Server) {
			matchmakingv1.RegisterMatchmakingServiceServer(server, transport)
		})
	}()
	go func() {
		httpAddress := config.String("MATCHMAKING_HTTP_ADDRESS", ":8082")
		errCh <- httpserver.Run(ctx, "matchmind-matchmaking-operations", httpAddress, httpserver.NewHandler(nil, registry, nil))
	}()
	err = <-errCh
	stop()
	if err != nil {
		slog.Error("matchmaking service stopped with error", "error", err)
		os.Exit(1)
	}
}
