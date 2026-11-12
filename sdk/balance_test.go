package gowallet

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestGetBalance(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodPost {
				t.Errorf("expected POST, got %s", r.Method)
			}

			w.Header().Set("Content-Type", "application/json")
			fmt.Fprint(w, `{
                "jsonrpc":"2.0",
                "id":1,
                "result":"0xde0b6b3a7640000"
            }`)
		},
	))
	defer server.Close()

	client, err := NewClient(server.URL)
	if err != nil {
		t.Fatal(err)
	}

	address := "0x0000000000000000000000000000000000000001"

	balance, err := client.GetBalance(context.Background(), address)
	if err != nil {
		t.Fatal(err)
	}

	if balance.String() != "1000000000000000000" {
		t.Fatalf("unexpected balance: %s", balance)
	}
}

func TestInvalidAddress(t *testing.T) {
	client, err := NewClient("https://example.com")
	if err != nil {
		t.Fatal(err)
	}

	_, err = client.GetBalance(context.Background(), "invalid")

	if err == nil {
		t.Fatal("expected address validation error")
	}
}
