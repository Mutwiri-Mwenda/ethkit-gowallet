package evm

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Mutwiri-Mwenda/gowallet/sdk/rpc"
)

func TestGetBalance(t *testing.T) {
	server := httptest.NewServer(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")

			fmt.Fprint(w, `{
                "jsonrpc": "2.0",
                "id": 1,
                "result": "0xde0b6b3a7640000"
            }`)
		}),
	)
	defer server.Close()

	rpcClient, err := rpc.NewClient(server.URL)
	if err != nil {
		t.Fatal(err)
	}

	client := NewClient(rpcClient)

	balance, err := client.GetBalance(
		context.Background(),
		"0x0000000000000000000000000000000000000001",
	)
	if err != nil {
		t.Fatal(err)
	}

	expected := "1000000000000000000"

	if balance.String() != expected {
		t.Fatalf(
			"expected %s wei, got %s",
			expected,
			balance.String(),
		)
	}
}
