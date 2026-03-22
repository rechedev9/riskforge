//go:build integration

package spanner

import (
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	"testing"

	"cloud.google.com/go/spanner"
)

// shortID returns a deterministic, unique ID that fits within STRING(36).
// It hashes the test name to produce a 32-char hex string.
func shortID(t *testing.T) string {
	t.Helper()
	h := sha256.Sum256([]byte(t.Name()))
	return fmt.Sprintf("%x", h[:16])
}

func newEmulatorClient(t *testing.T) *spanner.Client {
	t.Helper()
	host := os.Getenv("SPANNER_EMULATOR_HOST")
	if host == "" {
		t.Skip("SPANNER_EMULATOR_HOST not set — skipping integration test")
	}
	client, err := NewClient(
		context.Background(),
		envOrDefault("SPANNER_PROJECT", "riskforge-dev"),
		envOrDefault("SPANNER_INSTANCE", "test-instance"),
		envOrDefault("SPANNER_DATABASE", "test-db"),
	)
	if err != nil {
		t.Fatalf("newEmulatorClient: %v", err)
	}
	t.Cleanup(func() { client.Close() })
	return client
}

func envOrDefault(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
