package adapter_test

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/rechedev9/riskforge/internal/adapter"
	"github.com/rechedev9/riskforge/internal/domain"
)

var testLog = slog.New(slog.NewTextHandler(io.Discard, nil))

func retries(n int) *int { return &n }

func mockDeltaServer(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(adapter.DeltaResponse{ //nolint:errcheck
			QuoteID:         "delta-test-001",
			PremiumCents:    120_000,
			ValidForSeconds: 300,
		})
	}))
}

func TestHTTPCarrier_SuccessfulQuote(t *testing.T) {
	t.Parallel()

	srv := mockDeltaServer(t)
	defer srv.Close()

	carrier := adapter.NewDeltaCarrier(adapter.HTTPCarrierConfig{
		BaseURL:    srv.URL,
		MaxRetries: retries(1),
		RetryDelay: 10 * time.Millisecond,
		Timeout:    2 * time.Second,
	}, testLog)

	fn := adapter.RegisterDeltaCarrier(carrier)
	result, err := fn(t.Context(), domain.QuoteRequest{
		RequestID:     "test-req-01",
		CoverageLines: []domain.CoverageLine{domain.CoverageLineAuto},
	})

	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if result.CarrierID != "delta" {
		t.Errorf("carrier_id: want %q, got %q", "delta", result.CarrierID)
	}
	if result.Premium.Amount != 120_000 {
		t.Errorf("premium_cents: want 120000, got %d", result.Premium.Amount)
	}
	if result.Premium.Currency != "USD" {
		t.Errorf("currency: want USD, got %q", result.Premium.Currency)
	}
	if result.ExpiresAt.Before(time.Now().Add(4 * time.Minute)) {
		t.Errorf("expires_at should be ~5 minutes from now, got %v", result.ExpiresAt)
	}
}

func TestHTTPCarrier_RetryOn5xx(t *testing.T) {
	t.Parallel()

	var callCount atomic.Int32

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := callCount.Add(1)
		if n < 3 {
			http.Error(w, "service unavailable", http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(adapter.DeltaResponse{ //nolint:errcheck
			QuoteID:         "retry-test",
			PremiumCents:    80_000,
			ValidForSeconds: 300,
		})
	}))
	defer srv.Close()

	carrier := adapter.NewDeltaCarrier(adapter.HTTPCarrierConfig{
		BaseURL:    srv.URL,
		MaxRetries: retries(3),
		RetryDelay: 5 * time.Millisecond,
		Timeout:    2 * time.Second,
	}, testLog)

	fn := adapter.RegisterDeltaCarrier(carrier)
	result, err := fn(t.Context(), domain.QuoteRequest{
		RequestID:     "retry-req-01",
		CoverageLines: []domain.CoverageLine{domain.CoverageLineAuto},
	})

	if err != nil {
		t.Fatalf("expected success after retries, got: %v", err)
	}
	if result.Premium.Amount != 80_000 {
		t.Errorf("premium_cents: want 80000, got %d", result.Premium.Amount)
	}
	if n := callCount.Load(); n != 3 {
		t.Errorf("expected exactly 3 HTTP calls (2 failures + 1 success), got %d", n)
	}
}

func TestHTTPCarrier_NoRetryOn4xx(t *testing.T) {
	t.Parallel()

	var callCount atomic.Int32

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount.Add(1)
		http.Error(w, "bad request", http.StatusBadRequest)
	}))
	defer srv.Close()

	carrier := adapter.NewDeltaCarrier(adapter.HTTPCarrierConfig{
		BaseURL:    srv.URL,
		MaxRetries: retries(3),
		RetryDelay: 5 * time.Millisecond,
		Timeout:    2 * time.Second,
	}, testLog)

	fn := adapter.RegisterDeltaCarrier(carrier)
	_, err := fn(t.Context(), domain.QuoteRequest{
		RequestID:     "noretry-req-01",
		CoverageLines: []domain.CoverageLine{domain.CoverageLineAuto},
	})

	if err == nil {
		t.Fatal("expected error on 400, got nil")
	}
	if n := callCount.Load(); n != 1 {
		t.Errorf("expected exactly 1 HTTP call for 4xx (no retry), got %d", n)
	}
}

func TestHTTPCarrier_ContextCancellation(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
			return
		case <-time.After(5 * time.Second):
			w.WriteHeader(http.StatusOK)
		}
	}))
	defer srv.Close()

	carrier := adapter.NewDeltaCarrier(adapter.HTTPCarrierConfig{
		BaseURL:    srv.URL,
		MaxRetries: retries(1),
		RetryDelay: 5 * time.Millisecond,
		Timeout:    10 * time.Second,
	}, testLog)

	ctx, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
	defer cancel()

	fn := adapter.RegisterDeltaCarrier(carrier)
	_, err := fn(ctx, domain.QuoteRequest{
		RequestID:     "cancel-req-01",
		CoverageLines: []domain.CoverageLine{domain.CoverageLineAuto},
	})

	if err == nil {
		t.Fatal("expected error on context cancellation, got nil")
	}
}

