package tracing

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net"
	"testing"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/health"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/test/bufconn"
)

func TestUnaryInterceptorsPropagateSameTraceAcrossHop(t *testing.T) {
	serverInterceptor := UnaryServerInterceptor()
	clientInterceptor := UnaryClientInterceptor()
	incoming := metadata.NewIncomingContext(context.Background(), metadata.Pairs(MetadataKey, "trace-hop-1"))
	_, err := serverInterceptor(incoming, struct{}{}, &grpc.UnaryServerInfo{FullMethod: "/test.Service/Call"}, func(ctx context.Context, request any) (any, error) {
		if FromContext(ctx) != "trace-hop-1" {
			t.Fatalf("server context trace = %q", FromContext(ctx))
		}
		return nil, clientInterceptor(ctx, "/downstream.Service/Call", request, nil, nil, func(
			outgoing context.Context, _ string, _, _ any, _ *grpc.ClientConn, _ ...grpc.CallOption,
		) error {
			values, _ := metadata.FromOutgoingContext(outgoing)
			if values.Get(MetadataKey)[0] != "trace-hop-1" {
				t.Fatalf("downstream metadata = %+v", values)
			}
			return nil
		})
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestUnaryClientInterceptorPreservesOtherMetadata(t *testing.T) {
	ctx := metadata.NewOutgoingContext(context.Background(), metadata.Pairs("authorization", "token", MetadataKey, "existing-trace"))
	err := UnaryClientInterceptor()(ctx, "/test.Service/Call", nil, nil, nil, func(
		outgoing context.Context, _ string, _, _ any, _ *grpc.ClientConn, _ ...grpc.CallOption,
	) error {
		values, _ := metadata.FromOutgoingContext(outgoing)
		if values.Get("authorization")[0] != "token" || values.Get(MetadataKey)[0] != "existing-trace" {
			t.Fatalf("outgoing metadata = %+v", values)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestGRPCCodeMapsContextErrors(t *testing.T) {
	if grpcCode(context.Canceled) != codes.Canceled || grpcCode(context.DeadlineExceeded) != codes.DeadlineExceeded {
		t.Fatalf("context codes = %s/%s", grpcCode(context.Canceled), grpcCode(context.DeadlineExceeded))
	}
}

func TestGRPCServerReturnsTraceHeaderAndStructuredLog(t *testing.T) {
	var output bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&output, nil)))
	defer slog.SetDefault(previous)

	listener := bufconn.Listen(1024 * 1024)
	server := grpc.NewServer(grpc.ChainUnaryInterceptor(UnaryServerInterceptor()))
	healthServer := health.NewServer()
	healthServer.SetServingStatus("", healthpb.HealthCheckResponse_SERVING)
	healthpb.RegisterHealthServer(server, healthServer)
	errors := make(chan error, 1)
	go func() { errors <- server.Serve(listener) }()
	t.Cleanup(func() {
		server.Stop()
		_ = listener.Close()
		if err := <-errors; err != nil && err != grpc.ErrServerStopped {
			t.Errorf("server error = %v", err)
		}
	})
	connection, err := grpc.NewClient(
		"passthrough:///trace-test",
		grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) { return listener.Dial() }),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithChainUnaryInterceptor(UnaryClientInterceptor()),
	)
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Close()
	ctx, _ := WithTraceID(context.Background(), "trace-header-1")
	var header metadata.MD
	if _, err := healthpb.NewHealthClient(connection).Check(ctx, &healthpb.HealthCheckRequest{}, grpc.Header(&header)); err != nil {
		t.Fatal(err)
	}
	if values := header.Get(MetadataKey); len(values) != 1 || values[0] != "trace-header-1" {
		t.Fatalf("response trace header = %+v", header)
	}
	var record map[string]any
	if err := json.Unmarshal(bytes.TrimSpace(output.Bytes()), &record); err != nil {
		t.Fatalf("decode structured log %q: %v", output.String(), err)
	}
	if record["trace_id"] != "trace-header-1" || record["grpc_method"] != "/grpc.health.v1.Health/Check" || record["grpc_code"] != "OK" {
		t.Fatalf("structured log = %+v", record)
	}
}
