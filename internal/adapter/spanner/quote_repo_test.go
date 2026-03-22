//go:build integration

package spanner

import (
	"context"
	"fmt"
	"testing"
	"time"

	"cloud.google.com/go/spanner"

	"github.com/rechedev9/riskforge/internal/domain"
)

func TestQuoteRepo_Save_FindByRequestID(t *testing.T) {
	client := newEmulatorClient(t)
	repo := NewQuoteRepo(client)
	ctx := context.Background()

	reqID := fmt.Sprintf("test-quote-%s", t.Name())
	results := []domain.QuoteResult{
		{
			CarrierID:  "carrier-a",
			Premium:    domain.Money{Amount: 250000, Currency: "USD"},
			ExpiresAt:  time.Now().Add(10 * time.Minute),
			IsHedged:   false,
			Latency:    150 * time.Millisecond,
			CarrierRef: "ref-a",
		},
		{
			CarrierID:  "carrier-b",
			Premium:    domain.Money{Amount: 180000, Currency: "USD"},
			ExpiresAt:  time.Now().Add(10 * time.Minute),
			IsHedged:   true,
			Latency:    200 * time.Millisecond,
			CarrierRef: "ref-b",
		},
	}

	if err := repo.Save(ctx, reqID, results); err != nil {
		t.Fatalf("Save: %v", err)
	}

	found, ok, err := repo.FindByRequestID(ctx, reqID)
	if err != nil {
		t.Fatalf("FindByRequestID: %v", err)
	}
	if !ok {
		t.Fatal("expected found=true, got false")
	}
	if len(found) != 2 {
		t.Fatalf("expected 2 results, got %d", len(found))
	}
	// Results should be sorted by PremiumCents ASC.
	if found[0].Premium.Amount != 180000 {
		t.Errorf("expected first result premium 180000, got %d", found[0].Premium.Amount)
	}
	if found[1].Premium.Amount != 250000 {
		t.Errorf("expected second result premium 250000, got %d", found[1].Premium.Amount)
	}
}

func TestQuoteRepo_FindByRequestID_Miss(t *testing.T) {
	client := newEmulatorClient(t)
	repo := NewQuoteRepo(client)
	ctx := context.Background()

	reqID := fmt.Sprintf("test-quote-%s", t.Name())
	results, ok, err := repo.FindByRequestID(ctx, reqID)
	if err != nil {
		t.Fatalf("FindByRequestID: %v", err)
	}
	if ok {
		t.Fatal("expected found=false, got true")
	}
	if results != nil {
		t.Fatalf("expected nil results, got %v", results)
	}
}

func TestQuoteRepo_FindByRequestID_IgnoresExpired(t *testing.T) {
	client := newEmulatorClient(t)
	repo := NewQuoteRepo(client)
	ctx := context.Background()

	reqID := fmt.Sprintf("test-quote-%s", t.Name())

	// Insert an expired row via direct mutation.
	cols := []string{"RequestID", "CarrierID", "PremiumCents", "Currency", "ExpiresAt", "IsHedged", "LatencyMs", "CarrierRef", "CreatedAt"}
	m := spanner.InsertOrUpdate("Quotes", cols, []interface{}{
		reqID,
		"carrier-expired",
		int64(100000),
		"USD",
		time.Now().Add(-1 * time.Hour), // expired
		false,
		int64(100),
		"ref-expired",
		spanner.CommitTimestamp,
	})
	if _, err := client.Apply(ctx, []*spanner.Mutation{m}); err != nil {
		t.Fatalf("insert expired row: %v", err)
	}

	_, ok, err := repo.FindByRequestID(ctx, reqID)
	if err != nil {
		t.Fatalf("FindByRequestID: %v", err)
	}
	if ok {
		t.Fatal("expected found=false for expired quote, got true")
	}
}

func TestQuoteRepo_FindByRequestID_MixedExpiredAndValid(t *testing.T) {
	client := newEmulatorClient(t)
	repo := NewQuoteRepo(client)
	ctx := context.Background()

	reqID := fmt.Sprintf("test-quote-%s", t.Name())

	// Insert one valid row via repo.Save.
	validResults := []domain.QuoteResult{
		{
			CarrierID:  "carrier-valid",
			Premium:    domain.Money{Amount: 200000, Currency: "USD"},
			ExpiresAt:  time.Now().Add(10 * time.Minute),
			IsHedged:   false,
			Latency:    100 * time.Millisecond,
			CarrierRef: "ref-valid",
		},
	}
	if err := repo.Save(ctx, reqID, validResults); err != nil {
		t.Fatalf("Save valid: %v", err)
	}

	// Insert one expired row via direct mutation (same requestID, different carrierID).
	cols := []string{"RequestID", "CarrierID", "PremiumCents", "Currency", "ExpiresAt", "IsHedged", "LatencyMs", "CarrierRef", "CreatedAt"}
	m := spanner.InsertOrUpdate("Quotes", cols, []interface{}{
		reqID,
		"carrier-expired-mix",
		int64(150000),
		"USD",
		time.Now().Add(-1 * time.Hour), // expired
		false,
		int64(80),
		"ref-expired-mix",
		spanner.CommitTimestamp,
	})
	if _, err := client.Apply(ctx, []*spanner.Mutation{m}); err != nil {
		t.Fatalf("insert expired row: %v", err)
	}

	// FindByRequestID should return only the valid row.
	found, ok, err := repo.FindByRequestID(ctx, reqID)
	if err != nil {
		t.Fatalf("FindByRequestID: %v", err)
	}
	if !ok {
		t.Fatal("expected found=true (valid row exists), got false")
	}
	if len(found) != 1 {
		t.Fatalf("expected 1 result (only valid), got %d", len(found))
	}
	if found[0].CarrierID != "carrier-valid" {
		t.Errorf("expected carrier-valid, got %s", found[0].CarrierID)
	}
	if found[0].Premium.Amount != 200000 {
		t.Errorf("expected premium 200000, got %d", found[0].Premium.Amount)
	}
}

func TestQuoteRepo_DeleteExpired(t *testing.T) {
	client := newEmulatorClient(t)
	repo := NewQuoteRepo(client)
	ctx := context.Background()

	reqID := fmt.Sprintf("test-quote-%s", t.Name())

	// Insert an expired row via direct mutation.
	cols := []string{"RequestID", "CarrierID", "PremiumCents", "Currency", "ExpiresAt", "IsHedged", "LatencyMs", "CarrierRef", "CreatedAt"}
	m := spanner.InsertOrUpdate("Quotes", cols, []interface{}{
		reqID,
		"carrier-del-expired",
		int64(100000),
		"USD",
		time.Now().Add(-1 * time.Hour), // expired
		false,
		int64(100),
		"ref-del",
		spanner.CommitTimestamp,
	})
	if _, err := client.Apply(ctx, []*spanner.Mutation{m}); err != nil {
		t.Fatalf("insert expired row: %v", err)
	}

	count, err := repo.DeleteExpired(ctx)
	if err != nil {
		t.Fatalf("DeleteExpired: %v", err)
	}
	if count < 1 {
		t.Errorf("expected count >= 1, got %d", count)
	}

	// Verify the row is gone.
	_, ok, err := repo.FindByRequestID(ctx, reqID)
	if err != nil {
		t.Fatalf("FindByRequestID after delete: %v", err)
	}
	if ok {
		t.Fatal("expected found=false after DeleteExpired, got true")
	}
}
