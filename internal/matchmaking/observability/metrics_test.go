package observability

import (
	"net/http/httptest"
	"strings"
	"testing"

	platformmetrics "github.com/ccKccK-JF/MatchMind/internal/platform/metrics"
)

func TestMatchmakingMetricsExposeRequiredNames(t *testing.T) {
	registry := platformmetrics.NewRegistry()
	metrics := NewMatchmakingMetrics(registry)
	metrics.SetQueueSize(10)
	metrics.IncMatchAttempt()
	metrics.IncMatchSuccess()
	metrics.IncMatchFailure()
	metrics.IncReservationConflict()
	metrics.ObserveWaitSeconds(12)
	metrics.ObserveQualityScore(91)
	metrics.ObserveWorkerDuration(0.02)

	response := httptest.NewRecorder()
	registry.ServeHTTP(response, httptest.NewRequest("GET", "/metrics", nil))
	body := response.Body.String()
	for _, expected := range []string{
		"match_queue_size 10",
		"match_wait_seconds_count 1",
		"match_attempt_total 1",
		"match_success_total 1",
		"match_failure_total 1",
		"match_quality_score_count 1",
		"ticket_reservation_conflict_total 1",
		"match_worker_duration_seconds_count 1",
	} {
		if !strings.Contains(body, expected) {
			t.Fatalf("metrics body does not contain %q:\n%s", expected, body)
		}
	}
}
