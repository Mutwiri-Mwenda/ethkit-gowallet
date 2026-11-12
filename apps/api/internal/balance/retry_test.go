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

type retryReader struct {
	mu       sync.Mutex
	attempts int
	failures int
	err      error
}


type blockingRetryReader struct {
    mu       sync.Mutex
    attempts int
    started  chan struct{}
}

func (r *blockingRetryReader) GetBalance(
    ctx context.Context,
    chainID uint64,
    address string,
) (*big.Int, error) {
    r.mu.Lock()
    r.attempts++
    r.mu.Unlock()

    close(r.started)

    <-ctx.Done()
    return nil, ctx.Err()
}

func (r *blockingRetryReader) count() int {
    r.mu.Lock()
    defer r.mu.Unlock()
    return r.attempts
}


func (r *retryReader) GetBalance(
	ctx context.Context,
	chainID uint64,
	address string,
) (*big.Int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.attempts++

	if r.attempts <= r.failures {
		return nil, r.err
	}

	return big.NewInt(100), nil
}

func (r *retryReader) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.attempts
}

func TestRetrySucceedsAfterRateLimit(t *testing.T) {
	reader := &retryReader{
		failures: 2,
		err:      etherscan.ErrRateLimited,
	}

	service, err := NewWithProviderLimits(
		reader,
		time.Minute,
		5*time.Second,
		100,
		10,
		20,
		100,
	)
	if err != nil {
		t.Fatal(err)
	}

	address := "0x0000000000000000000000000000000000000001"

	amount, err := service.GetBalance(
		context.Background(),
		11155111,
		address,
	)
	if err != nil {
		t.Fatal(err)
	}

	if amount.Cmp(big.NewInt(100)) != 0 {
		t.Fatalf("unexpected balance: %s", amount)
	}

	if got := reader.count(); got != 3 {
		t.Fatalf("attempts = %d, want 3", got)
	}

	metrics := service.Metrics()

	if metrics.RateAdmitted != 3 {
		t.Fatalf("rate admissions = %d, want 3", metrics.RateAdmitted)
	}

	if metrics.UpstreamErrors != 2 {
		t.Fatalf("upstream errors = %d, want 2", metrics.UpstreamErrors)
	}
}

func TestRetryStopsAfterMaximumAttempts(t *testing.T) {
	reader := &retryReader{
		failures: 10,
		err:      etherscan.ErrRateLimited,
	}

	service, err := NewWithProviderLimits(
		reader,
		time.Minute,
		5*time.Second,
		100,
		10,
		20,
		100,
	)
	if err != nil {
		t.Fatal(err)
	}

	address := "0x0000000000000000000000000000000000000002"

	_, err = service.GetBalance(
		context.Background(),
		11155111,
		address,
	)

	if !errors.Is(err, etherscan.ErrRateLimited) {
		t.Fatalf("expected rate limit error, got %v", err)
	}

	if got := reader.count(); got != 3 {
		t.Fatalf("attempts = %d, want 3", got)
	}
}

func TestPermanentErrorIsNotRetried(t *testing.T) {
	reader := &retryReader{
		failures: 10,
		err:      etherscan.ErrInvalidAddress,
	}

	service, err := NewWithProviderLimits(
		reader,
		time.Minute,
		5*time.Second,
		100,
		10,
		20,
		100,
	)
	if err != nil {
		t.Fatal(err)
	}

	address := "0x0000000000000000000000000000000000000003"

	_, err = service.GetBalance(
		context.Background(),
		11155111,
		address,
	)

	if !errors.Is(err, etherscan.ErrInvalidAddress) {
		t.Fatalf("expected invalid address, got %v", err)
	}

	if got := reader.count(); got != 1 {
		t.Fatalf("attempts = %d, want 1", got)
	}
}


func TestRetryStopsWhenContextExpires(t *testing.T) {
    started := make(chan struct{})

    reader := &blockingRetryReader{
        started: started,
    }

    service, err := NewWithProviderLimits(
        reader,
        time.Minute,
        100*time.Millisecond,
        100,
        10,
        20,
        100,
    )
    if err != nil {
        t.Fatal(err)
    }

    address := "0x0000000000000000000000000000000000000004"

    _, err = service.GetBalance(
        context.Background(),
        11155111,
        address,
    )

    if !errors.Is(err, context.DeadlineExceeded) {
        t.Fatalf("expected deadline exceeded, got %v", err)
    }

    select {
    case <-started:
        // The provider was called before the deadline.
    default:
        t.Fatal("expected at least one upstream attempt")
    }

    if got := reader.count(); got != 1 {
        t.Fatalf("attempts = %d, want 1", got)
    }
}
