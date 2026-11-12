package handler

import (
	"context"
	"encoding/json"
	"errors"
	"math/big"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Mutwiri-Mwenda/ethkit-gowallet/apps/api/internal/balance"
)

type overloadedReader struct{}

func (overloadedReader) GetBalance(
	ctx context.Context,
	chainID uint64,
	address string,
) (*big.Int, error) {
	return nil, balance.ErrOverloaded
}

func TestBalanceOverloadReturns503(t *testing.T) {
	h, err := New(overloadedReader{}, 11155111)
	if err != nil {
		t.Fatal(err)
	}

	address := "0x0000000000000000000000000000000000000000"
	path := "/api/v1/wallets/" + address + "/balance"

	req := httptest.NewRequest(http.MethodGet, path, nil)
	rec := httptest.NewRecorder()

	h.Routes().ServeHTTP(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", rec.Code)
	}

	if got := rec.Header().Get("Retry-After"); got != "1" {
		t.Fatalf("Retry-After = %q, want 1", got)
	}

	if got := rec.Header().Get("Content-Type"); got != "application/json" {
		t.Fatalf("Content-Type = %q, want application/json", got)
	}

	var response struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}

	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if response.Error.Code != "upstream_overloaded" {
		t.Fatalf("error code = %q", response.Error.Code)
	}

	if response.Error.Message != http.StatusText(http.StatusServiceUnavailable) {
		t.Fatalf("unexpected message: %q", response.Error.Message)
	}
}

func TestOverloadErrorIdentity(t *testing.T) {
	wrapped := errors.New("unrelated error")

	if errors.Is(wrapped, balance.ErrOverloaded) {
		t.Fatal("unrelated error matched ErrOverloaded")
	}
}
