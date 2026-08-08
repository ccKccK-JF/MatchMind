package tracing

import (
	"context"
	"strings"
	"testing"
)

func TestWithTraceIDPreservesValidAndReplacesUntrustedValues(t *testing.T) {
	ctx, traceID := WithTraceID(context.Background(), " request-123 ")
	if traceID != "request-123" || FromContext(ctx) != traceID {
		t.Fatalf("valid trace = %q/%q", traceID, FromContext(ctx))
	}
	for _, invalid := range []string{"contains space", "line\nbreak", "非ASCII", strings.Repeat("a", maxTraceIDLength+1)} {
		invalidCtx, generated := WithTraceID(context.Background(), invalid)
		if generated == "" || generated == strings.TrimSpace(invalid) || FromContext(invalidCtx) != generated {
			t.Fatalf("invalid trace %q generated %q", invalid, generated)
		}
	}
}

func TestEnsureReusesContextTrace(t *testing.T) {
	ctx, _ := WithTraceID(context.Background(), "trace-1")
	ensured, traceID := Ensure(ctx)
	if traceID != "trace-1" || FromContext(ensured) != "trace-1" {
		t.Fatalf("Ensure() = %q/%q", traceID, FromContext(ensured))
	}
}
