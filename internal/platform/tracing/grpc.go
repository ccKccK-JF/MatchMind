package tracing

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

func UnaryClientInterceptor() grpc.UnaryClientInterceptor {
	return func(
		ctx context.Context,
		method string,
		request, response any,
		connection *grpc.ClientConn,
		invoker grpc.UnaryInvoker,
		options ...grpc.CallOption,
	) error {
		traceID := FromContext(ctx)
		outgoing, _ := metadata.FromOutgoingContext(ctx)
		if traceID == "" {
			traceID = firstValidMetadataValue(outgoing.Get(MetadataKey))
		}
		ctx, traceID = WithTraceID(ctx, traceID)
		outgoing = outgoing.Copy()
		outgoing.Set(MetadataKey, traceID)
		ctx = metadata.NewOutgoingContext(ctx, outgoing)
		return invoker(ctx, method, request, response, connection, options...)
	}
}

func UnaryServerInterceptor() grpc.UnaryServerInterceptor {
	return func(
		ctx context.Context,
		request any,
		info *grpc.UnaryServerInfo,
		handler grpc.UnaryHandler,
	) (any, error) {
		incoming, _ := metadata.FromIncomingContext(ctx)
		ctx, traceID := WithTraceID(ctx, firstValidMetadataValue(incoming.Get(MetadataKey)))
		_ = grpc.SetHeader(ctx, metadata.Pairs(MetadataKey, traceID))
		startedAt := time.Now()
		response, err := handler(ctx, request)
		slog.InfoContext(
			ctx,
			"gRPC request",
			"trace_id", traceID,
			"grpc_method", info.FullMethod,
			"grpc_code", grpcCode(err).String(),
			"duration_seconds", time.Since(startedAt).Seconds(),
		)
		return response, err
	}
}

func grpcCode(err error) codes.Code {
	switch {
	case err == nil:
		return codes.OK
	case errors.Is(err, context.Canceled):
		return codes.Canceled
	case errors.Is(err, context.DeadlineExceeded):
		return codes.DeadlineExceeded
	default:
		return status.Code(err)
	}
}

func firstValidMetadataValue(values []string) string {
	for _, value := range values {
		if normalized := Normalize(value); normalized != "" {
			return normalized
		}
	}
	return ""
}
