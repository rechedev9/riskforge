//go:build integration

package spanner

import (
	"context"
	"testing"
	"time"

	"cloud.google.com/go/spanner"
)

func TestCarrierRepo_ListActive_ReturnsActive(t *testing.T) {
	client := newEmulatorClient(t)
	repo := NewCarrierRepo(client)
	ctx := context.Background()

	base := shortID(t)
	id1 := base[:14] + "-a1"
	id2 := base[:14] + "-a2"
	id3 := base[:14] + "-in"
	cols := []string{"CarrierId", "Name", "Code", "IsActive", "CreatedAt", "UpdatedAt", "Config"}

	mutations := []*spanner.Mutation{
		spanner.InsertOrUpdate("Carriers", cols, []interface{}{
			id1, "Active One", "ACT1", true,
			spanner.CommitTimestamp, spanner.CommitTimestamp, spanner.NullJSON{},
		}),
		spanner.InsertOrUpdate("Carriers", cols, []interface{}{
			id2, "Active Two", "ACT2", true,
			spanner.CommitTimestamp, spanner.CommitTimestamp, spanner.NullJSON{},
		}),
		spanner.InsertOrUpdate("Carriers", cols, []interface{}{
			id3, "Inactive One", "INACT", false,
			spanner.CommitTimestamp, spanner.CommitTimestamp, spanner.NullJSON{},
		}),
	}
	if _, err := client.Apply(ctx, mutations); err != nil {
		t.Fatalf("insert carriers: %v", err)
	}

	carriers, err := repo.ListActive(ctx)
	if err != nil {
		t.Fatalf("ListActive: %v", err)
	}

	// Count our test carriers in the result set (other tests may have inserted rows).
	activeCount := 0
	inactiveFound := false
	for _, c := range carriers {
		switch c.ID {
		case id1, id2:
			activeCount++
		case id3:
			inactiveFound = true
		}
	}

	if activeCount != 2 {
		t.Errorf("expected 2 active test carriers, found %d", activeCount)
	}
	if inactiveFound {
		t.Error("inactive carrier should not appear in ListActive results")
	}
}

func TestCarrierRepo_ListActive_DecodesConfig(t *testing.T) {
	client := newEmulatorClient(t)
	repo := NewCarrierRepo(client)
	ctx := context.Background()

	carrierID := shortID(t)
	cols := []string{"CarrierId", "Name", "Code", "IsActive", "CreatedAt", "UpdatedAt", "Config"}

	configJSON := spanner.NullJSON{
		Value: map[string]interface{}{
			"TimeoutHint":      float64(200000000), // 200ms in nanoseconds
			"FailureThreshold": float64(3),
			"RateLimit": map[string]interface{}{
				"TokensPerSecond": float64(50),
				"Burst":           float64(5),
			},
		},
		Valid: true,
	}

	m := spanner.InsertOrUpdate("Carriers", cols, []interface{}{
		carrierID, "Config Carrier", "CFG1", true,
		spanner.CommitTimestamp, spanner.CommitTimestamp, configJSON,
	})
	if _, err := client.Apply(ctx, []*spanner.Mutation{m}); err != nil {
		t.Fatalf("insert carrier: %v", err)
	}

	carriers, err := repo.ListActive(ctx)
	if err != nil {
		t.Fatalf("ListActive: %v", err)
	}

	var found bool
	for _, c := range carriers {
		if c.ID != carrierID {
			continue
		}
		found = true
		if c.Config.TimeoutHint != 200*time.Millisecond {
			t.Errorf("expected TimeoutHint 200ms, got %v", c.Config.TimeoutHint)
		}
		if c.Config.FailureThreshold != 3 {
			t.Errorf("expected FailureThreshold 3, got %d", c.Config.FailureThreshold)
		}
		if c.Config.RateLimit.TokensPerSecond != 50 {
			t.Errorf("expected TokensPerSecond 50, got %f", c.Config.RateLimit.TokensPerSecond)
		}
		if c.Config.RateLimit.Burst != 5 {
			t.Errorf("expected Burst 5, got %d", c.Config.RateLimit.Burst)
		}
	}
	if !found {
		t.Fatalf("carrier %s not found in ListActive results", carrierID)
	}
}

func TestCarrierRepo_ListActive_NullConfig(t *testing.T) {
	client := newEmulatorClient(t)
	repo := NewCarrierRepo(client)
	ctx := context.Background()

	carrierID := shortID(t)
	cols := []string{"CarrierId", "Name", "Code", "IsActive", "CreatedAt", "UpdatedAt", "Config"}

	m := spanner.InsertOrUpdate("Carriers", cols, []interface{}{
		carrierID, "Null Config Carrier", "NUL1", true,
		spanner.CommitTimestamp, spanner.CommitTimestamp, spanner.NullJSON{},
	})
	if _, err := client.Apply(ctx, []*spanner.Mutation{m}); err != nil {
		t.Fatalf("insert carrier: %v", err)
	}

	carriers, err := repo.ListActive(ctx)
	if err != nil {
		t.Fatalf("ListActive: %v", err)
	}

	var found bool
	for _, c := range carriers {
		if c.ID != carrierID {
			continue
		}
		found = true
		if c.Config.TimeoutHint != 0 {
			t.Errorf("expected zero TimeoutHint, got %v", c.Config.TimeoutHint)
		}
		if c.Config.FailureThreshold != 0 {
			t.Errorf("expected zero FailureThreshold, got %d", c.Config.FailureThreshold)
		}
	}
	if !found {
		t.Fatalf("carrier %s not found in ListActive results", carrierID)
	}
}
