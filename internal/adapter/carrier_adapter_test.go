package adapter_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/rechedev9/riskforge/internal/adapter"
	"github.com/rechedev9/riskforge/internal/domain"
)

func TestNewCarrierAdapter_RequiresBaseURL(t *testing.T) {
	t.Parallel()
	_, err := adapter.NewCarrierAdapter(domain.Carrier{ID: "carrier-alpha"}, testLog)
	if err == nil {
		t.Fatal("expected error when BaseURL is empty")
	}
}

func TestNewCarrierAdapter_PostsToConfiguredBaseURL(t *testing.T) {
	t.Parallel()

	var path, apiKey string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path = r.URL.Path
		apiKey = r.Header.Get("X-API-Key")
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(adapter.DeltaResponse{
			QuoteID:         "cfg-1",
			PremiumCents:    42,
			ValidForSeconds: 120,
		})
	}))
	defer srv.Close()

	fn, err := adapter.NewCarrierAdapter(domain.Carrier{
		ID: "carrier-alpha",
		Config: domain.CarrierConfig{
			BaseURL:     srv.URL + "/",
			APIKey:      "secret",
			TimeoutHint: time.Second,
		},
	}, testLog)
	if err != nil {
		t.Fatalf("NewCarrierAdapter: %v", err)
	}
	result, err := fn(t.Context(), domain.QuoteRequest{
		RequestID:     "req-1",
		CoverageLines: []domain.CoverageLine{domain.CoverageLineAuto},
	})
	if err != nil {
		t.Fatalf("quote: %v", err)
	}
	if path != "/v1/quotes" {
		t.Fatalf("path = %q, want /v1/quotes", path)
	}
	if apiKey != "secret" {
		t.Fatalf("api key = %q", apiKey)
	}
	if result.CarrierID != "carrier-alpha" {
		t.Fatalf("carrier id = %q", result.CarrierID)
	}
	if result.Premium.Amount != 42 {
		t.Fatalf("premium = %d", result.Premium.Amount)
	}
}
