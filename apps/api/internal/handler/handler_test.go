package handler

import (
	"context"
	"encoding/json"
	"errors"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Mutwiri-Mwenda/gowallet/sdk/explorer/etherscan"
)

type fakeBalances struct {
	amount *big.Int
	err    error
}

func (f fakeBalances) GetBalance(
	ctx context.Context,
	chainID uint64,
	address string,
) (*big.Int, error) {
	return f.amount, f.err
}

func newRouter(t *testing.T, reader BalanceReader) http.Handler {
	t.Helper()
	h, err := New(reader, 11155111)
	if err != nil {
		t.Fatal(err)
	}
	return h.Routes()
}

func TestHealth(t *testing.T) {
	router := newRouter(t, fakeBalances{})
	req := httptest.NewRequest(http.MethodGet, "/api/v1/health", nil)
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), `"ok"`) {
		t.Fatalf("unexpected response: %s", rec.Body.String())
	}
}

func TestChain(t *testing.T) {
	router := newRouter(t, fakeBalances{})
	req := httptest.NewRequest(http.MethodGet, "/api/v1/chain", nil)
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	var body struct {
		ChainID  uint64 `json:"chain_id"`
		Name     string `json:"name"`
		Verified bool   `json:"verified"`
	}

	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if rec.Code != 200 || body.ChainID != 11155111 ||
		body.Name != "sepolia" || body.Verified {
		t.Fatalf("unexpected chain response: %+v", body)
	}
}

func TestBalanceSuccess(t *testing.T) {
	router := newRouter(t, fakeBalances{
		amount: big.NewInt(1000000000000000000),
	})

	path := "/api/v1/wallets/" +
		"0x0000000000000000000000000000000000000000/balance"

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest("GET", path, nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	var body struct {
		BalanceWei string `json:"balance_wei"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.BalanceWei != "1000000000000000000" {
		t.Fatalf("unexpected balance: %s", body.BalanceWei)
	}
}

func TestBalanceInvalidAddress(t *testing.T) {
	router := newRouter(t, fakeBalances{})

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(
		"GET", "/api/v1/wallets/not-an-address/balance", nil,
	))

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestBalanceProviderErrors(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want int
	}{
		{
			"rate limit",
			etherscan.ErrRateLimited,
			http.StatusServiceUnavailable,
		},
		{
			"timeout",
			context.DeadlineExceeded,
			http.StatusGatewayTimeout,
		},
		{
			"provider failure",
			errors.New("upstream unavailable"),
			http.StatusBadGateway,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			router := newRouter(t, fakeBalances{err: tt.err})
			path := "/api/v1/wallets/" +
				"0x0000000000000000000000000000000000000000/balance"

			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, httptest.NewRequest("GET", path, nil))

			if rec.Code != tt.want {
				t.Fatalf("expected %d, got %d", tt.want, rec.Code)
			}

			if strings.Contains(rec.Body.String(), "upstream unavailable") {
				t.Fatal("internal error leaked to client")
			}
		})
	}
}

func TestMethodNotAllowed(t *testing.T) {
	router := newRouter(t, fakeBalances{})
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, httptest.NewRequest(
		"POST", "/api/v1/health", nil,
	))

	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405, got %d", rec.Code)
	}
}
