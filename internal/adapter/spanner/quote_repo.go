package spanner

import (
	"context"
	"fmt"
	"time"

	"cloud.google.com/go/spanner"

	"github.com/rechedev9/riskforge/internal/domain"
)

// QuoteRepo implements ports.QuoteRepository over Spanner.
type QuoteRepo struct {
	client *spanner.Client
}

// NewQuoteRepo returns a QuoteRepository backed by Spanner.
func NewQuoteRepo(client *spanner.Client) *QuoteRepo {
	return &QuoteRepo{client: client}
}

func (r *QuoteRepo) Save(ctx context.Context, requestID string, results []domain.QuoteResult) error {
	var mutations []*spanner.Mutation
	for _, q := range results {
		m, err := spanner.InsertOrUpdateStruct("Quotes", &quoteRow{
			RequestID:    requestID,
			CarrierID:    q.CarrierID,
			PremiumCents: q.Premium.Amount,
			Currency:     q.Premium.Currency,
			ExpiresAt:    q.ExpiresAt,
			IsHedged:     q.IsHedged,
			LatencyMs:    q.Latency.Milliseconds(),
			CarrierRef:   q.CarrierRef,
			CreatedAt:    spanner.CommitTimestamp,
		})
		if err != nil {
			return fmt.Errorf("build mutation: %w", err)
		}
		mutations = append(mutations, m)
	}
	_, err := r.client.Apply(ctx, mutations)
	if err != nil {
		return fmt.Errorf("save quotes: %w", err)
	}
	return nil
}

func (r *QuoteRepo) FindByRequestID(ctx context.Context, requestID string) ([]domain.QuoteResult, bool, error) {
	stmt := spanner.Statement{
		SQL: `SELECT CarrierID, PremiumCents, Currency, ExpiresAt, IsHedged, LatencyMs, CarrierRef
		      FROM Quotes
		      WHERE RequestID = @requestID AND ExpiresAt > CURRENT_TIMESTAMP()
		      ORDER BY PremiumCents ASC`,
		Params: map[string]interface{}{"requestID": requestID},
	}
	iter := r.client.Single().Query(ctx, stmt)
	defer iter.Stop()

	var results []domain.QuoteResult
	err := iter.Do(func(row *spanner.Row) error {
		var q quoteRow
		if err := row.ToStruct(&q); err != nil {
			return err
		}
		results = append(results, domain.QuoteResult{
			RequestID:  requestID,
			CarrierID:  q.CarrierID,
			Premium:    domain.Money{Amount: q.PremiumCents, Currency: q.Currency},
			ExpiresAt:  q.ExpiresAt,
			IsHedged:   q.IsHedged,
			Latency:    time.Duration(q.LatencyMs) * time.Millisecond,
			CarrierRef: q.CarrierRef,
		})
		return nil
	})
	if err != nil {
		return nil, false, fmt.Errorf("find quotes: %w", err)
	}
	if len(results) == 0 {
		return nil, false, nil
	}
	return results, true, nil
}

func (r *QuoteRepo) DeleteExpired(ctx context.Context) (int64, error) {
	count, err := r.client.PartitionedUpdate(ctx, spanner.Statement{
		SQL: `DELETE FROM Quotes WHERE ExpiresAt <= CURRENT_TIMESTAMP()`,
	})
	if err != nil {
		return 0, fmt.Errorf("delete expired: %w", err)
	}
	return count, nil
}

// quoteRow maps to the Spanner Quotes table.
type quoteRow struct {
	RequestID    string    `spanner:"RequestID"`
	CarrierID    string    `spanner:"CarrierID"`
	PremiumCents int64     `spanner:"PremiumCents"`
	Currency     string    `spanner:"Currency"`
	ExpiresAt    time.Time `spanner:"ExpiresAt"`
	IsHedged     bool      `spanner:"IsHedged"`
	LatencyMs    int64     `spanner:"LatencyMs"`
	CarrierRef   string    `spanner:"CarrierRef"`
	CreatedAt    time.Time `spanner:"CreatedAt"`
}
