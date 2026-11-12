package balance

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

func TestRateLimiterRejectsInvalidConfiguration(t *testing.T) {
	for _, rate := range []int{-1, 0, int(time.Second) + 1} {
		if _, err := NewRateLimiter(rate); !errors.Is(err, ErrInvalidConfiguration) {
			t.Fatalf("rate %d: expected configuration error, got %v", rate, err)
		}
	}
}

func TestRateLimiterSpacesAdmissions(t *testing.T) {
	const rate = 20
	const requests = 5

	limiter, err := NewRateLimiter(rate)
	if err != nil {
		t.Fatal(err)
	}

	times := make(chan time.Time, requests)
	var wg sync.WaitGroup

	for i := 0; i < requests; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ctx, cancel := context.WithTimeout(
				context.Background(), 2*time.Second,
			)
			defer cancel()

			if err := limiter.Wait(ctx); err != nil {
				t.Errorf("rate admission: %v", err)
				return
			}
			times <- time.Now()
		}()
	}

	wg.Wait()
	close(times)

	var admitted []time.Time
	for at := range times {
		admitted = append(admitted, at)
	}

	if len(admitted) != requests {
		t.Fatalf("admitted %d, want %d", len(admitted), requests)
	}

	// The channel receives timestamps in admission order.
	for i := 1; i < len(admitted); i++ {
		gap := admitted[i].Sub(admitted[i-1])
		if gap < 40*time.Millisecond {
			t.Fatalf("admissions too close: %v", gap)
		}
	}

	got := limiter.Snapshot()
	if got.RateAdmitted != requests || got.RateWaiting != 0 {
		t.Fatalf("unexpected metrics: %+v", got)
	}
}

func TestRateLimiterCancellationDoesNotReserveSlot(t *testing.T) {
	limiter, err := NewRateLimiter(2)
	if err != nil {
		t.Fatal(err)
	}

	if err := limiter.Wait(context.Background()); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if err := limiter.Wait(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("expected cancellation, got %v", err)
	}

	got := limiter.Snapshot()
	if got.RateAdmitted != 1 {
		t.Fatalf("canceled request consumed a slot: %+v", got)
	}
}

func TestRateLimiterDeadline(t *testing.T) {
	limiter, err := NewRateLimiter(1)
	if err != nil {
		t.Fatal(err)
	}

	if err := limiter.Wait(context.Background()); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(
		context.Background(), 20*time.Millisecond,
	)
	defer cancel()

	err = limiter.Wait(ctx)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected deadline exceeded, got %v", err)
	}

	got := limiter.Snapshot()
	if got.RateAdmitted != 1 ||
		got.RateCanceled != 1 ||
		got.RateWaiting != 0 {
		t.Fatalf("unexpected metrics: %+v", got)
	}
}
