package config

import "os"

// String returns the environment variable value or fallback when it is empty.
func String(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}
