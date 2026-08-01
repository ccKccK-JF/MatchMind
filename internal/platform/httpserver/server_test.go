package httpserver

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestOperationsEndpoints(t *testing.T) {
	handler := NewHandler(http.NotFoundHandler(), http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		_, _ = response.Write([]byte("metric 1\n"))
	}), nil)

	for path, expectedStatus := range map[string]int{"/health": http.StatusOK, "/ready": http.StatusOK, "/metrics": http.StatusOK} {
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, path, nil))
		if response.Code != expectedStatus {
			t.Fatalf("%s returned %d", path, response.Code)
		}
	}
}

func TestReadinessFailure(t *testing.T) {
	handler := NewHandler(nil, nil, func(_ context.Context) error { return errors.New("offline") })
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/ready", nil))
	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("ready returned %d", response.Code)
	}
}
