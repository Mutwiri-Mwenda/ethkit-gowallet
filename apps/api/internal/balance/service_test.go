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

type readerFunc func(context.Context, uint64, string) (*big.Int, error)

func (f readerFunc) GetBalance(
	ctx context.Context,
	chainID uint64,
	address string,
) (*big.Int, error) {
	return f(ctx, chainID, address)
}

const wallet = "0x0000000000000000000000000000000000000000"

func newService(t *testing.T, reader Reader, ttl time.Duration) *Service {
	t.Helper()
	s, err := New(reader, ttl, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func awaitWaiters(t *testing.T, s *Service, count int) {
	t.Helper()

	deadline := time.Now().Add(3 * time.Second)
	k := key{chainID: 11155111, address: wallet}

	for time.Now().Before(deadline) {
		s.mu.Lock()
		f := s.inflight[k]
		n := 0
		if f != nil {
			n = f.waiters
		}
		s.mu.Unlock()

		if n == count {
			return
		}
		time.Sleep(time.Millisecond)
	}

	t.Fatalf("expected %d in-flight waiters", count)
}

func TestConcurrentRequestsShareOneFetch(t *testing.T) {
	const callers = 100

	var calls atomic.Int64
	started := make(chan struct{})
	release := make(chan struct{})

	s := newService(t, readerFunc(func(
		ctx context.Context, chainID uint64, address string,
	) (*big.Int, error) {
		if calls.Add(1) == 1 {
			close(started)
		}
		select {
		case <-release:
			return big.NewInt(42), nil
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}), time.Second)

	var wg sync.WaitGroup
	errs := make(chan error, callers)

	for i := 0; i < callers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			amount, err := s.GetBalance(
				context.Background(), 11155111, wallet,
			)
			if err != nil {
				errs <- err
				return
			}
			if amount.String() != "42" {
				errs <- errors.New("incorrect balance")
			}
		}()
	}

	<-started
	awaitWaiters(t, s, callers)
	close(release)

	wg.Wait()
	close(errs)

	for err := range errs {
		t.Error(err)
	}
	if got := calls.Load(); got != 1 {
		t.Fatalf("expected 1 upstream call, got %d", got)
	}
}

func TestCacheHitAndExpiry(t *testing.T) {
	var calls atomic.Int64

	s := newService(t, readerFunc(func(
		ctx context.Context, chainID uint64, address string,
	) (*big.Int, error) {
		return big.NewInt(calls.Add(1)), nil
	}), 20*time.Millisecond)

	first, err := s.GetBalance(context.Background(), 11155111, wallet)
	if err != nil {
		t.Fatal(err)
	}
	second, err := s.GetBalance(context.Background(), 11155111, wallet)
	if err != nil {
		t.Fatal(err)
	}

	if first.String() != "1" || second.String() != "1" ||
		calls.Load() != 1 {
		t.Fatal("cache hit failed")
	}

	time.Sleep(30 * time.Millisecond)

	third, err := s.GetBalance(context.Background(), 11155111, wallet)
	if err != nil {
		t.Fatal(err)
	}
	if third.String() != "2" || calls.Load() != 2 {
		t.Fatal("expired entry was reused")
	}
}

func TestCacheIsolationAndCopies(t *testing.T) {
	var calls atomic.Int64

	s := newService(t, readerFunc(func(
		ctx context.Context, chainID uint64, address string,
	) (*big.Int, error) {
		calls.Add(1)
		return big.NewInt(100), nil
	}), time.Second)

	a, err := s.GetBalance(context.Background(), 11155111, wallet)
	if err != nil {
		t.Fatal(err)
	}
	a.SetInt64(999)

	b, err := s.GetBalance(context.Background(), 11155111, wallet)
	if err != nil {
		t.Fatal(err)
	}
	if b.String() != "100" {
		t.Fatal("caller mutated cached balance")
	}

	_, err = s.GetBalance(context.Background(), 1, wallet)
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.GetBalance(
		context.Background(), 11155111,
		"0x1111111111111111111111111111111111111111",
	)
	if err != nil {
		t.Fatal(err)
	}

	if calls.Load() != 3 {
		t.Fatalf("expected 3 independent fetches, got %d", calls.Load())
	}
}

func TestErrorsAreNotCached(t *testing.T) {
	var calls atomic.Int64
	upstreamErr := errors.New("temporary failure")

	s := newService(t, readerFunc(func(
		ctx context.Context, chainID uint64, address string,
	) (*big.Int, error) {
		if calls.Add(1) == 1 {
			return nil, upstreamErr
		}
		return big.NewInt(7), nil
	}), time.Second)

	_, err := s.GetBalance(context.Background(), 11155111, wallet)
	if !errors.Is(err, upstreamErr) {
		t.Fatalf("unexpected error: %v", err)
	}

	amount, err := s.GetBalance(context.Background(), 11155111, wallet)
	if err != nil || amount.String() != "7" {
		t.Fatalf("retry failed: %v", err)
	}

	if calls.Load() != 2 {
		t.Fatal("failure was cached")
	}
}

func TestOneCancellationDoesNotCancelOthers(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	upstreamCanceled := make(chan struct{})

	s := newService(t, readerFunc(func(
		ctx context.Context, chainID uint64, address string,
	) (*big.Int, error) {
		close(started)
		select {
		case <-release:
			return big.NewInt(55), nil
		case <-ctx.Done():
			close(upstreamCanceled)
			return nil, ctx.Err()
		}
	}), time.Second)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	firstResult := make(chan error, 1)
	go func() {
		_, err := s.GetBalance(ctx, 11155111, wallet)
		firstResult <- err
	}()

	<-started

	secondResult := make(chan error, 1)
	go func() {
		amount, err := s.GetBalance(
			context.Background(), 11155111, wallet,
		)
		if err == nil && amount.String() != "55" {
			err = errors.New("wrong balance")
		}
		secondResult <- err
	}()

	awaitWaiters(t, s, 2)
	cancel()

	if err := <-firstResult; !errors.Is(err, context.Canceled) {
		t.Fatalf("expected cancellation, got %v", err)
	}

	select {
	case <-upstreamCanceled:
		t.Fatal("one caller canceled the shared upstream request")
	default:
	}

	close(release)
	if err := <-secondResult; err != nil {
		t.Fatal(err)
	}
}

func TestAllCallersCancelUpstream(t *testing.T) {
	started := make(chan struct{})
	upstreamCanceled := make(chan struct{})

	s := newService(t, readerFunc(func(
		ctx context.Context, chainID uint64, address string,
	) (*big.Int, error) {
		close(started)
		<-ctx.Done()
		close(upstreamCanceled)
		return nil, ctx.Err()
	}), time.Second)

	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan error, 1)

	go func() {
		_, err := s.GetBalance(ctx, 11155111, wallet)
		result <- err
	}()

	<-started
	cancel()
	defer cancel()

	if err := <-result; !errors.Is(err, context.Canceled) {
		t.Fatalf("expected cancellation, got %v", err)
	}

	select {
	case <-upstreamCanceled:
	case <-time.After(time.Second):
		t.Fatal("upstream request was not canceled")
	}
}

func TestInvalidConfiguration(t *testing.T) {
	_, err := New(nil, time.Second, time.Second)
	if !errors.Is(err, ErrInvalidConfiguration) {
		t.Fatalf("expected configuration error, got %v", err)
	}
}
