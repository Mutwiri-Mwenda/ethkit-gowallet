package balance

import (
	"context"
	"errors"
	"math/big"
	"sync"
	"testing"
	"time"

	"github.com/Mutwiri-Mwenda/gowallet/sdk/explorer/etherscan"
)

type circuitTestReader struct {
	mu    sync.Mutex
	calls int
	fail  bool
}

func (r *circuitTestReader) GetBalance(
	ctx context.Context,
	chainID uint64,
	address string,
) (*big.Int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.calls++

	if r.fail {
		return nil, etherscan.ErrRateLimited
	}

	return big.NewInt(1000), nil
}

func (r *circuitTestReader) setFail(fail bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.fail = fail
}

func (r *circuitTestReader) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.calls
}

func newCircuitTestService(
	t *testing.T,
	reader Reader,
	ttl time.Duration,
) *Service {
	t.Helper()

	s, err := NewWithProviderLimits(
		reader,
		ttl,
		5*time.Second,
		100,
		10,
		20,
		1000,
	)
	if err != nil {
		t.Fatal(err)
	}

	s.circuitBreaker, err = NewCircuitBreaker(
		1,
		20*time.Millisecond,
	)
	if err != nil {
		t.Fatal(err)
	}

	return s
}

func TestServiceOpenCircuitRejectsUncachedRequests(t *testing.T) {
	reader := &circuitTestReader{fail: true}
	s := newCircuitTestService(t, reader, time.Minute)

	_, err := s.GetBalance(context.Background(), 1, "0xAlice")
	if !errors.Is(err, etherscan.ErrRateLimited) {
		t.Fatalf("got %v", err)
	}

	if s.circuitBreaker.Snapshot().State != CircuitOpen {
		t.Fatal("circuit did not open")
	}

	before := reader.count()

	for i := 0; i < 20; i++ {
		_, err := s.GetBalance(
			context.Background(),
			1,
			"0xDifferent",
		)
		if !errors.Is(err, ErrCircuitOpen) {
			t.Fatalf("request %d: %v", i, err)
		}
	}

	if reader.count() != before {
		t.Fatal("open circuit contacted provider")
	}
}

func TestServiceOpenCircuitServesCachedBalance(t *testing.T) {
	reader := &circuitTestReader{}
	s := newCircuitTestService(t, reader, time.Minute)

	ctx := context.Background()

	amount, err := s.GetBalance(ctx, 1, "0xCached")
	if err != nil {
		t.Fatal(err)
	}

	reader.setFail(true)

	_, err = s.GetBalance(ctx, 1, "0xTrigger")
	if !errors.Is(err, etherscan.ErrRateLimited) {
		t.Fatalf("got %v", err)
	}

	if s.circuitBreaker.Snapshot().State != CircuitOpen {
		t.Fatal("circuit did not open")
	}

	before := reader.count()

	cached, err := s.GetBalance(ctx, 1, "0xCached")
	if err != nil {
		t.Fatal(err)
	}

	if cached.Cmp(amount) != 0 {
		t.Fatal("cached balance changed")
	}

	if reader.count() != before {
		t.Fatal("cache hit contacted provider")
	}
}

func TestServiceSuccessfulProbeRestoresTraffic(t *testing.T) {
	reader := &circuitTestReader{fail: true}
	s := newCircuitTestService(t, reader, time.Minute)

	ctx := context.Background()

	_, err := s.GetBalance(ctx, 1, "0xFailing")
	if !errors.Is(err, etherscan.ErrRateLimited) {
		t.Fatalf("got %v", err)
	}

	if s.circuitBreaker.Snapshot().State != CircuitOpen {
		t.Fatal("circuit did not open")
	}

	reader.setFail(false)
	time.Sleep(25 * time.Millisecond)

	_, err = s.GetBalance(ctx, 1, "0xRecovery")
	if err != nil {
		t.Fatalf("recovery probe failed: %v", err)
	}

	if s.circuitBreaker.Snapshot().State != CircuitClosed {
		t.Fatal("circuit did not close")
	}

	_, err = s.GetBalance(ctx, 1, "0xNormal")
	if err != nil {
		t.Fatalf("normal traffic failed: %v", err)
	}
}
