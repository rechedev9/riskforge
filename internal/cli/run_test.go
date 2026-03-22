package cli_test

import (
	"context"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/rechedev9/riskforge/internal/cli"
)

func TestRun_MissingAPIKeys(t *testing.T) {
	t.Setenv("API_KEYS", "")
	// Clear Spanner vars to avoid connection attempts.
	t.Setenv("SPANNER_PROJECT", "")
	t.Setenv("SPANNER_INSTANCE", "")
	t.Setenv("SPANNER_DATABASE", "")

	err := cli.Run(context.Background(), nil, io.Discard, io.Discard)
	if err == nil {
		t.Fatal("expected error when API_KEYS is empty")
	}
	if !strings.Contains(err.Error(), "API_KEYS environment variable required") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRun_CleanShutdown(t *testing.T) {
	port := freePort(t)
	t.Setenv("API_KEYS", "test-key-for-shutdown")
	t.Setenv("PORT", port)
	// No Spanner — uses mock carriers.
	t.Setenv("SPANNER_PROJECT", "")
	t.Setenv("SPANNER_INSTANCE", "")
	t.Setenv("SPANNER_DATABASE", "")

	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() {
		errCh <- cli.Run(ctx, nil, io.Discard, io.Discard)
	}()

	// Poll /healthz until ready.
	pollHealthz(t, "http://127.0.0.1:"+port+"/healthz", 5*time.Second)

	// Cancel context to trigger graceful shutdown.
	cancel()

	// Wait for Run to return.
	select {
	case err := <-errCh:
		if err != nil {
			t.Fatalf("Run returned error after clean shutdown: %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("Run did not return within 10s after context cancel")
	}
}

// freePort picks an available TCP port on localhost.
func freePort(t *testing.T) string {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("freePort: %v", err)
	}
	port := l.Addr().(*net.TCPAddr).Port
	l.Close()
	return strconv.Itoa(port)
}

// pollHealthz retries GET on the given URL until it returns 200 or the
// timeout elapses.
func pollHealthz(t *testing.T, url string, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		resp, err := http.Get(url)
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode == 200 {
				return
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("healthz at %s not ready after %v", url, timeout)
}
