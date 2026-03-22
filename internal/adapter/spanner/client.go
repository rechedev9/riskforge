package spanner

import (
	"context"
	"fmt"

	"cloud.google.com/go/spanner"
)

// NewClient creates a Spanner client. When SPANNER_EMULATOR_HOST is set,
// the client connects to the local emulator instead of GCP.
func NewClient(ctx context.Context, project, instance, database string) (*spanner.Client, error) {
	db := fmt.Sprintf("projects/%s/instances/%s/databases/%s", project, instance, database)
	client, err := spanner.NewClient(ctx, db)
	if err != nil {
		return nil, fmt.Errorf("spanner client: %w", err)
	}
	return client, nil
}
