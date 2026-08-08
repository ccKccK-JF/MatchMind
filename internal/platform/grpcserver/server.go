package grpcserver

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/health"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/reflection"

	"github.com/ccKccK-JF/MatchMind/internal/platform/tracing"
)

const gracefulStopTimeout = 10 * time.Second

type RegisterFunc func(server *grpc.Server)

// Run starts a gRPC server and blocks until the context is cancelled or the
// server fails. Every service automatically exposes the standard gRPC health
// and reflection APIs.
func Run(ctx context.Context, serviceName, address string, register RegisterFunc) error {
	listener, err := net.Listen("tcp", address)
	if err != nil {
		return err
	}

	server := grpc.NewServer(grpc.ChainUnaryInterceptor(tracing.UnaryServerInterceptor()))
	register(server)

	healthServer := health.NewServer()
	healthServer.SetServingStatus("", healthpb.HealthCheckResponse_SERVING)
	healthServer.SetServingStatus(serviceName, healthpb.HealthCheckResponse_SERVING)
	healthpb.RegisterHealthServer(server, healthServer)
	reflection.Register(server)

	errCh := make(chan error, 1)
	go func() {
		slog.Info("gRPC service started", "service", serviceName, "address", address)
		errCh <- server.Serve(listener)
	}()

	select {
	case <-ctx.Done():
		healthServer.SetServingStatus("", healthpb.HealthCheckResponse_NOT_SERVING)
		healthServer.SetServingStatus(serviceName, healthpb.HealthCheckResponse_NOT_SERVING)

		stopped := make(chan struct{})
		go func() {
			server.GracefulStop()
			close(stopped)
		}()

		select {
		case <-stopped:
			slog.Info("gRPC service stopped", "service", serviceName)
		case <-time.After(gracefulStopTimeout):
			slog.Warn("gRPC graceful shutdown timed out", "service", serviceName)
			server.Stop()
		}
		return nil
	case err := <-errCh:
		if errors.Is(err, grpc.ErrServerStopped) {
			return nil
		}
		return err
	}
}
