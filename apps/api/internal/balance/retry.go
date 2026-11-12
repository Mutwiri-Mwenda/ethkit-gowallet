package balance

import (
	"context"
	"errors"
	"math/big"
	"math/rand"
	"time"

	"github.com/Mutwiri-Mwenda/gowallet/sdk/explorer/etherscan"
)

type RetryPolicy struct {
	MaxAttempts int
	BaseDelay   time.Duration
	MaxDelay    time.Duration
}

func DefaultRetryPolicy() RetryPolicy {
	return RetryPolicy{
		MaxAttempts: 3,
		BaseDelay:   200 * time.Millisecond,
		MaxDelay:    2 * time.Second,
	}
}

func (p RetryPolicy) Validate() error {
	if p.MaxAttempts < 1 ||
		p.BaseDelay <= 0 ||
		p.MaxDelay < p.BaseDelay {
		return ErrInvalidConfiguration
	}
	return nil
}

// Retryable reports whether another attempt might succeed.
// Add additional error types only when the SDK
// explicitly guarantees they represent transient failures.
func Retryable(err error) bool {
	return errors.Is(err, etherscan.ErrRateLimited)
}

func (p RetryPolicy) backoff(retryNumber int) time.Duration {
	delay := p.BaseDelay

	for i := 1; i < retryNumber; i++ {
		if delay >= p.MaxDelay/2 {
			delay = p.MaxDelay
			break
		}
		delay *= 2
	}

	if delay > p.MaxDelay {
		delay = p.MaxDelay
	}

	// Full jitter: randomized delay between zero
	// and the exponential-backoff ceiling.
	return time.Duration(rand.Int63n(int64(delay) + 1))
}

func waitRetry(ctx context.Context, delay time.Duration) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	timer := time.NewTimer(delay)
	defer timer.Stop()

	select {
	case <-timer.C:
		return ctx.Err()
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (s *Service) fetchWithRetry(
	ctx context.Context,
	k key,
	address string,
) (*big.Int, error) {
	policy := DefaultRetryPolicy()

	for attempt := 1; attempt <= policy.MaxAttempts; attempt++ {
		if err := ctx.Err(); err != nil {
			return nil, err
		}

		// Every attempt consumes a rate admission.
		if err := s.rateLimiter.Wait(ctx); err != nil {
			return nil, err
		}

		if err := ctx.Err(); err != nil {
			return nil, err
		}

		s.counters.upstreamRequests.Add(1)

		amount, err := s.reader.GetBalance(
			ctx, k.chainID, address,
		)

		if err == nil {
			if amount == nil || amount.Sign() < 0 {
				s.counters.upstreamErrors.Add(1)
				return nil, errors.New("invalid upstream balance")
			}

			return amount, nil
		}

		s.counters.upstreamErrors.Add(1)

		if !Retryable(err) || attempt == policy.MaxAttempts {
			return nil, err
		}

		delay := policy.backoff(attempt)

		if err := waitRetry(ctx, delay); err != nil {
			return nil, err
		}
	}

	return nil, errors.New("retry attempts exhausted")
}
