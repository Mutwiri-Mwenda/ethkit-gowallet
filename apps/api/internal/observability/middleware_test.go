package observability

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

func testLogger(output *bytes.Buffer) *slog.Logger {
	return slog.New(slog.NewJSONHandler(output, nil))
}

func TestMiddlewareRequestIDAndLogging(t *testing.T) {
	var logs bytes.Buffer
	metrics := NewMetrics()

	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/health",
		func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"status":"ok"}`))
		})

	handler := Middleware(mux, testLogger(&logs), metrics)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(
		http.MethodGet,
		"/api/v1/health?secret=do-not-log",
		nil,
	)

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	id := rec.Header().Get("X-Request-ID")
	if len(id) != 32 {
		t.Fatalf("unexpected request ID: %q", id)
	}

	var entry map[string]any
	if err := json.Unmarshal(logs.Bytes(), &entry); err != nil {
		t.Fatal(err)
	}

	if entry["request_id"] != id {
		t.Fatal("request ID missing from log")
	}
	if entry["status"] != float64(200) {
		t.Fatal("incorrect logged status")
	}
	if entry["route"] != "GET /api/v1/health" {
		t.Fatal("incorrect route pattern")
	}
	if strings.Contains(logs.String(), "do-not-log") {
		t.Fatal("query parameter leaked into logs")
	}

	snapshot := metrics.Snapshot()
	if snapshot.Requests != 1 ||
		snapshot.Errors != 0 ||
		snapshot.Active != 0 {
		t.Fatalf("unexpected metrics: %+v", snapshot)
	}
}

func TestMiddlewareCountsServerErrors(t *testing.T) {
	var logs bytes.Buffer
	metrics := NewMetrics()

	handler := Middleware(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.Error(w, "failure", http.StatusBadGateway)
		}),
		testLogger(&logs),
		metrics,
	)

	rec := httptest.NewRecorder()
	handler.ServeHTTP(
		rec,
		httptest.NewRequest(http.MethodGet, "/", nil),
	)

	snapshot := metrics.Snapshot()
	if snapshot.Requests != 1 || snapshot.Errors != 1 {
		t.Fatalf("unexpected metrics: %+v", snapshot)
	}
}

func TestMiddlewareConcurrentRequests(t *testing.T) {
	const count = 100

	metrics := NewMetrics()
	logger := slog.New(slog.NewJSONHandler(
		&discardWriter{}, nil,
	))

	handler := Middleware(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		}),
		logger,
		metrics,
	)

	server := httptest.NewServer(handler)
	defer server.Close()

	var wg sync.WaitGroup
	errors := make(chan error, count)

	for i := 0; i < count; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()

			resp, err := server.Client().Get(server.URL)
			if err != nil {
				errors <- err
				return
			}
			defer resp.Body.Close()

			if resp.StatusCode != http.StatusOK {
				errors <- &statusError{resp.StatusCode}
			}
		}()
	}

	wg.Wait()
	close(errors)

	for err := range errors {
		t.Error(err)
	}

	snapshot := metrics.Snapshot()
	if snapshot.Requests != count ||
		snapshot.Errors != 0 ||
		snapshot.Active != 0 {
		t.Fatalf("unexpected metrics: %+v", snapshot)
	}
}

type discardWriter struct{}

func (*discardWriter) Write(p []byte) (int, error) {
	return len(p), nil
}

type statusError struct {
	status int
}

func (e *statusError) Error() string {
	return http.StatusText(e.status)
}

func TestMetricsEndpoint(t *testing.T) {
	metrics := NewMetrics()

	rec := httptest.NewRecorder()
	metrics.Handler().ServeHTTP(
		rec,
		httptest.NewRequest(http.MethodGet, "/metrics", nil),
	)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	var snapshot Snapshot
	if err := json.Unmarshal(rec.Body.Bytes(), &snapshot); err != nil {
		t.Fatal(err)
	}

	if snapshot.Requests != 0 || snapshot.Errors != 0 {
		t.Fatalf("unexpected metrics: %+v", snapshot)
	}
}
