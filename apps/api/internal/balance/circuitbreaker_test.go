package balance

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/Mutwiri-Mwenda/gowallet/sdk/explorer/etherscan"
)

func TestCircuitBreakerOpensAfterThreshold(t *testing.T) {
	b, err := NewCircuitBreaker(3, time.Second)
	if err != nil {
		t.Fatal(err)
	}

	for i := 0; i < 3; i++ {
		err := b.Call(context.Background(), func(context.Context) error {
			return etherscan.ErrRateLimited
		})
		if !errors.Is(err, etherscan.ErrRateLimited) {
			t.Fatal(err)
		}
	}

	if b.Snapshot().State != CircuitOpen {
		t.Fatal("expected open circuit")
	}

	err = b.Call(context.Background(), func(context.Context) error {
		t.Fatal("upstream must not be called")
		return nil
	})
	if !errors.Is(err, ErrCircuitOpen) {
		t.Fatalf("got %v", err)
	}
}

func TestCircuitBreakerSuccessfulProbeCloses(t *testing.T) {
	b, err := NewCircuitBreaker(1, 10*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}

	_ = b.Call(context.Background(), func(context.Context) error {
		return etherscan.ErrRateLimited
	})

	time.Sleep(15 * time.Millisecond)

	if err := b.Call(context.Background(), func(context.Context) error {
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	if b.Snapshot().State != CircuitClosed {
		t.Fatal("successful probe must close circuit")
	}
}

func TestCircuitBreakerAllowsOneHalfOpenProbe(t *testing.T) {
	b, err := NewCircuitBreaker(1, 10*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}

	_ = b.Call(context.Background(), func(context.Context) error {
		return etherscan.ErrRateLimited
	})

	time.Sleep(15 * time.Millisecond)

	started := make(chan struct{})
	release := make(chan struct{})
	done := make(chan error, 1)

	go func() {
		done <- b.Call(context.Background(), func(context.Context) error {
			close(started)
			<-release
			return nil
		})
	}()

	<-started

	err = b.Call(context.Background(), func(context.Context) error {
		t.Fatal("second probe must not run")
		return nil
	})
	if !errors.Is(err, ErrCircuitOpen) {
		t.Fatalf("got %v", err)
	}

	close(release)

	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func TestCircuitBreakerIgnoresPermanentErrors(t *testing.T) {
	b, _ := NewCircuitBreaker(1, time.Second)

	err := b.Call(context.Background(), func(context.Context) error {
		return etherscan.ErrInvalidAddress
	})

	if !errors.Is(err, etherscan.ErrInvalidAddress) {
		t.Fatal(err)
	}
	if b.Snapshot().State != CircuitClosed {
		t.Fatal("permanent error opened circuit")
	}
}

func TestCircuitBreakerCanceledProbeCanRetry(t *testing.T) {
	b, _ := NewCircuitBreaker(1, time.Millisecond)

	_ = b.Call(context.Background(), func(context.Context) error {
		return etherscan.ErrRateLimited
	})

	time.Sleep(2 * time.Millisecond)

	ctx, cancel := context.WithCancel(context.Background())
	err := b.Call(ctx, func(context.Context) error {
		cancel()
		return ctx.Err()
	})

	if !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}

	if b.Snapshot().State != CircuitOpen {
		t.Fatal("canceled probe should return circuit to open")
	}

	err = b.Call(context.Background(), func(context.Context) error {
		return nil
	})
	if err != nil {
		t.Fatalf("replacement probe failed: %v", err)
	}
}

func TestCircuitBreakerStaleSuccessDoesNotResetFailures(t *testing.T) {
	b, _ := NewCircuitBreaker(2, time.Second)

	started := make(chan struct{})
	release := make(chan struct{})
	var wg sync.WaitGroup

	wg.Add(1)
	go func() {
		defer wg.Done()
		_ = b.Call(context.Background(), func(context.Context) error {
			close(started)
			<-release
			return nil
		})
	}()

	<-started

	_ = b.Call(context.Background(), func(context.Context) error {
		return etherscan.ErrRateLimited
	})

	close(release)
	wg.Wait()

	if b.Snapshot().Failures != 1 {
		t.Fatal("stale success reset newer failure")
	}
}

func TestCircuitBreakerInvalidConfiguration(t *testing.T) {
	if _, err := NewCircuitBreaker(0, time.Second); err == nil {
		t.Fatal("expected invalid threshold error")
	}
	if _, err := NewCircuitBreaker(1, 0); err == nil {
		t.Fatal("expected invalid cooldown error")
	}
}
