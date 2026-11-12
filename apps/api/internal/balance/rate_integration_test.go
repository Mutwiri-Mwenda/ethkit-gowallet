package balance

import (
	"context"
	"math/big"
	"sync"
	"testing"
	"time"
)

type rateTestReader struct {
	mu    sync.Mutex
	calls []time.Time
}

func (r *rateTestReader) GetBalance(
	ctx context.Context,
	chainID uint64,
	address string,
) (*big.Int, error) {
	r.mu.Lock()
	r.calls = append(r.calls, time.Now())
	r.mu.Unlock()

	return big.NewInt(100), nil
}

func TestServiceEnforcesUpstreamRate(t *testing.T) {
	reader := &rateTestReader{}

	service, err := NewWithProviderLimits(
		reader,
		time.Minute,
		3*time.Second,
		100,
		10,
		10,
		10, // 100 ms between upstream starts
	)
	if err != nil {
		t.Fatal(err)
	}

	addresses := []string{
		"0x0000000000000000000000000000000000000001",
		"0x0000000000000000000000000000000000000002",
		"0x0000000000000000000000000000000000000003",
	}

	var wg sync.WaitGroup

	for _, address := range addresses {
		wg.Add(1)
		go func(addr string) {
			defer wg.Done()

			_, err := service.GetBalance(
				context.Background(), 11155111, addr,
			)
			if err != nil {
				t.Errorf("GetBalance: %v", err)
			}
		}(address)
	}

	wg.Wait()

	reader.mu.Lock()
	defer reader.mu.Unlock()

	if len(reader.calls) != 3 {
		t.Fatalf("upstream calls = %d, want 3", len(reader.calls))
	}

	for i := 1; i < len(reader.calls); i++ {
		gap := reader.calls[i].Sub(reader.calls[i-1])
		if gap < 80*time.Millisecond {
			t.Fatalf("upstream requests too close: %v", gap)
		}
	}

	metrics := service.Metrics()
	if metrics.RateAdmitted != 3 {
		t.Fatalf("rate admissions = %d, want 3", metrics.RateAdmitted)
	}
}

func TestCacheHitDoesNotConsumeRateSlot(t *testing.T) {
	reader := &rateTestReader{}

	service, err := NewWithProviderLimits(
		reader,
		time.Minute,
		3*time.Second,
		100,
		10,
		10,
		10,
	)
	if err != nil {
		t.Fatal(err)
	}

	address := "0x0000000000000000000000000000000000000001"

	for i := 0; i < 2; i++ {
		if _, err := service.GetBalance(
			context.Background(), 11155111, address,
		); err != nil {
			t.Fatal(err)
		}
	}

	metrics := service.Metrics()

	if metrics.RateAdmitted != 1 {
		t.Fatalf("rate admissions = %d, want 1", metrics.RateAdmitted)
	}

	if metrics.CacheHits != 1 {
		t.Fatalf("cache hits = %d, want 1", metrics.CacheHits)
	}
}
