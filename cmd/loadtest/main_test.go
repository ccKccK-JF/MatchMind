package main

import (
	"context"
	"testing"
	"time"
)

func TestPercentileMilliseconds(t *testing.T) {
	values := []time.Duration{time.Millisecond, 2 * time.Millisecond, 3 * time.Millisecond, 4 * time.Millisecond, 5 * time.Millisecond}
	if got := percentileMilliseconds(values, 0.95); got != 4 {
		t.Fatalf("p95 = %v, want 4", got)
	}
	if got := percentileMilliseconds(values, 0.99); got != 4 {
		t.Fatalf("p99 = %v, want 4", got)
	}
}

func TestRecorderSummary(t *testing.T) {
	recorder := &recorder{}
	recorder.attempted.Add(2)
	recorder.record(10*time.Millisecond, nil)
	recorder.record(0, context.DeadlineExceeded)
	result := recorder.summarize(time.Second)
	if result.Attempted != 2 || result.Successful != 1 || result.Failed != 1 || result.Throughput != 1 {
		t.Fatalf("summary = %+v", result)
	}
}
