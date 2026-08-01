package metrics

import (
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRegistryExposesPrometheusText(t *testing.T) {
	registry := NewRegistry()
	counter := registry.NewCounter("requests_total", "Handled requests.")
	gauge := registry.NewGauge("queue_size", "Current queue size.")
	histogram := registry.NewHistogram("duration_seconds", "Request duration.", []float64{0.1, 1})
	counter.Add(2)
	gauge.Set(3)
	histogram.Observe(0.5)

	response := httptest.NewRecorder()
	registry.ServeHTTP(response, httptest.NewRequest("GET", "/metrics", nil))
	body := response.Body.String()
	for _, expected := range []string{
		"requests_total 2",
		"queue_size 3",
		"duration_seconds_bucket{le=\"0.1\"} 0",
		"duration_seconds_bucket{le=\"1\"} 1",
		"duration_seconds_count 1",
	} {
		if !strings.Contains(body, expected) {
			t.Fatalf("metrics body does not contain %q:\n%s", expected, body)
		}
	}
}
