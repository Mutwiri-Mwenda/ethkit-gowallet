package observability

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"log/slog"
	"net/http"
	"sync/atomic"
	"time"
)

type Metrics struct {
	requests atomic.Uint64
	errors   atomic.Uint64
	active   atomic.Int64
	totalNS  atomic.Uint64
}

type Snapshot struct {
	Requests       uint64  `json:"requests_total"`
	Errors         uint64  `json:"errors_total"`
	Active         int64   `json:"active_requests"`
	AverageLatency float64 `json:"average_latency_ms"`
}

func NewMetrics() *Metrics {
	return &Metrics{}
}

func (m *Metrics) Snapshot() Snapshot {
	requests := m.requests.Load()

	var average float64
	if requests > 0 {
		average = float64(m.totalNS.Load()) /
			float64(requests) / 1e6
	}

	return Snapshot{
		Requests:       requests,
		Errors:         m.errors.Load(),
		Active:         m.active.Load(),
		AverageLatency: average,
	}
}

func (m *Metrics) Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.Header().Set("Allow", "GET")
			http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		_ = json.NewEncoder(w).Encode(m.Snapshot())
	})
}

type responseRecorder struct {
	http.ResponseWriter
	status int
}

func (r *responseRecorder) WriteHeader(status int) {
	if r.status != 0 {
		return
	}
	r.status = status
	r.ResponseWriter.WriteHeader(status)
}

func (r *responseRecorder) Write(p []byte) (int, error) {
	if r.status == 0 {
		r.WriteHeader(http.StatusOK)
	}
	return r.ResponseWriter.Write(p)
}

func requestID() (string, error) {
	var id [16]byte
	if _, err := rand.Read(id[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(id[:]), nil
}

func Middleware(
	next http.Handler,
	logger *slog.Logger,
	metrics *Metrics,
) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id, err := requestID()
		if err != nil {
			http.Error(w, "Internal Server Error",
				http.StatusInternalServerError)
			return
		}

		w.Header().Set("X-Request-ID", id)

		start := time.Now()
		metrics.active.Add(1)
		defer metrics.active.Add(-1)

		rec := &responseRecorder{ResponseWriter: w}

		defer func() {
			if rec.status == 0 {
				rec.status = http.StatusOK
			}

			duration := time.Since(start)
			metrics.requests.Add(1)
			metrics.totalNS.Add(uint64(duration.Nanoseconds()))

			if rec.status >= 500 {
				metrics.errors.Add(1)
			}

			logger.Info("http_request",
				"request_id", id,
				"method", r.Method,
				"route", r.Pattern,
				"status", rec.status,
				"duration_ms",
				float64(duration.Microseconds())/1000,
			)
		}()

		next.ServeHTTP(rec, r)
	})
}
