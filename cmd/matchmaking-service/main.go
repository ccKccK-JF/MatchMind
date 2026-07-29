package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	matchmakingv1 "github.com/ccKccK-JF/MatchMind/gen/go/matchmind/matchmaking/v1"
	"github.com/ccKccK-JF/MatchMind/internal/config"
	matchmakinggrpc "github.com/ccKccK-JF/MatchMind/internal/matchmaking/transport/grpc"
	"github.com/ccKccK-JF/MatchMind/internal/platform/grpcserver"
	"google.golang.org/grpc"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	address := config.String("MATCHMAKING_GRPC_ADDRESS", ":50052")
	err := grpcserver.Run(ctx, "matchmind.matchmaking.v1.MatchmakingService", address, func(server *grpc.Server) {
		matchmakingv1.RegisterMatchmakingServiceServer(server, matchmakinggrpc.NewServer())
	})
	if err != nil {
		slog.Error("matchmaking service stopped with error", "error", err)
		os.Exit(1)
	}
}
