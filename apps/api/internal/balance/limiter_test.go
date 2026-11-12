package balance

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

func TestLimiterRejectsWhenFull(t *testing.T) {
	limiter, err := NewLimiter(1, 0)
	if err != nil {
		t.Fatal(err)
	}

	if err := limiter.Acquire(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer limiter.Release()

	if err := limiter.Acquire(context.Background()); !errors.Is(err, ErrOverloaded) {
		t.Fatalf("expected overload, got %v", err)
	}

	got := limiter.Snapshot()
	if got.Active != 1 || got.Waiting != 0 || got.Rejected != 1 {
		t.Fatalf("unexpected metrics: %+v", got)
	}
}

func TestLimiterWaitingAndRelease(t *testing.T) {
	limiter, err := NewLimiter(1, 1)
	if err != nil {
		t.Fatal(err)
	}

	if err := limiter.Acquire(context.Background()); err != nil {
		t.Fatal(err)
	}

	done := make(chan error, 1)
	go func() {
		err := limiter.Acquire(context.Background())
		if err == nil {
			limiter.Release()
		}
		done <- err
	}()

	deadline := time.After(3 * time.Second)
	for limiter.Snapshot().Waiting != 1 {
		select {
		case <-deadline:
			t.Fatal("request did not enter waiting queue")
		default:
			time.Sleep(time.Millisecond)
		}
	}

	limiter.Release()

	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("waiting request never acquired capacity")
	}

	got := limiter.Snapshot()
	if got.Active != 0 || got.Waiting != 0 {
		t.Fatalf("capacity leaked: %+v", got)
	}
}

func TestLimiterCancellation(t *testing.T) {
	limiter, err := NewLimiter(1, 1)
	if err != nil {
		t.Fatal(err)
	}

	if err := limiter.Acquire(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer limiter.Release()

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)

	go func() {
		done <- limiter.Acquire(ctx)
	}()

	deadline := time.After(3 * time.Second)
	for limiter.Snapshot().Waiting != 1 {
		select {
		case <-deadline:
			t.Fatal("request did not enter queue")
		default:
			time.Sleep(time.Millisecond)
		}
	}

	cancel()

	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("expected cancellation, got %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("canceled request remained queued")
	}

	if got := limiter.Snapshot().Waiting; got != 0 {
		t.Fatalf("waiting capacity leaked: %d", got)
	}
}

func TestLimiterConcurrentBound(t *testing.T) {
	const maxConcurrent = 3
	const callers = 100

	limiter, err := NewLimiter(maxConcurrent, callers)
	if err != nil {
		t.Fatal(err)
	}

	var wg sync.WaitGroup
	var mu sync.Mutex
	active := 0
	maxObserved := 0

	for i := 0; i < callers; i++ {
		wg.Add(1)

		go func() {
			defer wg.Done()

			ctx, cancel := context.WithTimeout(
				context.Background(), 5*time.Second,
			)
			defer cancel()

			if err := limiter.Acquire(ctx); err != nil {
				t.Errorf("acquire: %v", err)
				return
			}

			mu.Lock()
			active++
			if active > maxObserved {
				maxObserved = active
			}
			mu.Unlock()

			time.Sleep(time.Millisecond)

			mu.Lock()
			active--
			mu.Unlock()

			limiter.Release()
		}()
	}

	wg.Wait()

	if maxObserved > maxConcurrent {
		t.Fatalf("observed %d active, limit %d",
			maxObserved, maxConcurrent)
	}

	got := limiter.Snapshot()
	if got.Active != 0 || got.Waiting != 0 {
		t.Fatalf("limiter capacity leaked: %+v", got)
	}
}

func TestInvalidLimiterConfiguration(t *testing.T) {
	for _, config := range [][2]int{
		{0, 1},
		{-1, 1},
		{1, -1},
	} {
		_, err := NewLimiter(config[0], config[1])
		if !errors.Is(err, ErrInvalidConfiguration) {
			t.Fatalf("configuration %v: got %v", config, err)
		}
	}
}
