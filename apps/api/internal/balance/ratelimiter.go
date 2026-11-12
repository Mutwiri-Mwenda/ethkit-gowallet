package balance

import (
	"context"
	"sync"
	"sync/atomic"
	"time"
)

type RateSnapshot struct {
	RateWaiting  int64  `json:"rate_waiting"`
	RateAdmitted uint64 `json:"rate_admitted_total"`
	RateCanceled uint64 `json:"rate_canceled_total"`
	RatePerSec   int    `json:"rate_limit_per_second"`
}

type RateLimiter struct {
	interval time.Duration
	mu       sync.Mutex
	next     time.Time
	gate     chan struct{}

	waiting  atomic.Int64
	admitted atomic.Uint64
	canceled atomic.Uint64
}

func NewRateLimiter(requestsPerSecond int) (*RateLimiter, error) {
	if requestsPerSecond <= 0 ||
		requestsPerSecond > int(time.Second) {
		return nil, ErrInvalidConfiguration
	}

	return &RateLimiter{
		interval: time.Second / time.Duration(requestsPerSecond),
		gate:     make(chan struct{}, 1),
	}, nil
}

func (l *RateLimiter) Wait(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	l.waiting.Add(1)
	defer l.waiting.Add(-1)

	// Serialize admission without reserving future slots.
	select {
	case l.gate <- struct{}{}:
	case <-ctx.Done():
		l.canceled.Add(1)
		return ctx.Err()
	}
	defer func() { <-l.gate }()

	l.mu.Lock()
	next := l.next
	l.mu.Unlock()

	delay := time.Until(next)
	if delay > 0 {
		timer := time.NewTimer(delay)
		defer timer.Stop()

		select {
		case <-timer.C:
		case <-ctx.Done():
			l.canceled.Add(1)
			return ctx.Err()
		}
	}

	// A canceled request must not consume a rate slot.
	if err := ctx.Err(); err != nil {
		l.canceled.Add(1)
		return err
	}

	l.mu.Lock()
	l.next = time.Now().Add(l.interval)
	l.mu.Unlock()

	l.admitted.Add(1)
	return nil
}

func (l *RateLimiter) Snapshot() RateSnapshot {
	return RateSnapshot{
		RateWaiting:  l.waiting.Load(),
		RateAdmitted: l.admitted.Load(),
		RateCanceled: l.canceled.Load(),
		RatePerSec:   int(time.Second / l.interval),
	}
}
