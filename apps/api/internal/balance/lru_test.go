package balance

import (
	"context"
	"math/big"
	"sync"
	"testing"
	"time"
)

type lruReader struct {
	mu    sync.Mutex
	calls map[string]int
}

func (r *lruReader) GetBalance(
	ctx context.Context,
	chainID uint64,
	address string,
) (*big.Int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.calls == nil {
		r.calls = make(map[string]int)
	}
	r.calls[address]++

	return big.NewInt(1000), nil
}

func (r *lruReader) count(address string) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.calls[address]
}

func TestLRUEvictsLeastRecentlyUsed(t *testing.T) {
	reader := &lruReader{}
	s, err := NewWithCapacity(reader, time.Minute, time.Second, 2)
	if err != nil {
		t.Fatal(err)
	}

	ctx := context.Background()

	for _, address := range []string{"A", "B", "A", "C", "A", "B"} {
		if _, err := s.GetBalance(ctx, 1, address); err != nil {
			t.Fatal(err)
		}
	}

	// A was refreshed before C was inserted, so B was evicted.
	// The final B request must therefore reach the reader again.
	if got := reader.count("A"); got != 1 {
		t.Fatalf("A fetched %d times, want 1", got)
	}
	if got := reader.count("B"); got != 2 {
		t.Fatalf("B fetched %d times, want 2", got)
	}

	m := s.Metrics()
	if m.CacheEntries != 2 || m.CacheCapacity != 2 {
		t.Fatalf("unexpected cache size: %+v", m)
	}
	if m.CacheEvictions != 2 {
		t.Fatalf("expected 2 evictions, got %d", m.CacheEvictions)
	}
}

func TestExpiredEntriesRemovedBeforeCapacityEviction(t *testing.T) {
	reader := &lruReader{}
	s, err := NewWithCapacity(
		reader,
		20*time.Millisecond,
		time.Second,
		2,
	)
	if err != nil {
		t.Fatal(err)
	}

	ctx := context.Background()

	for _, address := range []string{"A", "B"} {
		if _, err := s.GetBalance(ctx, 1, address); err != nil {
			t.Fatal(err)
		}
	}

	time.Sleep(40 * time.Millisecond)

	if _, err := s.GetBalance(ctx, 1, "C"); err != nil {
		t.Fatal(err)
	}

	m := s.Metrics()
	if m.CacheEntries != 1 {
		t.Fatalf("expected 1 entry, got %d", m.CacheEntries)
	}
	if m.CacheEvictions != 0 {
		t.Fatalf("expired entries counted as evictions: %+v", m)
	}
}

func TestCacheCapacityUnderConcurrency(t *testing.T) {
	reader := &lruReader{}
	const capacity = 10
	const callers = 200

	s, err := NewWithProviderLimits(
		reader,
		time.Minute,
		5*time.Second,
		capacity,
		callers, // Allow all 200 distinct requests to run concurrently.
		callers, // No overload rejection during this cache-specific test.
		100000,  // High admission rate to minimize pacing effects.
	)
	if err != nil {
		t.Fatal(err)
	}

	var wg sync.WaitGroup
	errs := make(chan error, callers)

	for i := 0; i < callers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()

			address := big.NewInt(int64(i)).String()
			_, err := s.GetBalance(
				context.Background(), 1, address,
			)
			errs <- err
		}(i)
	}

	wg.Wait()
	close(errs)

	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}

	m := s.Metrics()
	if m.CacheEntries > capacity {
		t.Fatalf("capacity exceeded: %+v", m)
	}
	if m.CacheEntries != capacity {
		t.Fatalf("expected %d entries, got %d",
			capacity, m.CacheEntries)
	}
	if m.CacheEvictions != callers-capacity {
		t.Fatalf("unexpected eviction count: %+v", m)
	}
}

func TestInvalidCacheCapacity(t *testing.T) {
	_, err := NewWithCapacity(
		&lruReader{}, time.Second, time.Second, 0,
	)
	if err == nil {
		t.Fatal("expected invalid capacity error")
	}
}

func TestCacheReturnsDefensiveCopies(t *testing.T) {
	s, err := NewWithCapacity(
		&lruReader{}, time.Minute, time.Second, 2,
	)
	if err != nil {
		t.Fatal(err)
	}

	ctx := context.Background()
	first, err := s.GetBalance(ctx, 1, "A")
	if err != nil {
		t.Fatal(err)
	}

	first.SetInt64(999999)

	second, err := s.GetBalance(ctx, 1, "A")
	if err != nil {
		t.Fatal(err)
	}

	if second.Int64() != 1000 {
		t.Fatalf("cached balance was mutated: %s", second)
	}
}
