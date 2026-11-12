package balance

import (
	"context"
	"errors"
	"sync/atomic"
)

var ErrOverloaded = errors.New("upstream capacity exhausted")

type LimiterSnapshot struct {
	Active   int64  `json:"upstream_active"`
	Waiting  int64  `json:"upstream_waiting"`
	Rejected uint64 `json:"upstream_rejected_total"`
}

type Limiter struct {
	slots   chan struct{}
	queue   chan struct{}
	active  atomic.Int64
	waiting atomic.Int64
	rejects atomic.Uint64
}

func NewLimiter(maxConcurrent, maxWaiting int) (*Limiter, error) {
	if maxConcurrent <= 0 || maxWaiting < 0 {
		return nil, ErrInvalidConfiguration
	}

	return &Limiter{
		slots: make(chan struct{}, maxConcurrent),
		queue: make(chan struct{}, maxWaiting),
	}, nil
}

func (l *Limiter) Acquire(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	// Fast path: acquire an available upstream slot.
	select {
	case l.slots <- struct{}{}:
		l.active.Add(1)
		if err := ctx.Err(); err != nil {
			l.Release()
			return err
		}
		return nil
	default:
	}

	// No slot is available. Try to reserve waiting capacity.
	select {
	case l.queue <- struct{}{}:
		l.waiting.Add(1)
	default:
		l.rejects.Add(1)
		return ErrOverloaded
	}

	defer func() {
		<-l.queue
		l.waiting.Add(-1)
	}()

	select {
	case l.slots <- struct{}{}:
		l.active.Add(1)
		if err := ctx.Err(); err != nil {
			l.Release()
			return err
		}
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (l *Limiter) Release() {
	<-l.slots
	l.active.Add(-1)
}

func (l *Limiter) Snapshot() LimiterSnapshot {
	return LimiterSnapshot{
		Active:   l.active.Load(),
		Waiting:  l.waiting.Load(),
		Rejected: l.rejects.Load(),
	}
}
