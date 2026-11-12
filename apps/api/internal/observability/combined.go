package observability

import (
	"encoding/json"
	"net/http"

	"github.com/Mutwiri-Mwenda/ethkit-gowallet/apps/api/internal/balance"
)

type BalanceMetricsReader interface {
	Metrics() balance.MetricsSnapshot
}

type CombinedSnapshot struct {
	Snapshot
	balance.MetricsSnapshot
}

func CombinedHandler(
	httpMetrics *Metrics,
	balanceMetrics BalanceMetricsReader,
) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.Header().Set("Allow", "GET")
			http.Error(w, "Method Not Allowed",
				http.StatusMethodNotAllowed)
			return
		}

		snapshot := CombinedSnapshot{
			Snapshot:        httpMetrics.Snapshot(),
			MetricsSnapshot: balanceMetrics.Metrics(),
		}

		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		_ = json.NewEncoder(w).Encode(snapshot)
	})
}
