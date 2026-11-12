package rpc

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestNewClient(t *testing.T) {
	tests := []struct {
		name    string
		url     string
		wantErr bool
	}{
		{"HTTPS", "https://rpc.example.com", false},
		{"local HTTP", "http://localhost:8545", false},
		{"loopback HTTP", "http://127.0.0.1:8545", false},
		{"empty", "", true},
		{"FTP", "ftp://example.com", true},
		{"missing host", "https://", true},
		{"remote HTTP", "http://rpc.example.com", true},
		{"credentials", "https://user:pass@example.com", true},
		{"fragment", "https://example.com/#secret", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, err := NewClient(tt.url)
			if tt.wantErr {
				if !errors.Is(err, ErrInvalidEndpoint) {
					t.Fatalf("expected invalid endpoint, got %v", err)
				}
				return
			}
			if err != nil || c == nil {
				t.Fatalf("expected client, got %v", err)
			}
		})
	}
}

func TestClientCallSuccess(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodPost {
				t.Errorf("expected POST, got %s", r.Method)
			}
			if r.Header.Get("Content-Type") != "application/json" {
				t.Error("missing JSON content type")
			}

			var req Request
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				t.Errorf("decode request: %v", err)
				return
			}
			if req.JSONRPC != "2.0" || req.Method != "eth_getBalance" {
				t.Errorf("unexpected request: %+v", req)
			}

			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"jsonrpc": "2.0",
				"id":      req.ID,
				"result":  "0xde0b6b3a7640000",
			})
		},
	))
	defer server.Close()

	client, err := NewClient(server.URL)
	if err != nil {
		t.Fatal(err)
	}

	var balance string
	err = client.Call(context.Background(), "eth_getBalance",
		[]any{"0x0000000000000000000000000000000000000001", "latest"},
		&balance,
	)
	if err != nil {
		t.Fatal(err)
	}
	if balance != "0xde0b6b3a7640000" {
		t.Fatalf("unexpected balance: %s", balance)
	}
}

func TestClientCallInvalidResponses(t *testing.T) {
	tests := []struct {
		name     string
		status   int
		body     string
		expected error
	}{
		{"malformed JSON", 200, `{invalid`, ErrInvalidResponse},
		{"JSON array", 200, `[]`, ErrInvalidResponse},
		{"wrong version", 200, `{"jsonrpc":"1.0","id":1,"result":"0x0"}`, ErrInvalidResponse},
		{"wrong ID", 200, `{"jsonrpc":"2.0","id":99,"result":"0x0"}`, ErrInvalidResponse},
		{"missing ID", 200, `{"jsonrpc":"2.0","result":"0x0"}`, ErrInvalidResponse},
		{"missing result", 200, `{"jsonrpc":"2.0","id":1}`, ErrInvalidResponse},
		{"null result", 200, `{"jsonrpc":"2.0","id":1,"result":null}`, ErrInvalidResponse},
		{"null error", 200, `{"jsonrpc":"2.0","id":1,"error":null}`, ErrInvalidResponse},
		{"both fields", 200, `{"jsonrpc":"2.0","id":1,"result":"0x0","error":{"code":-1,"message":"failed"}}`, ErrInvalidResponse},
		{"invalid error", 200, `{"jsonrpc":"2.0","id":1,"error":{"message":"failed"}}`, ErrInvalidResponse},
		{"wrong result type", 200, `{"jsonrpc":"2.0","id":1,"result":123}`, ErrInvalidResponse},
		{"HTTP 500", 500, `failure`, ErrHTTPStatus},
		{"HTTP 503", 503, `unavailable`, ErrHTTPStatus},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(
				func(w http.ResponseWriter, r *http.Request) {
					w.WriteHeader(tt.status)
					_, _ = w.Write([]byte(tt.body))
				},
			))
			defer server.Close()

			client, err := NewClient(server.URL)
			if err != nil {
				t.Fatal(err)
			}

			var result string
			err = client.Call(context.Background(), "eth_chainId",
				[]any{}, &result)

			if !errors.Is(err, tt.expected) {
				t.Fatalf("expected %v, got %v", tt.expected, err)
			}
		})
	}
}

func TestClientCallRPCError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			_, _ = fmt.Fprint(w, `{
				"jsonrpc":"2.0",
				"id":1,
				"error":{"code":-32601,"message":"Method not found"}
			}`)
		},
	))
	defer server.Close()

	client, err := NewClient(server.URL)
	if err != nil {
		t.Fatal(err)
	}

	var result string
	err = client.Call(context.Background(), "unknown", []any{}, &result)

	var rpcErr *RPCError
	if !errors.As(err, &rpcErr) {
		t.Fatalf("expected RPCError, got %v", err)
	}
	if rpcErr.Code != -32601 {
		t.Fatalf("unexpected code: %d", rpcErr.Code)
	}
}

func TestClientCallCancellation(t *testing.T) {
	client, err := NewClient("http://localhost:8545")
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	var result string
	err = client.Call(ctx, "eth_chainId", []any{}, &result)

	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected cancellation, got %v", err)
	}
}

func TestClientCallTimeout(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			time.Sleep(100 * time.Millisecond)
			_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":"0x1"}`))
		},
	))
	defer server.Close()

	client, err := NewClient(server.URL)
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(
		context.Background(), 10*time.Millisecond)
	defer cancel()

	var result string
	err = client.Call(ctx, "eth_chainId", []any{}, &result)

	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected deadline exceeded, got %v", err)
	}
	if !IsTimeout(err) {
		t.Fatal("expected IsTimeout to return true")
	}
}

func TestClientCallResponseLimit(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte(strings.Repeat("x", maxResponseBytes+1)))
		},
	))
	defer server.Close()

	client, err := NewClient(server.URL)
	if err != nil {
		t.Fatal(err)
	}

	var result string
	err = client.Call(context.Background(), "eth_chainId", []any{}, &result)

	if !errors.Is(err, ErrInvalidResponse) {
		t.Fatalf("expected invalid response, got %v", err)
	}
}

func TestClientCallConcurrent(t *testing.T) {
	var mu sync.Mutex
	seen := make(map[uint64]bool)

	server := httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			var req Request
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				t.Errorf("decode: %v", err)
				return
			}

			mu.Lock()
			if seen[req.ID] {
				t.Errorf("duplicate request ID: %d", req.ID)
			}
			seen[req.ID] = true
			mu.Unlock()

			_ = json.NewEncoder(w).Encode(map[string]any{
				"jsonrpc": "2.0",
				"id":      req.ID,
				"result":  "0x1",
			})
		},
	))
	defer server.Close()

	client, err := NewClient(server.URL)
	if err != nil {
		t.Fatal(err)
	}

	const workers = 50
	var wg sync.WaitGroup
	errCh := make(chan error, workers)

	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			var result string
			err := client.Call(context.Background(),
				"eth_chainId", []any{}, &result)
			if err != nil {
				errCh <- err
			} else if result != "0x1" {
				errCh <- fmt.Errorf("unexpected result: %s", result)
			}
		}()
	}

	wg.Wait()
	close(errCh)

	for err := range errCh {
		t.Error(err)
	}

	mu.Lock()
	defer mu.Unlock()
	if len(seen) != workers {
		t.Fatalf("expected %d unique IDs, got %d", workers, len(seen))
	}
}
