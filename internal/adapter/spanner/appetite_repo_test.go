//go:build integration

package spanner

import (
	"context"
	"testing"

	"cloud.google.com/go/spanner"

	"github.com/rechedev9/riskforge/internal/domain"
)

// insertParentCarrier inserts a minimal Carriers row so that interleaved
// AppetiteRules rows can reference it.
func insertParentCarrier(t *testing.T, client *spanner.Client, carrierID string) {
	t.Helper()
	cols := []string{"CarrierId", "Name", "Code", "IsActive", "CreatedAt", "UpdatedAt", "Config"}
	m := spanner.InsertOrUpdate("Carriers", cols, []interface{}{
		carrierID, "Test Carrier " + carrierID, "TC-" + carrierID, true,
		spanner.CommitTimestamp, spanner.CommitTimestamp, spanner.NullJSON{},
	})
	if _, err := client.Apply(context.Background(), []*spanner.Mutation{m}); err != nil {
		t.Fatalf("insertParentCarrier(%s): %v", carrierID, err)
	}
}

func TestAppetiteRepo_FindMatchingRules_RequiredOnly(t *testing.T) {
	client := newEmulatorClient(t)
	repo := NewAppetiteRepo(client)
	ctx := context.Background()

	carrierID := shortID(t)
	insertParentCarrier(t, client, carrierID)

	cols := []string{"CarrierId", "RuleId", "State", "LineOfBusiness", "ClassCode", "MinPremium", "MaxPremium", "IsActive", "CreatedAt"}
	mutations := []*spanner.Mutation{
		spanner.InsertOrUpdate("AppetiteRules", cols, []interface{}{
			carrierID, "rule-ca", "CA", "auto",
			spanner.NullString{}, spanner.NullFloat64{}, spanner.NullFloat64{},
			true, spanner.CommitTimestamp,
		}),
		spanner.InsertOrUpdate("AppetiteRules", cols, []interface{}{
			carrierID, "rule-ny", "NY", "auto",
			spanner.NullString{}, spanner.NullFloat64{}, spanner.NullFloat64{},
			true, spanner.CommitTimestamp,
		}),
	}
	if _, err := client.Apply(ctx, mutations); err != nil {
		t.Fatalf("insert rules: %v", err)
	}

	rules, err := repo.FindMatchingRules(ctx, domain.RiskClassification{
		State:          "CA",
		LineOfBusiness: "auto",
	})
	if err != nil {
		t.Fatalf("FindMatchingRules: %v", err)
	}
	if len(rules) != 1 {
		t.Fatalf("expected 1 rule, got %d", len(rules))
	}
	if rules[0].State != "CA" {
		t.Errorf("expected state CA, got %s", rules[0].State)
	}
}

func TestAppetiteRepo_FindMatchingRules_WithClassCode(t *testing.T) {
	client := newEmulatorClient(t)
	repo := NewAppetiteRepo(client)
	ctx := context.Background()

	carrierID := shortID(t)
	insertParentCarrier(t, client, carrierID)

	cols := []string{"CarrierId", "RuleId", "State", "LineOfBusiness", "ClassCode", "MinPremium", "MaxPremium", "IsActive", "CreatedAt"}
	mutations := []*spanner.Mutation{
		// Rule with specific ClassCode.
		spanner.InsertOrUpdate("AppetiteRules", cols, []interface{}{
			carrierID, "rule-specific", "CA", "auto",
			spanner.NullString{StringVal: "auto-commercial", Valid: true},
			spanner.NullFloat64{}, spanner.NullFloat64{},
			true, spanner.CommitTimestamp,
		}),
		// Rule with NULL ClassCode (wildcard).
		spanner.InsertOrUpdate("AppetiteRules", cols, []interface{}{
			carrierID, "rule-wildcard", "CA", "auto",
			spanner.NullString{}, spanner.NullFloat64{}, spanner.NullFloat64{},
			true, spanner.CommitTimestamp,
		}),
	}
	if _, err := client.Apply(ctx, mutations); err != nil {
		t.Fatalf("insert rules: %v", err)
	}

	rules, err := repo.FindMatchingRules(ctx, domain.RiskClassification{
		State:          "CA",
		LineOfBusiness: "auto",
		ClassCode:      "auto-commercial",
	})
	if err != nil {
		t.Fatalf("FindMatchingRules: %v", err)
	}
	// Both rules should match: NULL ClassCode acts as wildcard, specific matches exactly.
	if len(rules) != 2 {
		t.Fatalf("expected 2 rules (wildcard + specific), got %d", len(rules))
	}
}

