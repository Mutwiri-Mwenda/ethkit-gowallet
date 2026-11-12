package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Mutwiri-Mwenda/gowallet/sdk/explorer/etherscan"
)

const integrationAddress = "0x0000000000000000000000000000000000000000"

type balanceFunc func(
	context.Context,
	uint64,
	string,
) (*big.Int, error)

func (f balanceFunc) GetBalance(
	ctx context.Context,
	chainID uint64,
	address string,
) (*big.Int, error) {
	return f(ctx, chainID, address)
}

func startTestAPI(t *testing.T, reader BalanceReader) *httptest.Server {
	t.Helper()

	h, err := New(reader, 11155111)
	if err != nil {
		t.Fatal(err)
	}

	srv := httptest.NewServer(h.Routes())
	t.Cleanup(srv.Close)
	return srv
}

func requestBalance(
	ctx context.Context,
	client *http.Client,
	baseURL string,
	address string,
) (*http.Response, error) {
	req, err := http.NewRequestWithContext(
		ctx,
		http.MethodGet,
		baseURL+"/api/v1/wallets/"+address+"/balance",
		nil,
	)
	if err != nil {
		return nil, err
	}
	return client.Do(req)
}

func TestIntegrationBalanceSuccess(t *testing.T) {
	var calls atomic.Int64

	provider := balanceFunc(func(
		ctx context.Context,
		chainID uint64,
		address string,
	) (*big.Int, error) {
		calls.Add(1)

		if chainID != 11155111 {
			t.Errorf("unexpected chain ID: %d", chainID)
		}
		if address != integrationAddress {
			t.Errorf("unexpected address: %s", address)
		}

		amount, ok := new(big.Int).SetString(
			"123456789012345678901234567890", 10,
		)
		if !ok {
			return nil, errors.New("invalid test fixture")
		}
		return amount, nil
	})

	srv := startTestAPI(t, provider)

	resp, err := requestBalance(
		context.Background(),
		srv.Client(),
		srv.URL,
		integrationAddress,
	)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	if got := resp.Header.Get("Content-Type"); got != "application/json" {
		t.Fatalf("unexpected content type: %s", got)
	}
	if got := resp.Header.Get("Cache-Control"); got != "no-store" {
		t.Fatalf("unexpected cache policy: %s", got)
	}

	var body struct {
		Address    string `json:"address"`
		ChainID    uint64 `json:"chain_id"`
		BalanceWei string `json:"balance_wei"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}

	if body.Address != integrationAddress ||
		body.ChainID != 11155111 ||
		body.BalanceWei != "123456789012345678901234567890" {
		t.Fatalf("unexpected response: %+v", body)
	}

	if calls.Load() != 1 {
		t.Fatalf("expected one provider call, got %d", calls.Load())
	}
}

func TestIntegrationInvalidAddressSkipsProvider(t *testing.T) {
	var calls atomic.Int64

	provider := balanceFunc(func(
		ctx context.Context,
		chainID uint64,
		address string,
	) (*big.Int, error) {
		calls.Add(1)
		return big.NewInt(0), nil
	})

	srv := startTestAPI(t, provider)

	resp, err := requestBalance(
		context.Background(),
		srv.Client(),
		srv.URL,
		"invalid-address",
	)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
	if calls.Load() != 0 {
		t.Fatal("invalid address reached the provider")
	}
}

func TestIntegrationProviderFailures(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want int
		code string
	}{
		{
			"rate limited",
			&etherscan.APIError{
				StatusCode: 429,
				RateLimit:  true,
			},
			http.StatusServiceUnavailable,
			"provider_rate_limited",
		},
		{
			"deadline exceeded",
			context.DeadlineExceeded,
			http.StatusGatewayTimeout,
			"provider_timeout",
		},
		{
			"upstream HTTP failure",
			&etherscan.APIError{StatusCode: 500},
			http.StatusBadGateway,
			"provider_failure",
		},
		{
			"malformed upstream response",
			etherscan.ErrInvalidResponse,
			http.StatusBadGateway,
			"provider_failure",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			provider := balanceFunc(func(
				ctx context.Context,
				chainID uint64,
				address string,
			) (*big.Int, error) {
				return nil, tt.err
			})

			srv := startTestAPI(t, provider)

			resp, err := requestBalance(
				context.Background(),
				srv.Client(),
				srv.URL,
				integrationAddress,
			)
			if err != nil {
				t.Fatal(err)
			}
			defer resp.Body.Close()

			if resp.StatusCode != tt.want {
				t.Fatalf(
					"expected %d, got %d",
					tt.want,
					resp.StatusCode,
				)
			}

			var body struct {
				Error struct {
					Code    string `json:"code"`
					Message string `json:"message"`
				} `json:"error"`
			}

			if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
				t.Fatal(err)
			}

			if body.Error.Code != tt.code {
				t.Fatalf(
					"expected error code %q, got %q",
					tt.code,
					body.Error.Code,
				)
			}

			if strings.Contains(body.Error.Message, "upstream") {
				t.Fatal("upstream error detail leaked")
			}
		})
	}
}

func TestIntegrationInvalidProviderBalance(t *testing.T) {
	for _, amount := range []*big.Int{nil, big.NewInt(-1)} {
		provider := balanceFunc(func(
			ctx context.Context,
			chainID uint64,
			address string,
		) (*big.Int, error) {
			return amount, nil
		})

		srv := startTestAPI(t, provider)

		resp, err := requestBalance(
			context.Background(),
			srv.Client(),
			srv.URL,
			integrationAddress,
		)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()

		if resp.StatusCode != http.StatusBadGateway {
			t.Fatalf(
				"expected 502 for invalid balance, got %d",
				resp.StatusCode,
			)
		}
	}
}

func TestIntegrationCancellation(t *testing.T) {
	started := make(chan struct{})
	canceled := make(chan struct{})

	provider := balanceFunc(func(
		ctx context.Context,
		chainID uint64,
		address string,
	) (*big.Int, error) {
		close(started)
		<-ctx.Done()
		close(canceled)
		return nil, ctx.Err()
	})

	srv := startTestAPI(t, provider)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	result := make(chan error, 1)

	go func() {
		resp, err := requestBalance(
			ctx, srv.Client(), srv.URL, integrationAddress,
		)
		if resp != nil {
			resp.Body.Close()
		}
		result <- err
	}()

	select {
	case <-started:
	case <-time.After(3 * time.Second):
		t.Fatal("provider was never called")
	}

	cancel()

	select {
	case err := <-result:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("expected cancellation, got %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("HTTP client did not stop after cancellation")
	}

	select {
	case <-canceled:
	case <-time.After(3 * time.Second):
		t.Fatal("provider did not observe request cancellation")
	}
}

func TestIntegrationConcurrentRequests(t *testing.T) {
	const requests = 50

	var calls atomic.Int64

	provider := balanceFunc(func(
		ctx context.Context,
		chainID uint64,
		address string,
	) (*big.Int, error) {
		calls.Add(1)
		return big.NewInt(42), nil
	})

	srv := startTestAPI(t, provider)

	var wg sync.WaitGroup
	errs := make(chan error, requests)

	for i := 0; i < requests; i++ {
		wg.Add(1)

		go func() {
			defer wg.Done()

			resp, err := requestBalance(
				context.Background(),
				srv.Client(),
				srv.URL,
				integrationAddress,
			)
			if err != nil {
				errs <- err
				return
			}
			defer resp.Body.Close()

			if resp.StatusCode != http.StatusOK {
				errs <- fmt.Errorf(
					"unexpected status: %d",
					resp.StatusCode,
				)
				return
			}

			var body struct {
				BalanceWei string `json:"balance_wei"`
			}

			if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
				errs <- err
				return
			}
			if body.BalanceWei != "42" {
				errs <- fmt.Errorf(
					"unexpected balance: %s",
					body.BalanceWei,
				)
			}
		}()
	}

	wg.Wait()
	close(errs)

	for err := range errs {
		t.Error(err)
	}

	if calls.Load() != requests {
		t.Fatalf(
			"expected %d provider calls, got %d",
			requests,
			calls.Load(),
		)
	}
}
