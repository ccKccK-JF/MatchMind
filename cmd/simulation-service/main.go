package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	simulationv1 "github.com/ccKccK-JF/MatchMind/gen/go/matchmind/simulation/v1"
	"github.com/ccKccK-JF/MatchMind/internal/config"
	"github.com/ccKccK-JF/MatchMind/internal/platform/grpcserver"
	simulationgrpc "github.com/ccKccK-JF/MatchMind/internal/simulation/transport/grpc"
	"google.golang.org/grpc"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	address := config.String("SIMULATION_GRPC_ADDRESS", ":50053")
	err := grpcserver.Run(ctx, "matchmind.simulation.v1.SimulationService", address, func(server *grpc.Server) {
		simulationv1.RegisterSimulationServiceServer(server, simulationgrpc.NewServer())
	})
	if err != nil {
		slog.Error("simulation service stopped with error", "error", err)
		os.Exit(1)
	}
}
