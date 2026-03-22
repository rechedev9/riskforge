package orchestrator_test

import (
	"testing"

	"go.uber.org/goleak"
)

func TestMain(m *testing.M) {
	goleak.VerifyTestMain(m,
		goleak.IgnoreTopFunction("golang.org/x/sync/singleflight.(*Group).doCall"),
		goleak.IgnoreTopFunction("github.com/rechedev9/riskforge/internal/orchestrator.(*Orchestrator).fanOut"),
		goleak.IgnoreTopFunction("github.com/rechedev9/riskforge/internal/orchestrator.hedgeMonitor"),
		goleak.IgnoreTopFunction("golang.org/x/sync/errgroup.(*Group).Go.func1"),
		goleak.IgnoreAnyFunction("internal/poll.runtime_pollWait"),
	)
}
