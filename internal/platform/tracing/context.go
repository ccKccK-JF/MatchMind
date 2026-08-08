package tracing

import (
	"context"
	"strconv"
	"strings"
	"time"

	platformid "github.com/ccKccK-JF/MatchMind/internal/platform/id"
)

const (
	HeaderName       = "X-Trace-ID"
	MetadataKey      = "x-trace-id"
	maxTraceIDLength = 128
)

type traceIDKey struct{}

func WithTraceID(ctx context.Context, candidate string) (context.Context, string) {
	traceID := Normalize(candidate)
	if traceID == "" {
		traceID = newTraceID()
	}
	return context.WithValue(ctx, traceIDKey{}, traceID), traceID
}

func Ensure(ctx context.Context) (context.Context, string) {
	if traceID := FromContext(ctx); traceID != "" {
		return ctx, traceID
	}
	return WithTraceID(ctx, "")
}

func FromContext(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	traceID, _ := ctx.Value(traceIDKey{}).(string)
	return Normalize(traceID)
}

func Normalize(value string) string {
	value = strings.TrimSpace(value)
	if value == "" || len(value) > maxTraceIDLength {
		return ""
	}
	for _, character := range value {
		if (character >= 'a' && character <= 'z') ||
			(character >= 'A' && character <= 'Z') ||
			(character >= '0' && character <= '9') {
			continue
		}
		switch character {
		case '-', '_', '.', ':', '/':
			continue
		default:
			return ""
		}
	}
	return value
}

func newTraceID() string {
	traceID, err := platformid.UUID()
	if err == nil {
		return traceID
	}
	return "trace-" + strconv.FormatInt(time.Now().UTC().UnixNano(), 36)
}
