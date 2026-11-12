package balance

import (
	"context"
	"errors"
	"math/big"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// blockingRateReader keeps the first upstream call running
// so concurrent callers can join the same flight.
type blockingRateReader struct {
	calls   atomic.Int64
	started chan struct{}
	release chan struct{}
}

func (r *blockingRateReader) GetBalance(
	ctx context.Context,
	chainID uint64,
	address string,
) (*big.Int, error) {
	if r.calls.Add(1) == 1 {
		close(r.started)
	}

	select {
	case <-r.release:
		return big.NewInt(100), nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func TestCoalescedRequestsConsumeOneRateAdmission(t *testing.T) {
	const callers = 100

	reader := &blockingRateReader{
		started: make(chan struct{}),
		release: make(chan struct{}),
	}

	service, err := NewWithProviderLimits(
		reader,
		time.Minute,
		5*time.Second,
		100,
		10,
		20,
		10,
	)
	if err != nil {
		t.Fatal(err)
	}

	address := "0x0000000000000000000000000000000000000001"

	start := make(chan struct{})
	var wg sync.WaitGroup

	for i := 0; i < callers; i++ {
		wg.Add(1)

		go func() {
			defer wg.Done()

			<-start

			ctx, cancel := context.WithTimeout(
				context.Background(), 4*time.Second,
			)
			defer cancel()

			amount, err := service.GetBalance(
				ctx, 11155111, address,
			)
			if err != nil {
				t.Errorf("GetBalance: %v", err)
				return
			}

			if amount.Cmp(big.NewInt(100)) != 0 {
				t.Errorf("unexpected balance: %s", amount)
			}
		}()
	}

	close(start)

	select {
	case <-reader.started:
	case <-time.After(2 * time.Second):
		t.Fatal("upstream request never started")
	}

	// Give concurrent callers time to join the flight.
	time.Sleep(50 * time.Millisecond)

	close(reader.release)
	wg.Wait()

	if got := reader.calls.Load(); got != 1 {
		t.Fatalf("upstream calls = %d, want 1", got)
	}

	metrics := service.Metrics()
	if metrics.RateAdmitted != 1 {
		t.Fatalf(
			"rate admissions = %d, want 1",
			metrics.RateAdmitted,
		)
	}
}

type countingRateReader struct {
	calls atomic.Int64
}

func (r *countingRateReader) GetBalance(
	ctx context.Context,
	chainID uint64,
	address string,
) (*big.Int, error) {
	r.calls.Add(1)
	return big.NewInt(100), nil
}

func TestCanceledRateWaitNeverReachesProvider(t *testing.T) {
	reader := &countingRateReader{}

	service, err := NewWithProviderLimits(
		reader,
		time.Minute,
		3*time.Second,
		100,
		10,
		20,
		1, // One admission per second.
	)
	if err != nil {
		t.Fatal(err)
	}

	first := "0x0000000000000000000000000000000000000001"
	second := "0x0000000000000000000000000000000000000002"

	// Consume the immediate admission slot.
	if _, err := service.GetBalance(
		context.Background(), 11155111, first,
	); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(
		context.Background(), 30*time.Millisecond,
	)
	defer cancel()

	_, err = service.GetBalance(ctx, 11155111, second)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected deadline exceeded, got %v", err)
	}

	// Wait for the abandoned flight to finish cleanup.
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if service.Metrics().RateCanceled >= 1 {
			break
		}
		time.Sleep(time.Millisecond)
	}

	if got := reader.calls.Load(); got != 1 {
		t.Fatalf("provider calls = %d, want 1", got)
	}

	metrics := service.Metrics()

	if metrics.RateAdmitted != 1 {
		t.Fatalf(
			"rate admissions = %d, want 1",
			metrics.RateAdmitted,
		)
	}

	if metrics.RateCanceled != 1 {
		t.Fatalf(
			"rate cancellations = %d, want 1",
			metrics.RateCanceled,
		)
	}
}
