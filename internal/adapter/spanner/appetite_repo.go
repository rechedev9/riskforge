package spanner

import (
	"context"
	"encoding/json"
	"fmt"

	"cloud.google.com/go/spanner"

	"github.com/rechedev9/riskforge/internal/domain"
)

// AppetiteRepo implements ports.AppetiteRepository over Spanner.
type AppetiteRepo struct {
	client *spanner.Client
}

// NewAppetiteRepo returns an AppetiteRepository backed by Spanner.
func NewAppetiteRepo(client *spanner.Client) *AppetiteRepo {
	return &AppetiteRepo{client: client}
}

func (r *AppetiteRepo) FindMatchingRules(ctx context.Context, risk domain.RiskClassification) ([]domain.AppetiteRule, error) {
	sql := `SELECT RuleId, CarrierId, State, LineOfBusiness, ClassCode, MinPremium, MaxPremium, EligibilityCriteria
	        FROM AppetiteRules
	        WHERE IsActive = true AND State = @state AND LineOfBusiness = @lob`
	params := map[string]interface{}{
		"state": risk.State,
		"lob":   risk.LineOfBusiness,
	}

	if risk.ClassCode != "" {
		sql += ` AND (ClassCode IS NULL OR ClassCode = @classCode)`
		params["classCode"] = risk.ClassCode
	}

	if risk.EstimatedPremium > 0 {
		sql += ` AND (MinPremium IS NULL OR MinPremium <= @premium)
		         AND (MaxPremium IS NULL OR MaxPremium >= @premium)`
		params["premium"] = risk.EstimatedPremium
	}

	stmt := spanner.Statement{SQL: sql, Params: params}
	return r.queryRules(ctx, stmt)
}

func (r *AppetiteRepo) ListAll(ctx context.Context) ([]domain.AppetiteRule, error) {
	stmt := spanner.Statement{
		SQL: `SELECT RuleId, CarrierId, State, LineOfBusiness, ClassCode, MinPremium, MaxPremium, EligibilityCriteria
		      FROM AppetiteRules WHERE IsActive = true`,
	}
	return r.queryRules(ctx, stmt)
}

func (r *AppetiteRepo) queryRules(ctx context.Context, stmt spanner.Statement) ([]domain.AppetiteRule, error) {
	iter := r.client.Single().Query(ctx, stmt)
	defer iter.Stop()

	var rules []domain.AppetiteRule
	err := iter.Do(func(row *spanner.Row) error {
		var ruleID, carrierID, state, lob string
		var classCode spanner.NullString
		var minPremium, maxPremium spanner.NullFloat64
		var criteria spanner.NullJSON

		if err := row.Columns(&ruleID, &carrierID, &state, &lob, &classCode, &minPremium, &maxPremium, &criteria); err != nil {
			return err
		}

		rule := domain.AppetiteRule{
			RuleID:         ruleID,
			CarrierID:      carrierID,
			State:          state,
			LineOfBusiness: lob,
			IsActive:       true,
		}
		if classCode.Valid {
			rule.ClassCode = classCode.StringVal
		}
		if minPremium.Valid {
			rule.MinPremium = minPremium.Float64
		}
		if maxPremium.Valid {
			rule.MaxPremium = maxPremium.Float64
		}
		if criteria.Valid {
			raw, _ := json.Marshal(criteria.Value)
			rule.EligibilityCriteria = json.RawMessage(raw)
		}

		rules = append(rules, rule)
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("query appetite rules: %w", err)
	}
	return rules, nil
}
