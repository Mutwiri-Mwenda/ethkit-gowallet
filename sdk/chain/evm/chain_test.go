package evm

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Mutwiri-Mwenda/gowallet/sdk/rpc"
)

func newTestChainClient(
	t *testing.T,
	result string,
) *Client {
	t.Helper()

	server := httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			var request rpc.Request

			if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
				t.Errorf("decode request: %v", err)
				http.Error(w, "invalid request", http.StatusBadRequest)
				return
			}

			if request.Method != "eth_chainId" {
				t.Errorf("unexpected method: %s", request.Method)
			}

			w.Header().Set("Content-Type", "application/json")

			if err := json.NewEncoder(w).Encode(map[string]any{
				"jsonrpc": "2.0",
				"id":      request.ID,
				"result":  result,
			}); err != nil {
				t.Errorf("encode response: %v", err)
			}
		},
	))

	t.Cleanup(server.Close)

	rpcClient, err := rpc.NewClient(server.URL)
	if err != nil {
		t.Fatal(err)
	}

	return NewClient(rpcClient)
}

func TestChainID(t *testing.T) {
	tests := []struct {
		name    string
		result  string
		want    uint64
		wantErr bool
	}{
		{"mainnet", "0x1", EthereumMainnet, false},
		{"sepolia", "0xaa36a7", Sepolia, false},
		{"empty", "", 0, true},
		{"missing prefix", "1", 0, true},
		{"invalid hex", "0xzz", 0, true},
		{"zero", "0x0", 0, true},
		{"overflow", "0x10000000000000000", 0, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := newTestChainClient(t, tt.result)

			got, err := client.ChainID(context.Background())

			if tt.wantErr {
				if !errors.Is(err, ErrInvalidChainID) {
					t.Fatalf("expected ErrInvalidChainID, got %v", err)
				}
				return
			}

			if err != nil {
				t.Fatal(err)
			}

			if got != tt.want {
				t.Fatalf("expected %d, got %d", tt.want, got)
			}
		})
	}
}

func TestVerifyChainIDSuccess(t *testing.T) {
	client := newTestChainClient(t, "0xaa36a7")

	err := client.VerifyChainID(
		context.Background(),
		Sepolia,
	)

	if err != nil {
		t.Fatalf("expected Sepolia verification to pass: %v", err)
	}
}

func TestVerifyChainIDMismatch(t *testing.T) {
	client := newTestChainClient(t, "0x1")

	err := client.VerifyChainID(
		context.Background(),
		Sepolia,
	)

	if !errors.Is(err, ErrChainMismatch) {
		t.Fatalf("expected ErrChainMismatch, got %v", err)
	}

	var mismatch *ChainMismatchError

	if !errors.As(err, &mismatch) {
		t.Fatalf("expected ChainMismatchError, got %v", err)
	}

	if mismatch.Expected != Sepolia {
		t.Fatalf("unexpected expected chain: %d", mismatch.Expected)
	}

	if mismatch.Actual != EthereumMainnet {
		t.Fatalf("unexpected actual chain: %d", mismatch.Actual)
	}
}

func TestVerifyChainIDRejectsZero(t *testing.T) {
	client := newTestChainClient(t, "0x1")

	err := client.VerifyChainID(context.Background(), 0)

	if !errors.Is(err, ErrInvalidChainID) {
		t.Fatalf("expected ErrInvalidChainID, got %v", err)
	}
}
