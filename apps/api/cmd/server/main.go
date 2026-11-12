package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/Mutwiri-Mwenda/ethkit-gowallet/apps/api/internal/balance"
	"github.com/Mutwiri-Mwenda/ethkit-gowallet/apps/api/internal/handler"
	"github.com/Mutwiri-Mwenda/ethkit-gowallet/apps/api/internal/observability"
	"github.com/Mutwiri-Mwenda/ethkit-gowallet/apps/api/internal/server"
	"github.com/Mutwiri-Mwenda/gowallet/sdk/explorer/etherscan"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(
		os.Stdout,
		&slog.HandlerOptions{Level: slog.LevelInfo},
	))

	apiKey := os.Getenv("ETHERSCAN_API_KEY")
	if apiKey == "" {
		logger.Error("ETHERSCAN_API_KEY is required")
		os.Exit(1)
	}

	client, err := etherscan.NewClient(apiKey)
	if err != nil {
		logger.Error("invalid Etherscan configuration")
		os.Exit(1)
	}

	balanceService, err := balance.NewWithProviderLimits(
		client,
		5*time.Second,
		8*time.Second,
		1000, // Cache capacity
		10,   // Concurrent upstream operations
		20,   // Waiting upstream operations
		5,    // Upstream request starts per second
	)
	if err != nil {
		logger.Error("invalid balance service configuration")
		os.Exit(1)
	}

	const chainID uint64 = 11155111

	h, err := handler.New(balanceService, chainID)
	if err != nil {
		logger.Error("invalid API configuration")
		os.Exit(1)
	}

	metrics := observability.NewMetrics()

	mux := http.NewServeMux()
	mux.Handle("/api/", h.Routes())
	mux.Handle(
		"GET /metrics",
		observability.CombinedHandler(metrics, balanceService),
	)

	httpHandler := observability.Middleware(
		mux,
		logger,
		metrics,
	)

	srv := server.New("127.0.0.1:8080", httpHandler)

	ctx, stop := signal.NotifyContext(
		context.Background(),
		os.Interrupt,
		syscall.SIGTERM,
	)
	defer stop()

	done := make(chan error, 1)
	go func() {
		done <- srv.ListenAndServe()
	}()

	logger.Info("api_starting",
		"address", "127.0.0.1:8080",
		"chain_id", chainID,
	)

	select {
	case err := <-done:
		if err != nil {
			logger.Error("server_failed", "error", err)
			os.Exit(1)
		}
		return
	case <-ctx.Done():
		logger.Info("shutdown_requested")
	}

	shutdownCtx, cancel := context.WithTimeout(
		context.Background(),
		10*time.Second,
	)
	defer cancel()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		logger.Error("shutdown_failed", "error", err)
	}

	if err := <-done; err != nil {
		logger.Error("server_failed", "error", err)
	}

	logger.Info("api_stopped")
}
