package spanner

import (
	"context"
	"encoding/json"
	"fmt"

	"cloud.google.com/go/spanner"

	"github.com/rechedev9/riskforge/internal/domain"
)

// CarrierRepo implements ports.CarrierRepository over Spanner.
type CarrierRepo struct {
	client *spanner.Client
}

// NewCarrierRepo returns a CarrierRepository backed by Spanner.
func NewCarrierRepo(client *spanner.Client) *CarrierRepo {
	return &CarrierRepo{client: client}
}

func (r *CarrierRepo) ListActive(ctx context.Context) ([]domain.Carrier, error) {
	stmt := spanner.Statement{
		SQL: `SELECT CarrierId, Name, Code, Config FROM Carriers WHERE IsActive = true`,
	}
	iter := r.client.Single().Query(ctx, stmt)
	defer iter.Stop()

	var carriers []domain.Carrier
	err := iter.Do(func(row *spanner.Row) error {
		var id, name, code string
		var configJSON spanner.NullJSON
		if err := row.Columns(&id, &name, &code, &configJSON); err != nil {
			return err
		}

		c := domain.Carrier{
			ID:       id,
			Code:     code,
			Name:     name,
			IsActive: true,
		}

		if configJSON.Valid {
			raw, err := json.Marshal(configJSON.Value)
			if err == nil {
				var cfg domain.CarrierConfig
				if err := json.Unmarshal(raw, &cfg); err == nil {
					c.Config = cfg
				}
			}
		}

		carriers = append(carriers, c)
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("list carriers: %w", err)
	}
	return carriers, nil
}