func TestAppetiteRepo_FindMatchingRules_PremiumRange(t *testing.T) {
	client := newEmulatorClient(t)
	repo := NewAppetiteRepo(client)
	ctx := context.Background()

	carrierID := shortID(t)
	insertParentCarrier(t, client, carrierID)

	cols := []string{"CarrierId", "RuleId", "State", "LineOfBusiness", "ClassCode", "MinPremium", "MaxPremium", "IsActive", "CreatedAt"}
	m := spanner.InsertOrUpdate("AppetiteRules", cols, []interface{}{
		carrierID, "rule-range", "CA", "auto",
		spanner.NullString{},
		spanner.NullFloat64{Float64: 1000, Valid: true},
		spanner.NullFloat64{Float64: 5000, Valid: true},
		true, spanner.CommitTimestamp,
	})
	if _, err := client.Apply(ctx, []*spanner.Mutation{m}); err != nil {
		t.Fatalf("insert rule: %v", err)
	}

	// Within range — should match.
	rules, err := repo.FindMatchingRules(ctx, domain.RiskClassification{
		State:            "CA",
		LineOfBusiness:   "auto",
		EstimatedPremium: 3000,
	})
	if err != nil {
		t.Fatalf("FindMatchingRules (in range): %v", err)
	}
	if len(rules) != 1 {
		t.Fatalf("expected 1 rule for premium 3000, got %d", len(rules))
	}

	// Outside range — should not match.
	rules, err = repo.FindMatchingRules(ctx, domain.RiskClassification{
		State:            "CA",
		LineOfBusiness:   "auto",
		EstimatedPremium: 6000,
	})
	if err != nil {
		t.Fatalf("FindMatchingRules (out of range): %v", err)
	}
	if len(rules) != 0 {
		t.Fatalf("expected 0 rules for premium 6000, got %d", len(rules))
	}
}

func TestAppetiteRepo_FindMatchingRules_NoMatch(t *testing.T) {
	client := newEmulatorClient(t)
	repo := NewAppetiteRepo(client)
	ctx := context.Background()

	rules, err := repo.FindMatchingRules(ctx, domain.RiskClassification{
		State:          "ZZ",
		LineOfBusiness: "nonexistent",
	})
	if err != nil {
		t.Fatalf("FindMatchingRules: %v", err)
	}
	if len(rules) != 0 {
		t.Fatalf("expected 0 rules, got %d", len(rules))
	}
}

func TestAppetiteRepo_ListAll(t *testing.T) {
	client := newEmulatorClient(t)
	repo := NewAppetiteRepo(client)
	ctx := context.Background()

	// Insert 3 rules across 2 parent carriers.
	base := shortID(t)
	carrier1 := base[:15] + "-c1"
	carrier2 := base[:15] + "-c2"
	insertParentCarrier(t, client, carrier1)
	insertParentCarrier(t, client, carrier2)

	cols := []string{"CarrierId", "RuleId", "State", "LineOfBusiness", "ClassCode", "MinPremium", "MaxPremium", "IsActive", "CreatedAt"}
	mutations := []*spanner.Mutation{
		spanner.InsertOrUpdate("AppetiteRules", cols, []interface{}{
			carrier1, "rule-1", "CA", "auto",
			spanner.NullString{}, spanner.NullFloat64{}, spanner.NullFloat64{},
			true, spanner.CommitTimestamp,
		}),
		spanner.InsertOrUpdate("AppetiteRules", cols, []interface{}{
			carrier1, "rule-2", "TX", "homeowners",
			spanner.NullString{}, spanner.NullFloat64{}, spanner.NullFloat64{},
			true, spanner.CommitTimestamp,
		}),
		spanner.InsertOrUpdate("AppetiteRules", cols, []interface{}{
			carrier2, "rule-3", "NY", "auto",
			spanner.NullString{}, spanner.NullFloat64{}, spanner.NullFloat64{},
			true, spanner.CommitTimestamp,
		}),
	}
	if _, err := client.Apply(ctx, mutations); err != nil {
		t.Fatalf("insert rules: %v", err)
	}

	rules, err := repo.ListAll(ctx)
	if err != nil {
		t.Fatalf("ListAll: %v", err)
	}

	// Count our test rules in the result set.
	testRuleCount := 0
	for _, r := range rules {
		switch {
		case r.CarrierID == carrier1 && (r.RuleID == "rule-1" || r.RuleID == "rule-2"):
			testRuleCount++
		case r.CarrierID == carrier2 && r.RuleID == "rule-3":
			testRuleCount++
		}
	}
	if testRuleCount != 3 {
		t.Errorf("expected 3 test rules in ListAll, found %d", testRuleCount)
	}
}
