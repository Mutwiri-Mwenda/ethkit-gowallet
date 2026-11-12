package balance

import (
	"context"
	"errors"
	"math/big"
	"sync"
	"testing"
	"time"
)

type metricsReader struct {
	mu      sync.Mutex
	calls   int
	started chan struct{}
	release chan struct{}
	err     error
}

func (r *metricsReader) GetBalance(
	ctx context.Context,
	chainID uint64,
	address string,
) (*big.Int, error) {
	r.mu.Lock()
	r.calls++
	r.mu.Unlock()

	if r.started != nil {
		select {
		case r.started <- struct{}{}:
		default:
		}
	}

	if r.release != nil {
		select {
		case <-r.release:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}

	if r.err != nil {
		return nil, r.err
	}
	return big.NewInt(1000), nil
}

func newMetricsService(t *testing.T, r Reader) *Service {
	t.Helper()

	s, err := New(r, time.Minute, time.Second*5)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestMetricsCacheHitAndMiss(t *testing.T) {
	reader := &metricsReader{}
	s := newMetricsService(t, reader)
	ctx := context.Background()

	for i := 0; i < 3; i++ {
		if _, err := s.GetBalance(ctx, 1, "0xabc"); err != nil {
			t.Fatal(err)
		}
	}

	got := s.Metrics()
	if got.CacheHits != 2 ||
		got.CacheMisses != 1 ||
		got.CoalescedRequests != 0 ||
		got.UpstreamRequests != 1 ||
		got.UpstreamErrors != 0 {
		t.Fatalf("unexpected metrics: %+v", got)
	}
}

func TestMetricsCoalescing(t *testing.T) {
	reader := &metricsReader{
		started: make(chan struct{}, 1),
		release: make(chan struct{}),
	}
	s := newMetricsService(t, reader)

	const callers = 100
	var wg sync.WaitGroup
	errs := make(chan error, callers)

	// Start one upstream request and hold it open.
	wg.Add(1)
	go func() {
		defer wg.Done()
		_, err := s.GetBalance(context.Background(), 1, "0xabc")
		errs <- err
	}()

	select {
	case <-reader.started:
	case <-time.After(3 * time.Second):
		t.Fatal("upstream request did not start")
	}

	// All other callers should join the existing flight.
	for i := 1; i < callers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := s.GetBalance(context.Background(), 1, "0xabc")
			errs <- err
		}()
	}

	deadline := time.After(3 * time.Second)
	for {
		if s.Metrics().CoalescedRequests == callers-1 {
			break
		}
		select {
		case <-deadline:
			t.Fatal("callers did not join the flight")
		default:
			time.Sleep(time.Millisecond)
		}
	}

	close(reader.release)
	wg.Wait()
	close(errs)

	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}

	got := s.Metrics()
	if got.CacheMisses != callers ||
		got.CoalescedRequests != callers-1 ||
		got.UpstreamRequests != 1 ||
		got.UpstreamErrors != 0 {
		t.Fatalf("unexpected metrics: %+v", got)
	}

	reader.mu.Lock()
	calls := reader.calls
	reader.mu.Unlock()

	if calls != 1 {
		t.Fatalf("expected one upstream call, got %d", calls)
	}
}

func TestMetricsUpstreamErrors(t *testing.T) {
	reader := &metricsReader{
		err: errors.New("provider unavailable"),
	}
	s := newMetricsService(t, reader)

	for i := 0; i < 2; i++ {
		_, err := s.GetBalance(context.Background(), 1, "0xabc")
		if err == nil {
			t.Fatal("expected upstream error")
		}
	}

	got := s.Metrics()
	if got.CacheHits != 0 ||
		got.CacheMisses != 2 ||
		got.UpstreamRequests != 2 ||
		got.UpstreamErrors != 2 {
		t.Fatalf("unexpected metrics: %+v", got)
	}
}

func TestMetricsInvalidRequestsNotCounted(t *testing.T) {
	s := newMetricsService(t, &metricsReader{})

	_, err := s.GetBalance(context.Background(), 0, "0xabc")
	if !errors.Is(err, ErrInvalidConfiguration) {
		t.Fatalf("unexpected error: %v", err)
	}

	expected := MetricsSnapshot{
		CacheCapacity: DefaultCacheCapacity,
		RateSnapshot: RateSnapshot{
			RatePerSec: DefaultRatePerSecond,
		},
		CircuitSnapshot: CircuitSnapshot{
			State: CircuitClosed,
		},
	}

	if got := s.Metrics(); got != expected {
		t.Fatalf("unexpected metrics: got %+v, want %+v",
			got, expected)
	}
}
