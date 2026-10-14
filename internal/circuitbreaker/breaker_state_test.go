package circuitbreaker

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/rechedev9/riskforge/internal/ports"
	"github.com/rechedev9/riskforge/internal/testutil"
)

func TestExecute_UnknownStateRejected(t *testing.T) {
	t.Parallel()
	rec := testutil.NewNoopRecorder()
	b := New("carrier", Config{
		FailureThreshold: 1,
		SuccessThreshold: 1,
		OpenTimeout:      time.Second,
	}, rec)
	b.state.Store(99)
	called := false
	err := b.Execute(context.Background(), func() error {
		called = true
		return nil
	})
	if called {
		t.Fatal("fn ran for an unknown state")
	}
	var unknown unknownStateError
	if !errors.As(err, &unknown) {
		t.Fatalf("got %v, want unknownStateError", err)
	}
	if unknown.state != ports.CBState(99) {
		t.Fatalf("state = %d", unknown.state)
	}
}
