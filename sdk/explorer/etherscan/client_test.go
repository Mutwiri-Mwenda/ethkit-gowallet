package etherscan

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

const testAddress = "0x0000000000000000000000000000000000000000"
const testChainID uint64 = 11155111

func testClient(
	t *testing.T,
	handler http.HandlerFunc,
) *Client {
	t.Helper()

	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)

	client, err := newClient("test-secret-key", server.URL)
	if err != nil {
		t.Fatal(err)
	}
	return client
}

func TestNewClient(t *testing.T) {
	for _, key := range []string{"", " ", "bad key", "bad\nkey"} {
		_, err := NewClient(key)
		if !errors.Is(err, ErrInvalidAPIKey) {
			t.Fatalf("key %q: expected ErrInvalidAPIKey, got %v",
				key, err)
		}
	}

	client, err := NewClient("valid-test-key")
	if err != nil {
		t.Fatal(err)
	}
	if client.endpoint != baseURL {
		t.Fatal("unexpected production endpoint")
	}
}

func TestGetBalanceSuccess(t *testing.T) {
	client := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("expected GET, got %s", r.Method)
		}

		q := r.URL.Query()
		expected := map[string]string{
			"chainid": "11155111",
			"module":  "account",
			"action":  "balance",
			"address": testAddress,
			"tag":     "latest",
			"apikey":  "test-secret-key",
		}

		for key, want := range expected {
			if got := q.Get(key); got != want {
				t.Errorf("%s: expected %q, got %q", key, want, got)
			}
		}

		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, `{
			"status":"1",
			"message":"OK",
			"result":"1000000000000000000"
		}`)
	})

	balance, err := client.GetBalance(
		context.Background(), testChainID, testAddress)
	if err != nil {
		t.Fatal(err)
	}

	if balance.String() != "1000000000000000000" {
		t.Fatalf("unexpected balance: %s", balance)
	}
}

func TestGetBalanceFailures(t *testing.T) {
	tests := []struct {
		name    string
		status  int
		body    string
		wantErr error
	}{
		{
			"HTTP 429", 429, `rate limited`, ErrRateLimited,
		},
		{
			"HTTP 500", 500, `server failure`, ErrAPI,
		},
		{
			"HTTP 503", 503, `unavailable`, ErrAPI,
		},
		{
			"malformed JSON", 200, `{invalid`, ErrInvalidResponse,
		},
		{
			"null result", 200,
			`{"status":"1","message":"OK","result":null}`,
			ErrInvalidResponse,
		},
		{
			"invalid balance", 200,
			`{"status":"1","message":"OK","result":"abc"}`,
			ErrInvalidBalance,
		},
		{
			"negative balance", 200,
			`{"status":"1","message":"OK","result":"-100"}`,
			ErrInvalidBalance,
		},
		{
			"API rejection", 200,
			`{"status":"0","message":"NOTOK","result":"Invalid API Key"}`,
			ErrAPI,
		},
		{
			"API rate limit", 200,
			`{"status":"0","message":"NOTOK","result":"Max rate limit reached"}`,
			ErrRateLimited,
		},
		{
			"oversized response", 200,
			strings.Repeat("x", maxResponseBytes+1),
			ErrInvalidResponse,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := testClient(t,
				func(w http.ResponseWriter, r *http.Request) {
					w.WriteHeader(tt.status)
					_, _ = fmt.Fprint(w, tt.body)
				})

			_, err := client.GetBalance(
				context.Background(), testChainID, testAddress)

			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("expected %v, got %v", tt.wantErr, err)
			}

			if err != nil &&
				strings.Contains(err.Error(), "test-secret-key") {
				t.Fatal("API key leaked in error")
			}
		})
	}
}

func TestGetBalanceInvalidAddress(t *testing.T) {
	client, err := NewClient("test-key")
	if err != nil {
		t.Fatal(err)
	}

	_, err = client.GetBalance(
		context.Background(), testChainID, "invalid")

	if !errors.Is(err, ErrInvalidAddress) {
		t.Fatalf("expected ErrInvalidAddress, got %v", err)
	}
}

func TestGetBalanceRejectsRedirect(t *testing.T) {
	var redirected bool

	destination := httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			redirected = true
		},
	))
	defer destination.Close()

	client := testClient(t,
		func(w http.ResponseWriter, r *http.Request) {
			http.Redirect(w, r, destination.URL, http.StatusFound)
		})

	_, err := client.GetBalance(
		context.Background(), testChainID, testAddress)

	if !errors.Is(err, ErrAPI) {
		t.Fatalf("expected ErrAPI, got %v", err)
	}

	if redirected {
		t.Fatal("client followed a redirect")
	}
}

func TestGetBalanceCancellation(t *testing.T) {
	client := testClient(t,
		func(w http.ResponseWriter, r *http.Request) {
			<-r.Context().Done()
		})

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := client.GetBalance(ctx, testChainID, testAddress)

	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got %v", err)
	}
}

func TestGetBalanceTimeout(t *testing.T) {
	client := testClient(t,
		func(w http.ResponseWriter, r *http.Request) {
			time.Sleep(100 * time.Millisecond)
			_, _ = fmt.Fprint(w, `{
				"status":"1",
				"message":"OK",
				"result":"0"
			}`)
		})

	ctx, cancel := context.WithTimeout(
		context.Background(), 10*time.Millisecond)
	defer cancel()

	_, err := client.GetBalance(ctx, testChainID, testAddress)

	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected deadline exceeded, got %v", err)
	}
}

func TestGetBalanceTransportErrorRedaction(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {},
	))
	endpoint := server.URL
	server.Close()

	client, err := newClient("private-test-key", endpoint)
	if err != nil {
		t.Fatal(err)
	}

	_, err = client.GetBalance(
		context.Background(), testChainID, testAddress)

	if !errors.Is(err, ErrTransport) {
		t.Fatalf("expected ErrTransport, got %v", err)
	}

	if strings.Contains(err.Error(), "private-test-key") {
		t.Fatal("API key leaked in transport error")
	}
}
