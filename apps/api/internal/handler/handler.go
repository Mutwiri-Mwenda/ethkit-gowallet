package handler

import (
	"context"
	"encoding/json"
	"errors"
	"math/big"
	"net/http"
	"strings"
	"time"

	"github.com/Mutwiri-Mwenda/ethkit-gowallet/apps/api/internal/balance"
	"github.com/Mutwiri-Mwenda/gowallet/sdk/explorer/etherscan"
)

type BalanceReader interface {
	GetBalance(context.Context, uint64, string) (*big.Int, error)
}

type Handler struct {
	balances BalanceReader
	chainID  uint64
}

func New(balances BalanceReader, chainID uint64) (*Handler, error) {
	if balances == nil || chainID == 0 {
		return nil, errors.New("invalid API configuration")
	}
	return &Handler{balances: balances, chainID: chainID}, nil
}

func (h *Handler) Routes() http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /api/v1/health", h.health)
	mux.HandleFunc("GET /api/v1/chain", h.chain)
	mux.HandleFunc(
		"GET /api/v1/wallets/{address}/balance",
		h.balance,
	)

	return mux
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func writeError(w http.ResponseWriter, status int, code string) {
	writeJSON(w, status, map[string]any{
		"error": map[string]string{
			"code":    code,
			"message": http.StatusText(status),
		},
	})
}

func (h *Handler) health(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{
		"status": "ok",
	})
}

func (h *Handler) chain(w http.ResponseWriter, r *http.Request) {
	name := "unknown"
	if h.chainID == 11155111 {
		name = "sepolia"
	} else if h.chainID == 1 {
		name = "ethereum-mainnet"
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"chain_id": h.chainID,
		"name":     name,
		"verified": false,
	})
}

func (h *Handler) balance(w http.ResponseWriter, r *http.Request) {
	address := r.PathValue("address")

	if len(address) != 42 ||
		!strings.HasPrefix(address, "0x") {
		writeError(w, http.StatusBadRequest, "invalid_address")
		return
	}

	for _, c := range address[2:] {
		if !((c >= '0' && c <= '9') ||
			(c >= 'a' && c <= 'f') ||
			(c >= 'A' && c <= 'F')) {
			writeError(w, http.StatusBadRequest, "invalid_address")
			return
		}
	}

	ctx, cancel := context.WithTimeout(r.Context(), 8*time.Second)
	defer cancel()

	amount, err := h.balances.GetBalance(
		ctx, h.chainID, address,
	)
	if err != nil {
		switch {
		case errors.Is(err, balance.ErrOverloaded):
			w.Header().Set("Retry-After", "1")
			writeError(w, http.StatusServiceUnavailable, "upstream_overloaded")

		case errors.Is(err, etherscan.ErrInvalidAddress):
			writeError(w, http.StatusBadRequest, "invalid_address")

		case errors.Is(err, etherscan.ErrRateLimited):
			writeError(w, http.StatusServiceUnavailable, "provider_rate_limited")
		case errors.Is(err, balance.ErrCircuitOpen):
			w.Header().Set("Retry-After", "10")
			writeError(w, http.StatusServiceUnavailable, "upstream_circuit_open")

		case errors.Is(err, context.DeadlineExceeded):
			writeError(w, http.StatusGatewayTimeout, "provider_timeout")

		case errors.Is(err, context.Canceled):
			// The caller may have disconnected.
			return

		default:
			writeError(w, http.StatusBadGateway, "provider_failure")
		}
		return
	}

	if amount == nil || amount.Sign() < 0 {
		writeError(w, http.StatusBadGateway, "invalid_provider_balance")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"address":     address,
		"chain_id":    h.chainID,
		"balance_wei": amount.String(),
	})
}