func TestHTTPCarrier_OversizedResponseRejected(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"quote_id":"`))
		chunk := make([]byte, 32*1024)
		for i := range chunk {
			chunk[i] = 'a'
		}
		for range 352 {
			w.Write(chunk)
		}
		w.Write([]byte(`"}`))
	}))
	defer srv.Close()

	carrier := adapter.NewDeltaCarrier(adapter.HTTPCarrierConfig{
		BaseURL:    srv.URL,
		MaxRetries: retries(0),
		Timeout:    5 * time.Second,
	}, testLog)

	fn := adapter.RegisterDeltaCarrier(carrier)
	_, err := fn(t.Context(), domain.QuoteRequest{
		RequestID:     "oversize-req-01",
		CoverageLines: []domain.CoverageLine{domain.CoverageLineAuto},
	})

	if err == nil {
		t.Fatal("expected error for oversized response, got nil")
	}
}

func TestHTTPCarrier_AllAttemptsExhausted(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "internal server error", http.StatusInternalServerError)
	}))
	defer srv.Close()

	carrier := adapter.NewDeltaCarrier(adapter.HTTPCarrierConfig{
		BaseURL:    srv.URL,
		MaxRetries: retries(2),
		RetryDelay: 5 * time.Millisecond,
		Timeout:    1 * time.Second,
	}, testLog)

	fn := adapter.RegisterDeltaCarrier(carrier)
	_, err := fn(t.Context(), domain.QuoteRequest{
		RequestID:     "exhaust-req-01",
		CoverageLines: []domain.CoverageLine{domain.CoverageLineAuto},
	})

	if err == nil {
		t.Fatal("expected error when all attempts exhausted, got nil")
	}
}

func TestHTTPCarrier_TrailingSlashTrimmed(t *testing.T) {
	t.Parallel()

	var path string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(adapter.DeltaResponse{ //nolint:errcheck
			QuoteID:         "slash",
			PremiumCents:    1,
			ValidForSeconds: 60,
		})
	}))
	defer srv.Close()

	carrier := adapter.NewHTTPCarrier("slash", adapter.HTTPCarrierConfig{
		BaseURL:    srv.URL + "/",
		MaxRetries: retries(0),
		Timeout:    time.Second,
	}, testLog)
	var out adapter.DeltaResponse
	if err := carrier.Post(t.Context(), "/v1/quotes", map[string]string{}, &out); err != nil {
		t.Fatalf("Post: %v", err)
	}
	if path != "/v1/quotes" {
		t.Fatalf("path = %q, want /v1/quotes", path)
	}
}

func TestHTTPCarrier_ZeroRetriesDoesNotRetry(t *testing.T) {
	t.Parallel()

	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		http.Error(w, "nope", http.StatusInternalServerError)
	}))
	defer srv.Close()

	carrier := adapter.NewHTTPCarrier("once", adapter.HTTPCarrierConfig{
		BaseURL:    srv.URL,
		MaxRetries: retries(0),
		RetryDelay: time.Millisecond,
		Timeout:    time.Second,
	}, testLog)
	var out adapter.DeltaResponse
	err := carrier.Post(t.Context(), "/v1/quotes", map[string]string{}, &out)
	if err == nil {
		t.Fatal("expected error")
	}
	if calls.Load() != 1 {
		t.Fatalf("calls = %d, want 1", calls.Load())
	}
}

func TestHTTPCarrier_NilMaxRetriesDefaultsToThree(t *testing.T) {
	t.Parallel()

	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		http.Error(w, "nope", http.StatusBadGateway)
	}))
	defer srv.Close()

	carrier := adapter.NewHTTPCarrier("default-retries", adapter.HTTPCarrierConfig{
		BaseURL:    srv.URL,
		RetryDelay: time.Millisecond,
		Timeout:    time.Second,
	}, testLog)
	var out adapter.DeltaResponse
	if err := carrier.Post(t.Context(), "/v1/quotes", map[string]string{}, &out); err == nil {
		t.Fatal("expected error")
	}
	if calls.Load() != 4 {
		t.Fatalf("calls = %d, want 4 (1 + default 3 retries)", calls.Load())
	}
}

func TestHTTPCarrier_NetworkErrorHasNoStatus(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	srv.Close()

	carrier := adapter.NewHTTPCarrier("down", adapter.HTTPCarrierConfig{
		BaseURL:    srv.URL,
		MaxRetries: retries(0),
		Timeout:    time.Second,
	}, testLog)
	var out adapter.DeltaResponse
	err := carrier.Post(t.Context(), "/v1/quotes", map[string]string{}, &out)
	if err == nil {
		t.Fatal("expected error")
	}
	if strings.Contains(err.Error(), "non-retryable status") {
		t.Fatalf("network failure classified as HTTP status: %v", err)
	}
}
