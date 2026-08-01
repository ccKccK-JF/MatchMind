package httpserver

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"time"
)

const shutdownTimeout = 10 * time.Second

type ReadinessFunc func(context.Context) error

// NewHandler combines an application handler with conventional process
// endpoints. Readiness may be nil when the process has no downstreams.
func NewHandler(application http.Handler, metricHandler http.Handler, readiness ReadinessFunc) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", func(response http.ResponseWriter, _ *http.Request) {
		writeStatus(response, http.StatusOK, "ok")
	})
	mux.HandleFunc("GET /ready", func(response http.ResponseWriter, request *http.Request) {
		if readiness != nil {
			ctx, cancel := context.WithTimeout(request.Context(), time.Second)
			defer cancel()
			if err := readiness(ctx); err != nil {
				writeStatus(response, http.StatusServiceUnavailable, "not_ready")
				return
			}
		}
		writeStatus(response, http.StatusOK, "ready")
	})
	if metricHandler != nil {
		mux.Handle("GET /metrics", metricHandler)
	}
	if application != nil {
		mux.Handle("/", application)
	}
	return mux
}

func Run(ctx context.Context, serviceName, address string, handler http.Handler) error {
	server := &http.Server{
		Addr:              address,
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
	errCh := make(chan error, 1)
	go func() {
		slog.Info("HTTP service started", "service", serviceName, "address", address)
		errCh <- server.ListenAndServe()
	}()

	select {
	case <-ctx.Done():
		shutdownContext, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
		defer cancel()
		if err := server.Shutdown(shutdownContext); err != nil {
			return err
		}
		slog.Info("HTTP service stopped", "service", serviceName)
		return nil
	case err := <-errCh:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	}
}

func writeStatus(response http.ResponseWriter, statusCode int, status string) {
	response.Header().Set("Content-Type", "application/json")
	response.WriteHeader(statusCode)
	_ = json.NewEncoder(response).Encode(map[string]string{"status": status})
}
