package logging

import (
	"log/slog"
	"os"
)

// Configure installs the process-wide structured JSON logger.
func Configure() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo})))
}
