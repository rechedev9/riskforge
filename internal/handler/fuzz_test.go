package handler_test

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/rechedev9/riskforge/internal/domain"
	"github.com/rechedev9/riskforge/internal/handler"
	"github.com/rechedev9/riskforge/internal/ports"
	"github.com/rechedev9/riskforge/internal/testutil"
)

// noopOrchestrator always returns an empty result set with no error.
type noopOrchestrator struct{}

func (o *noopOrchestrator) GetQuotes(_ context.Context, _ domain.QuoteRequest) ([]domain.QuoteResult, error) {
	return []domain.QuoteResult{}, nil
}

var _ ports.OrchestratorPort = (*noopOrchestrator)(nil)

// FuzzPostQuotes sends random bytes as the POST /quotes body.
// The handler must never panic regardless of input.
func FuzzPostQuotes(f *testing.F) {
	// Seed corpus: valid, edge-case, and adversarial inputs.
	f.Add([]byte(`{"request_id":"test","coverage_lines":["auto"],"timeout_ms":5000}`))
	f.Add([]byte(`{}`))
	f.Add([]byte(`{"request_id":"","coverage_lines":[]}`))
	f.Add([]byte(`not json at all`))
	f.Add([]byte(`{"request_id":"x","coverage_lines":["auto"],"timeout_ms":-1}`))
	f.Add([]byte(`{"request_id":"x","coverage_lines":["auto"],"timeout_ms":999999}`))
	f.Add(make([]byte, 2<<20)) // 2 MiB (over 1 MiB limit)
	f.Add([]byte(`{"request_id":"` + string(make([]byte, 300)) + `","coverage_lines":["auto"]}`))
	f.Add([]byte("\x00\x01\x02\x03"))
	f.Add([]byte(`[]`))
	f.Add([]byte(`null`))
	f.Add([]byte(`{"request_id":"a","coverage_lines":["auto","homeowners","umbrella"],"timeout_ms":100}`))
	f.Add([]byte(`{"request_id":"a","coverage_lines":["life"]}`))

	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	_ = testutil.NewNoopRecorder()
	reg := prometheus.NewRegistry()

	h := handler.New(handler.HandlerConfig{
		Orch:     &noopOrchestrator{},
		Metrics:  testutil.NewNoopRecorder(),
		Gatherer: reg,
		Log:      log,
		DB:       nil,
	})

	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	f.Fuzz(func(t *testing.T, data []byte) {
		req := httptest.NewRequest(http.MethodPost, "/quotes", bytes.NewReader(data))
		req.Header.Set("Content-Type", "application/json")

		w := httptest.NewRecorder()
		mux.ServeHTTP(w, req)

		// The only assertion: the handler returned a valid HTTP status code
		// and did not panic.
		if w.Code < 200 || w.Code > 599 {
			t.Fatalf("unexpected status code: %d", w.Code)
		}
	})
}
