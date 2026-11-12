# ethkit-gowallet

[![Go CI](https://github.com/Mutwiri-Mwenda/ethkit-gowallet/actions/workflows/go-ci.yml/badge.svg)](https://github.com/Mutwiri-Mwenda/ethkit-gowallet/actions/workflows/go-ci.yml)

A Go SDK and HTTP API for reading Ethereum account balances, built around upstream reliability: caching, request coalescing, rate and concurrency limits, retries, and a circuit breaker.

> **Work in progress.** The API is read-only (no wallet creation, key custody, signing, or transfers) and currently fixed to Ethereum Sepolia (chain ID `11155111`).

## Features

- Native ETH balance lookup via Etherscan API V2, returned as a precise `math/big.Int` (wei)
- In-process LRU cache (5s TTL, 1,000 entries)
- Request coalescing for simultaneous lookups of the same chain/address; abandoned work is cancelled
- Upstream limits: 10 concurrent, 20 queued, 5 attempts/second (retries count)
- Retries on Etherscan rate-limit errors only: up to 3 attempts, exponential backoff with full jitter
- Circuit breaker: 3 qualifying failures to open, 10s cooldown, one half-open probe
- Request IDs, structured JSON logs, JSON metrics, timeouts, graceful shutdown
- Separate SDK packages for Etherscan and JSON-RPC (the RPC client is not wired into the API)

All limits are hard-coded in `apps/api/cmd/server/main.go` and the balance-service defaults. They are not configurable by environment variable yet.

## Layout

```
ethkit-gowallet/
├── apps/api/                    # HTTP API (separate Go module)
│   ├── cmd/server/              # Entry point
│   └── internal/
│       ├── balance/             # Cache, coalescing, limits, retries, circuit breaker
│       ├── handler/             # Routes and error mapping
│       ├── observability/       # Logs, request IDs, metrics
│       └── server/              # Server configuration
├── sdk/                         # SDK (separate Go module)
│   ├── chain/evm/               # Chain ID and balance operations
│   ├── explorer/etherscan/      # Etherscan API V2 client
│   ├── rpc/                     # JSON-RPC transport
│   └── cmd/etherscan/           # Example CLI
└── .github/workflows/go-ci.yml
```

`apps/api/go.mod` uses a local `replace` pointing at `../../sdk`, so keep this layout when building the API.

## Quick start

Requires Go 1.24.4 and an Etherscan API key.

```bash
export ETHERSCAN_API_KEY="your-etherscan-api-key"
cd apps/api
go run ./cmd/server
```

The server listens on `127.0.0.1:8080` and exits at startup if the key is missing or invalid. Never commit API keys.

## API

| Method | Path | Description |
|--------|------|-------------|
| GET | `/api/v1/health` | Process health only; does not check upstream |
| GET | `/api/v1/chain` | Configured chain metadata |
| GET | `/api/v1/wallets/{address}/balance` | Native ETH balance in wei |
| GET | `/metrics` | JSON HTTP and balance-service metrics |

```bash
curl http://127.0.0.1:8080/api/v1/wallets/0x0000000000000000000000000000000000000000/balance
```

```json
{
  "address": "0x0000000000000000000000000000000000000000",
  "chain_id": 11155111,
  "balance_wei": "1000000000000000000"
}
```

`address` must be `0x` plus 40 hex characters. `balance_wei` is a decimal string (1 ETH = 10^18 wei). All responses send `Cache-Control: no-store`.

`/metrics` returns JSON, not Prometheus text format. It covers HTTP counts and latency, cache hits/misses/evictions, coalesced requests, upstream attempts/errors, limiter and rate-limiter stats, and circuit-breaker state.

### Errors

```json
{"error":{"code":"invalid_address","message":"Bad Request"}}
```

| Status | Code | Meaning |
|--------|------|---------|
| 400 | `invalid_address` | Invalid address syntax |
| 502 | `provider_failure` | Other upstream failure |
| 502 | `invalid_provider_balance` | Invalid balance returned by upstream |
| 503 | `upstream_overloaded` | Concurrency or queue capacity exceeded (`Retry-After: 1`) |
| 503 | `provider_rate_limited` | Etherscan rate limit hit |
| 503 | `upstream_circuit_open` | Circuit breaker rejected the request (`Retry-After: 10`) |
| 504 | `provider_timeout` | Upstream deadline exceeded |

## SDK

```go
client, err := etherscan.NewClient(os.Getenv("ETHERSCAN_API_KEY"))
if err != nil {
    panic(err)
}

wei, err := client.GetBalance(
    context.Background(),
    11155111,
    "0x0000000000000000000000000000000000000000",
)
if err != nil {
    panic(err)
}
fmt.Println(wei.String())
```

Import path: `github.com/Mutwiri-Mwenda/gowallet/sdk/explorer/etherscan`

To run the example CLI:

```bash
export ETHERSCAN_API_KEY="your-etherscan-api-key"
export ETH_WALLET_ADDRESS="0x0000000000000000000000000000000000000000"
cd sdk
go run ./cmd/etherscan
```

## Testing

```bash
(cd sdk && go test -count=1 -race ./... && go vet ./... && go build ./...)
(cd apps/api && go test -count=1 -race ./... && go vet ./... && go build ./...)
```

Formatting check:

```bash
(cd sdk && test -z "$(gofmt -l .)")
(cd apps/api && test -z "$(gofmt -l .)")
```

CI runs formatting, race-enabled tests, vet, and build for both modules on pushes and pull requests to `main`.

## Limitations

- Bound to localhost and fixed to Sepolia
- Cache and limits are per process, not shared across replicas
- `/metrics` is unauthenticated; do not expose it publicly without access controls
- `/api/v1/health` is not an upstream readiness check
- No custody, signing, transfers, or token balances
- Frontend (Next.js/TypeScript) is planned, not started
- Not production-hardened; no security review yet