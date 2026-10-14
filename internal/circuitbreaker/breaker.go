// Package circuitbreaker provides a per-carrier circuit breaker
// implemented with sync/atomic operations.
// All methods are safe for concurrent use.
package circuitbreaker

import (
	"context"
	"fmt"
	"sync/atomic"
	"time"

	"github.com/rechedev9/riskforge/internal/domain"
	"github.com/rechedev9/riskforge/internal/ports"
)

type unknownStateError struct {
	state ports.CBState
}

func (e unknownStateError) Error() string {
	return fmt.Sprintf("circuit breaker state %d", int32(e.state))
}

// defaultFailureThreshold is used when Config.FailureThreshold is zero.
const defaultFailureThreshold = 5

// defaultOpenTimeout is used when Config.OpenTimeout is zero.
const defaultOpenTimeout = 30 * time.Second

// Config holds the tuneable parameters for a Breaker instance.
type Config struct {
	// FailureThreshold is the number of consecutive failures that trips the
	// circuit from Closed to Open. Defaults to 5.
	FailureThreshold int
	// SuccessThreshold is the number of consecutive successes in HalfOpen
	// before the circuit closes. Defaults to 1 when zero.
	SuccessThreshold int
	// OpenTimeout is how long the circuit stays Open before transitioning to
	// HalfOpen for a probe. Defaults to 30 seconds.
	OpenTimeout time.Duration
}

// Breaker is a per-carrier 3-state circuit breaker.
// The zero value is not valid — use New.
type Breaker struct {
	carrierID string
	cfg       Config
	metrics   ports.MetricsRecorder

	state atomic.Int32
	// failures counts consecutive failures in the Closed state.
	failures atomic.Int32
	// successes counts consecutive successes in the HalfOpen state.
	successes atomic.Int32
	// halfOpenInFlight is 0 when no probe is in flight, 1 when one is.
	halfOpenInFlight atomic.Int32
	// openedAt is the Unix nanosecond timestamp when the circuit was last opened.
	openedAt atomic.Int64
}

// New returns a Breaker initialised to Closed state with defaults applied to
// zero-valued Config fields.
func New(carrierID string, cfg Config, m ports.MetricsRecorder) *Breaker {
	if cfg.FailureThreshold <= 0 {
		cfg.FailureThreshold = defaultFailureThreshold
	}
	if cfg.SuccessThreshold <= 0 {
		cfg.SuccessThreshold = 1
	}
	if cfg.OpenTimeout <= 0 {
		cfg.OpenTimeout = defaultOpenTimeout
	}
	b := &Breaker{
		carrierID: carrierID,
		cfg:       cfg,
		metrics:   m,
	}
	// Emit initial gauge so Prometheus is seeded from the start.
	m.SetCBState(carrierID, ports.CBStateClosed)
	return b
}

// State returns the current CBState of the breaker.
func (b *Breaker) State() ports.CBState {
	return ports.CBState(b.state.Load())
}

// Execute runs fn through the circuit breaker using the following rules:
//
//   - Closed: fn is called. On failure the failure counter increments; when it
//     reaches FailureThreshold the circuit trips to Open.
//   - Open: if OpenTimeout has not elapsed, domain.ErrCircuitOpen is returned
//     immediately without calling fn. After OpenTimeout the circuit transitions
//     to HalfOpen and falls through.
//   - HalfOpen: exactly one probe (halfOpenInFlight CAS 0→1) is allowed. If a
//     second concurrent call arrives it receives domain.ErrCircuitOpen. On probe
//     success the circuit closes after SuccessThreshold successes; on probe
//     failure it re-opens.
func (b *Breaker) Execute(ctx context.Context, fn func() error) error {
	current := b.state.Load()

	switch ports.CBState(current) {
	case ports.CBStateOpen:
		if time.Since(time.Unix(0, b.openedAt.Load())) < b.cfg.OpenTimeout {
			return domain.ErrCircuitOpen
		}
		if b.cas(ports.CBStateOpen, ports.CBStateHalfOpen) {
			b.metrics.RecordCBTransition(b.carrierID, ports.CBStateOpen, ports.CBStateHalfOpen)
			b.metrics.SetCBState(b.carrierID, ports.CBStateHalfOpen)
		}
		return b.executeHalfOpen(ctx, fn)

	case ports.CBStateHalfOpen:
		return b.executeHalfOpen(ctx, fn)

	case ports.CBStateClosed:
		return b.executeClosed(ctx, fn)

	default:
		return unknownStateError{state: ports.CBState(current)}
	}
}

func (b *Breaker) cas(from, to ports.CBState) bool {
	return b.state.CompareAndSwap(int32(from), int32(to))
}

// executeClosed runs fn in the Closed state, managing the failure counter and
// tripping to Open when FailureThreshold is reached.
func (b *Breaker) executeClosed(ctx context.Context, fn func() error) error {
	if err := fn(); err != nil {
		newFailures := b.failures.Add(1)
		if newFailures >= int32(b.cfg.FailureThreshold) {
			if b.cas(ports.CBStateClosed, ports.CBStateOpen) {
				b.openedAt.Store(time.Now().UnixNano())
				b.metrics.RecordCBTransition(b.carrierID, ports.CBStateClosed, ports.CBStateOpen)
				b.metrics.SetCBState(b.carrierID, ports.CBStateOpen)
			}
		}
		return err
	}
	b.failures.Store(0)
	return nil
}

// executeHalfOpen enforces exactly-one probe concurrency and handles the
// HalfOpen→Closed and HalfOpen→Open transitions.
func (b *Breaker) executeHalfOpen(ctx context.Context, fn func() error) error {
	if !b.halfOpenInFlight.CompareAndSwap(0, 1) {
		return domain.ErrCircuitOpen
	}
	defer b.halfOpenInFlight.Store(0)

	if err := fn(); err != nil {
		if b.cas(ports.CBStateHalfOpen, ports.CBStateOpen) {
			b.openedAt.Store(time.Now().UnixNano())
			b.successes.Store(0)
			b.metrics.RecordCBTransition(b.carrierID, ports.CBStateHalfOpen, ports.CBStateOpen)
			b.metrics.SetCBState(b.carrierID, ports.CBStateOpen)
		}
		return err
	}

	newSuccesses := b.successes.Add(1)
	if newSuccesses >= int32(b.cfg.SuccessThreshold) {
		if b.cas(ports.CBStateHalfOpen, ports.CBStateClosed) {
			b.failures.Store(0)
			b.successes.Store(0)
			b.metrics.RecordCBTransition(b.carrierID, ports.CBStateHalfOpen, ports.CBStateClosed)
			b.metrics.SetCBState(b.carrierID, ports.CBStateClosed)
		}
	}
	return nil
}

var _ interface {
	Execute(context.Context, func() error) error
	State() ports.CBState
} = (*Breaker)(nil)
