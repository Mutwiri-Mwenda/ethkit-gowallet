package balance

import (
	"container/list"
	"context"
	"errors"
	"math/big"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

var ErrInvalidConfiguration = errors.New("invalid balance service configuration")

const (
	DefaultCacheCapacity    = 1000
	DefaultMaxConcurrent    = 10
	DefaultMaxWaiting       = 20
	DefaultRatePerSecond    = 5
	DefaultCircuitThreshold = 3
	DefaultCircuitCooldown  = 10 * time.Second
)

type Reader interface {
	GetBalance(context.Context, uint64, string) (*big.Int, error)
}

type key struct {
	chainID uint64
	address string
}

type entry struct {
	key       key
	amount    *big.Int
	expiresAt time.Time
}

type flight struct {
	done    chan struct{}
	cancel  context.CancelFunc
	waiters int
	amount  *big.Int
	err     error
}

type MetricsSnapshot struct {
	CacheHits         uint64 `json:"cache_hits_total"`
	CacheMisses       uint64 `json:"cache_misses_total"`
	CoalescedRequests uint64 `json:"coalesced_requests_total"`
	UpstreamRequests  uint64 `json:"upstream_requests_total"`
	UpstreamErrors    uint64 `json:"upstream_errors_total"`
	CacheEvictions    uint64 `json:"cache_evictions_total"`
	CacheEntries      int    `json:"cache_entries"`
	CacheCapacity     int    `json:"cache_capacity"`

	LimiterSnapshot
	RateSnapshot
	CircuitSnapshot
}

type counters struct {
	cacheHits         atomic.Uint64
	cacheMisses       atomic.Uint64
	coalescedRequests atomic.Uint64
	upstreamRequests  atomic.Uint64
	upstreamErrors    atomic.Uint64
	cacheEvictions    atomic.Uint64
}

type Service struct {
	reader         Reader
	ttl            time.Duration
	timeout        time.Duration
	capacity       int
	limiter        *Limiter
	rateLimiter    *RateLimiter
	circuitBreaker *CircuitBreaker

	mu       sync.Mutex
	cache    map[key]*list.Element
	lru      *list.List
	inflight map[key]*flight

	counters counters
}

func New(
	reader Reader,
	ttl, timeout time.Duration,
) (*Service, error) {
	return NewWithProviderLimits(
		reader,
		ttl,
		timeout,
		DefaultCacheCapacity,
		DefaultMaxConcurrent,
		DefaultMaxWaiting,
		DefaultRatePerSecond,
	)
}

func NewWithCapacity(
	reader Reader,
	ttl, timeout time.Duration,
	capacity int,
) (*Service, error) {
	return NewWithProviderLimits(
		reader,
		ttl,
		timeout,
		capacity,
		DefaultMaxConcurrent,
		DefaultMaxWaiting,
		DefaultRatePerSecond,
	)
}

func NewWithLimits(
	reader Reader,
	ttl, timeout time.Duration,
	capacity, maxConcurrent, maxWaiting int,
) (*Service, error) {
	return NewWithProviderLimits(
		reader,
		ttl,
		timeout,
		capacity,
		maxConcurrent,
		maxWaiting,
		DefaultRatePerSecond,
	)
}

func NewWithProviderLimits(
	reader Reader,
	ttl, timeout time.Duration,
	capacity, maxConcurrent, maxWaiting, requestsPerSecond int,
) (*Service, error) {
	if reader == nil ||
		ttl <= 0 ||
		timeout <= 0 ||
		capacity <= 0 {
		return nil, ErrInvalidConfiguration
	}

	limiter, err := NewLimiter(maxConcurrent, maxWaiting)
	if err != nil {
		return nil, err
	}

	rateLimiter, err := NewRateLimiter(requestsPerSecond)
	if err != nil {
		return nil, err
	}

	circuitBreaker, err := NewCircuitBreaker(
		DefaultCircuitThreshold,
		DefaultCircuitCooldown,
	)
	if err != nil {
		return nil, err
	}

	return &Service{
		reader:         reader,
		ttl:            ttl,
		timeout:        timeout,
		capacity:       capacity,
		limiter:        limiter,
		rateLimiter:    rateLimiter,
		circuitBreaker: circuitBreaker,
		cache:          make(map[key]*list.Element),
		lru:            list.New(),
		inflight:       make(map[key]*flight),
	}, nil
}

func (s *Service) Metrics() MetricsSnapshot {
	s.mu.Lock()
	defer s.mu.Unlock()

	return MetricsSnapshot{
		CacheHits:         s.counters.cacheHits.Load(),
		CacheMisses:       s.counters.cacheMisses.Load(),
		CoalescedRequests: s.counters.coalescedRequests.Load(),
		UpstreamRequests:  s.counters.upstreamRequests.Load(),
		UpstreamErrors:    s.counters.upstreamErrors.Load(),
		CacheEvictions:    s.counters.cacheEvictions.Load(),
		CacheEntries:      len(s.cache),
		CacheCapacity:     s.capacity,
		LimiterSnapshot:   s.limiter.Snapshot(),
		RateSnapshot:      s.rateLimiter.Snapshot(),
		CircuitSnapshot:   s.circuitBreaker.Snapshot(),
	}
}

func (s *Service) removeEntry(element *list.Element) {
	item := element.Value.(*entry)
	delete(s.cache, item.key)
	s.lru.Remove(element)
}

func (s *Service) getCached(k key) (*big.Int, bool) {
	element, ok := s.cache[k]
	if !ok {
		return nil, false
	}

	item := element.Value.(*entry)

	if !time.Now().Before(item.expiresAt) {
		s.removeEntry(element)
		return nil, false
	}

	s.lru.MoveToFront(element)

	return new(big.Int).Set(item.amount), true
}

func (s *Service) pruneExpired(now time.Time) {
	for element := s.lru.Back(); element != nil; {
		previous := element.Prev()
		item := element.Value.(*entry)

		if !now.Before(item.expiresAt) {
			s.removeEntry(element)
		}

		element = previous
	}
}

func (s *Service) putCached(k key, amount *big.Int) {
	now := time.Now()
	s.pruneExpired(now)

	if element, ok := s.cache[k]; ok {
		item := element.Value.(*entry)
		item.amount = new(big.Int).Set(amount)
		item.expiresAt = now.Add(s.ttl)
		s.lru.MoveToFront(element)
		return
	}

	if len(s.cache) >= s.capacity {
		if oldest := s.lru.Back(); oldest != nil {
			s.removeEntry(oldest)
			s.counters.cacheEvictions.Add(1)
		}
	}

	element := s.lru.PushFront(&entry{
		key:       k,
		amount:    new(big.Int).Set(amount),
		expiresAt: now.Add(s.ttl),
	})

	s.cache[k] = element
}

func (s *Service) GetBalance(
	ctx context.Context,
	chainID uint64,
	address string,
) (*big.Int, error) {
	if ctx == nil || chainID == 0 || address == "" {
		return nil, ErrInvalidConfiguration
	}

	if err := ctx.Err(); err != nil {
		return nil, err
	}

	k := key{
		chainID: chainID,
		address: strings.ToLower(address),
	}

	s.mu.Lock()

	if err := ctx.Err(); err != nil {
		s.mu.Unlock()
		return nil, err
	}

	if amount, ok := s.getCached(k); ok {
		s.counters.cacheHits.Add(1)
		s.mu.Unlock()
		return amount, nil
	}

	s.counters.cacheMisses.Add(1)

	if f, ok := s.inflight[k]; ok {
		s.counters.coalescedRequests.Add(1)
		f.waiters++
		s.mu.Unlock()

		return s.wait(ctx, k, f)
	}

	upstreamCtx, cancel := context.WithTimeout(
		context.Background(),
		s.timeout,
	)

	f := &flight{
		done:    make(chan struct{}),
		cancel:  cancel,
		waiters: 1,
	}

	s.inflight[k] = f
	s.mu.Unlock()

	go s.fetch(upstreamCtx, k, address, f)

	return s.wait(ctx, k, f)
}

func (s *Service) fetch(
	ctx context.Context,
	k key,
	address string,
	f *flight,
) {
	defer f.cancel()

	// Acquire concurrency capacity once for the retry workflow.
	err := s.limiter.Acquire(ctx)

	var amount *big.Int

	if err == nil {
		func() {
			defer s.limiter.Release()

			err = s.circuitBreaker.Call(
				ctx,
				func(ctx context.Context) error {
					var callErr error
					amount, callErr = s.fetchWithRetry(ctx, k, address)
					return callErr
				},
			)
		}()
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	// An abandoned flight must not overwrite newer results.
	if s.inflight[k] != f {
		return
	}

	if err == nil {
		f.amount = new(big.Int).Set(amount)
		s.putCached(k, amount)
	} else {
		f.err = err
	}

	delete(s.inflight, k)
	close(f.done)
}

func (s *Service) wait(
	ctx context.Context,
	k key,
	f *flight,
) (*big.Int, error) {
	select {
	case <-ctx.Done():
		s.mu.Lock()

		if s.inflight[k] == f {
			f.waiters--

			if f.waiters == 0 {
				delete(s.inflight, k)
				f.cancel()
			}
		}

		s.mu.Unlock()

		return nil, ctx.Err()

	case <-f.done:
		if err := ctx.Err(); err != nil {
			return nil, err
		}

		if f.err != nil {
			return nil, f.err
		}

		return new(big.Int).Set(f.amount), nil
	}
}
