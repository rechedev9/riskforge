package ports

import (
	"context"

	"github.com/rechedev9/riskforge/internal/domain"
)

// AppetiteRepository provides access to carrier appetite rules.
type AppetiteRepository interface {
	// FindMatchingRules returns active appetite rules that match the given risk classification.
	// Returns an empty slice if no rules match.
	FindMatchingRules(ctx context.Context, risk domain.RiskClassification) ([]domain.AppetiteRule, error)

	// ListAll returns all active appetite rules. Used for cache population.
	ListAll(ctx context.Context) ([]domain.AppetiteRule, error)
}

// CarrierRepository provides access to carrier definitions.
type CarrierRepository interface {
	// ListActive returns all active carriers with their configurations.
	ListActive(ctx context.Context) ([]domain.Carrier, error)
}
