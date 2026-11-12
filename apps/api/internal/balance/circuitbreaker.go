package balance

import (
	"context"
	"errors"
	"sync"
	"time"
)

var ErrCircuitOpen = errors.New("upstream circuit open")

type CircuitState string

const (
	CircuitClosed   CircuitState = "closed"
	CircuitOpen     CircuitState = "open"
	CircuitHalfOpen CircuitState = "half_open"
)

type CircuitSnapshot struct {
	State    CircuitState `json:"circuit_state"`
	Failures int          `json:"circuit_consecutive_failures"`
	Rejected uint64       `json:"circuit_rejected_total"`
}

type circuitPermit struct {
	probe      bool
	generation uint64
}

type CircuitBreaker struct {
	mu         sync.Mutex
	state      CircuitState
	failures   int
	threshold  int
	cooldown   time.Duration
	openedAt   time.Time
	probing    bool
	rejected   uint64
	generation uint64
}

func NewCircuitBreaker(
	threshold int,
	cooldown time.Duration,
) (*CircuitBreaker, error) {
	if threshold <= 0 || cooldown <= 0 {
		return nil, ErrInvalidConfiguration
	}

	return &CircuitBreaker{
		state:     CircuitClosed,
		threshold: threshold,
		cooldown:  cooldown,
	}, nil
}

func (b *CircuitBreaker) acquire() (circuitPermit, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.state == CircuitOpen &&
		time.Since(b.openedAt) >= b.cooldown {
		b.state = CircuitHalfOpen
		b.probing = false
		b.generation++
	}

	switch b.state {
	case CircuitClosed:
		return circuitPermit{generation: b.generation}, nil

	case CircuitHalfOpen:
		if !b.probing {
			b.probing = true
			return circuitPermit{
				probe:      true,
				generation: b.generation,
			}, nil
		}
	}

	b.rejected++
	return circuitPermit{}, ErrCircuitOpen
}

func (b *CircuitBreaker) complete(
	permit circuitPermit,
	err error,
) {
	b.mu.Lock()
	defer b.mu.Unlock()

	if permit.generation != b.generation {
		return
	}

	if permit.probe {
		if b.state != CircuitHalfOpen {
			return
		}

		b.probing = false

		if errors.Is(err, context.Canceled) ||
			errors.Is(err, context.DeadlineExceeded) {
			b.state = CircuitOpen
			b.openedAt = time.Now().Add(-b.cooldown)
			b.generation++
			return
		}

		if err == nil {
			b.state = CircuitClosed
			b.failures = 0
			b.generation++
			return
		}

		b.state = CircuitOpen
		b.openedAt = time.Now()
		b.generation++
		return
	}

	if b.state != CircuitClosed {
		return
	}

	if err == nil {
		b.failures = 0
		b.generation++
		return
	}

	if !Retryable(err) {
		return
	}

	b.failures++
	b.generation++

	if b.failures >= b.threshold {
		b.state = CircuitOpen
		b.openedAt = time.Now()
	}

}

func (b *CircuitBreaker) Call(
	ctx context.Context,
	fn func(context.Context) error,
) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	permit, err := b.acquire()
	if err != nil {
		return err
	}

	err = fn(ctx)
	b.complete(permit, err)
	return err
}

func (b *CircuitBreaker) Snapshot() CircuitSnapshot {
	b.mu.Lock()
	defer b.mu.Unlock()

	return CircuitSnapshot{
		State:    b.state,
		Failures: b.failures,
		Rejected: b.rejected,
	}
}
