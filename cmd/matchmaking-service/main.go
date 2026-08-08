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
	"github.com/jackc/pgx/v5/pgxpool"
	redisclient "github.com/redis/go-redis/v9"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

type ticketRepository interface {
	application.TicketStore
	application.MatchQueue
	application.AssignedTicketCompleter
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
	playerConnection, err := grpc.NewClient(playerTarget, grpc.WithTransportCredentials(insecure.NewCredentials()))
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
	var policyManager *application.PolicyManager
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
		policyManager, err = application.NewPolicyManager(
			[]domain.MatchPolicy{greedyPolicy, beamPolicy}, greedyPolicy.Version,
		)
		if err == nil {
			err = policyManager.StartExperiment(application.PolicyExperiment{
				ID: "team-formation-v2", ControlVersion: greedyPolicy.Version,
				TreatmentVersion: beamPolicy.Version, TreatmentBasisPoints: treatmentBasisPoints,
				AssignmentSalt: config.String("MATCHMAKING_AB_SALT", "matchmind-team-formation-v2"),
				StartedAt:      time.Now(),
			})
		}
		if err != nil {
			slog.Error("configure matchmaking A/B experiment", "error", err)
			os.Exit(1)
		}
		policy = greedyPolicy
	default:
		slog.Error("unsupported matchmaking policy mode", "mode", policyMode)
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
			store, matchStore, application.NewLocalAllocator(nil), policy, nil, nil,
		)
		if workerErr != nil {
			slog.Error("create matchmaking worker", "worker_index", workerIndex, "error", workerErr)
			os.Exit(1)
		}
		if policyManager != nil {
			worker.SetPolicySelector(policyManager)
		}
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
	transport := matchmakinggrpc.NewServer(service, matchService, analysisService)

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
