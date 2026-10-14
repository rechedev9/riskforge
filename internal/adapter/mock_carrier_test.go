// Package adapter_test tests mock carrier behaviour profiles: Alpha, Beta, Gamma.
// REQ-ADAPT-003
package adapter_test

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/rechedev9/riskforge/internal/adapter"
	"github.com/rechedev9/riskforge/internal/domain"
)

var devNull = slog.New(slog.NewTextHandler(io.Discard, nil))

func makeRequest(requestID string) adapter.MockRequest {
	return adapter.MockRequest{
		RequestID:     requestID,
		CoverageLines: []string{"auto"},
	}
}

func TestMockCarrier_AlphaReturnsWithinLatencyBound(t *testing.T) {
	t.Parallel()

	// Worst case latency = 50 + 10 = 60ms — well under 100ms.
	mc := adapter.NewAlpha(devNull)
	ctx := t.Context()

	start := time.Now()
	resp, err := mc.Call(ctx, makeRequest("alpha-001"))
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("REQ-ADAPT-003 Alpha: unexpected error: %v", err)
	}
	if elapsed > 100*time.Millisecond {
		t.Fatalf("REQ-ADAPT-003 Alpha: latency %v exceeds 100ms P99 bound", elapsed)
	}
	if resp.CarrierID != "alpha" {
		t.Fatalf("REQ-ADAPT-003 Alpha: CarrierID = %q, want alpha", resp.CarrierID)
	}
}

func TestMockCarrier_BetaFailureRateApproximatelyTenPercent(t *testing.T) {
	t.Parallel()

	// Test with 500 calls; assert 3%–20% fail (wider bounds to avoid flakiness).
	mc := adapter.NewBeta(devNull)
	ctx := t.Context()

	const calls = 500
	failures := 0

	for i := 0; i < calls; i++ {
		_, err := mc.Call(ctx, makeRequest("beta-rate-test"))
		if err != nil {
			if err == domain.ErrCarrierUnavailable {
				failures++
			} else {
				t.Fatalf("REQ-ADAPT-003 Beta: unexpected error (not ErrCarrierUnavailable): %v", err)
			}
		}
	}

	rate := float64(failures) / float64(calls)
	if rate < 0.03 || rate > 0.20 {
		t.Fatalf("REQ-ADAPT-003 Beta: failure rate %.2f not in [0.03, 0.20] over %d calls", rate, calls)
	}
}

func TestMockCarrier_GammaReturnsInLatencyWindow(t *testing.T) {
	t.Parallel()

	mc := adapter.NewGamma(devNull)
	ctx := t.Context()

	start := time.Now()
	resp, err := mc.Call(ctx, makeRequest("gamma-001"))
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("REQ-ADAPT-003 Gamma: unexpected error: %v", err)
	}
	const lower = 700 * time.Millisecond
	const upper = 950 * time.Millisecond
	if elapsed < lower || elapsed > upper {
		t.Fatalf("REQ-ADAPT-003 Gamma: latency %v not in [%v, %v]", elapsed, lower, upper)
	}
	if resp.CarrierID != "gamma" {
		t.Fatalf("REQ-ADAPT-003 Gamma: CarrierID = %q, want gamma", resp.CarrierID)
	}
}

func TestMockCarrier_CtxCancellationExitsWithin10ms(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		carrier *adapter.MockCarrier
	}{
		{name: "REQ-ADAPT-003 alpha ctx cancel", carrier: adapter.NewAlpha(devNull)},
		{name: "REQ-ADAPT-003 beta ctx cancel", carrier: adapter.NewBeta(devNull)},
		{name: "REQ-ADAPT-003 gamma ctx cancel", carrier: adapter.NewGamma(devNull)},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ctx, cancel := context.WithTimeout(t.Context(), 100*time.Millisecond)
			defer cancel()

			start := time.Now()
			_, err := tc.carrier.Call(ctx, makeRequest("cancel-test"))
			elapsed := time.Since(start)

			_ = err
			if elapsed > 110*time.Millisecond {
				t.Fatalf("%s: took %v after ctx cancel (want ≤110ms)", tc.name, elapsed)
			}
		})
	}
}

func TestMockCarrier_GammaCtxCancellationReturnsError(t *testing.T) {
	t.Parallel()

	mc := adapter.NewGamma(devNull)
	ctx, cancel := context.WithTimeout(t.Context(), 100*time.Millisecond)
	defer cancel()

	start := time.Now()
	_, err := mc.Call(ctx, makeRequest("gamma-cancel"))
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("REQ-ADAPT-003 Gamma ctx cancel: expected error, got nil")
	}
	if elapsed > 110*time.Millisecond {
		t.Fatalf("REQ-ADAPT-003 Gamma ctx cancel: elapsed %v exceeds 110ms grace window", elapsed)
	}
}
