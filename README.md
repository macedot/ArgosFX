<h1 align="center">ArgosFX</h1>

<p align="center"><strong>Server-side FX rate reporting with multi-provider aggregation</strong></p>

<p align="center">
  <a href="LICENSE"><img src="https://img.shields.io/badge/license-AGPL--3.0--or--later-blue" alt="License: AGPL-3.0-or-later" /></a>
  <a href="https://github.com/macedot/ArgosFX/actions/workflows/ci.yml"><img src="https://img.shields.io/github/actions/workflow/status/macedot/ArgosFX/ci.yml?branch=main&label=ci" alt="CI" /></a>
  <img src="https://img.shields.io/badge/Go-1.27-00ADD8?logo=go&logoColor=white" alt="Go" />
  <img src="https://img.shields.io/badge/SQLite-WAL-003B57?logo=sqlite&logoColor=white" alt="SQLite" />
  <img src="https://img.shields.io/badge/Docker-Compose-2496ED?logo=docker&logoColor=white" alt="Docker" />
  <img src="https://img.shields.io/badge/Caddy-2-1F88C0?logo=caddy&logoColor=white" alt="Caddy" />
</p>

---

**ArgosFX** is a self-hosted FX rate reporting backend. It polls configured 3rd-party FX providers on a schedule, stores every reading in SQLite, aggregates them with a median across providers, and serves computed rates over a small HTTP API. Sits behind Caddy for rate limiting and response caching. Internal use today; future versions add webhooks, rate-move alerts, and an admin UI.

## Features

- **Polling** — one goroutine per provider, scheduled by cron or auto-spaced from a daily call budget (soft budget: skip + warn when exhausted)
- **Storage** — every reading persisted in SQLite (WAL mode, pure-Go driver via `modernc.org/sqlite`)
- **Aggregation** — median across providers, outlier filter, cross rates computed via USD bridge
- **HTTP cache** — in-process compute cache (default 60s) + `Cache-Control` header for downstream Caddy
- **Decoupled I/O** — HTTP requests never trigger upstream calls; polling is fully independent
- **Providers** — Frankfurter (no key, ECB), MoneyConvert (no key), Yahoo Finance (unofficial, best-effort)

## Quick Start

```bash
cd deploy && docker compose up --build
```

Open [http://localhost:8080](http://localhost:8080).

### Pre-built images (from GitHub Container Registry)

```bash
GHCR_OWNER=macedot IMAGE_TAG=latest docker compose up
```

### Local development (no Docker)

```bash
ARGOSFX_CURRENCIES=EUR,BRL,GBP,JPY \
ARGOSFX_CONFIG_PATH=./deploy/config.example.yaml \
ARGOSFX_DB_PATH=./tmp/argosfx.db \
go run ./cmd/argosfx
```

## Configuration

| Var | Default | Description |
| --- | --- | --- |
| `ARGOSFX_CURRENCIES` | (required) | Comma-separated whitelist; `USD` is always added implicitly |
| `ARGOSFX_CONFIG_PATH` | `/etc/argosfx/config.yaml` | Path to YAML config |
| `ARGOSFX_DB_PATH` | `/data/argosfx.db` | SQLite file location |
| `ARGOSFX_LOG_LEVEL` | `info` | `debug` / `info` / `warn` / `error` |

The YAML config (`deploy/config.example.yaml`) defines the server listen address, cache TTLs, aggregator tolerances, and the provider list (type, `calls_per_day`, `priority`, `enabled`).

Example provider entry:

```yaml
providers:
  - name: frankfurter
    type: frankfurter
    calls_per_day: 1000
    priority: 1
    enabled: true
  - name: yahoo
    type: yahoo
    calls_per_day: 2000
    priority: 3
    enabled: true
```

## API

| Method | Path | Description |
| --- | --- | --- |
| `GET` | `/v1/healthz` | Liveness |
| `GET` | `/v1/readyz` | Readiness (DB + at least one fresh reading) |
| `GET` | `/v1/rates` | All configured currencies vs USD |
| `GET` | `/v1/rates/{base}` | All quotes from `{base}` |
| `GET` | `/v1/rates/{base}/{quote}` | Any pair, computed via USD bridge |
| `GET` | `/v1/rates/{base}/{quote}/history` | Historical series (`?start=&end=&step=`) |
| `GET` | `/v1/providers` | Configured providers + today's usage |

Example:

```sh
$ curl -s localhost:8080/v1/rates | jq
{
  "base": "USD",
  "as_of": "2026-08-31T11:42:00Z",
  "freshness_seconds": 47,
  "providers_used": ["frankfurter", "yahoo"],
  "rates": { "EUR": 0.9213, "BRL": 5.4821 }
}
```

History example:

```sh
$ curl -s 'localhost:8080/v1/rates/USD/EUR/history?step=1h' | jq '.points | length'
```

## Project layout

```
cmd/argosfx/                entrypoint
internal/config/            YAML + env loading
internal/store/             SQLite (modernc.org/sqlite, pure Go)
internal/provider/          Provider interface
internal/provider/adapters/ frankfurter, moneyconvert, yahoo
internal/aggregator/        median, outlier filter, cross-rate, history
internal/scheduler/         cron + soft budget
internal/ratelookup/        in-process compute cache
internal/httpapi/           chi handlers
deploy/                     docker-compose, Caddyfile, config.example.yaml
docs/                       FUTURE.md and other docs
```

## Development

### Prerequisites

- Go 1.27+
- (optional) Docker + Docker Compose v2 for the container path
- (optional) `xcaddy` if you want to rebuild the Caddy image with custom plugins

### Local development

```bash
go build ./...
go test ./...
```

### Testing

```bash
go test ./...                  # all tests
go test -race ./internal/...   # race detector
go test -cover ./...           # coverage
```

## Architecture

```
                     ┌──────────────────────────────────────────┐
                     │ Caddy (edge)                              │
                     │  - rate_limit (per-IP)                    │
                     │  - response cache (Cache-Control from app) │
                     │  - gzip, security headers                  │
                     │  - reverse_proxy → argosfx:8080            │
                     └──────────────┬───────────────────────────┘
                                    │
                     ┌──────────────▼───────────────────────────┐
                     │ argosfx (Go)                              │
                     │  ┌────────────────────────────────────┐  │
                     │  │ HTTP layer (chi)                   │  │
                     │  │   GET /v1/healthz, /readyz         │  │
                     │  │   GET /v1/rates[, /{b}, /{b}/{q}]  │  │
                     │  │   GET /v1/rates/{b}/{q}/history    │  │
                     │  │   GET /v1/providers                │  │
                     │  └─────┬──────────────────────────────┘  │
                     │        │                                  │
                     │  ┌─────▼──────────────────────────────┐  │
                     │  │ Compute-rate cache (in-proc LRU)   │  │
                     │  │  key: (from,to)  TTL: cfg (60s)    │  │
                     │  └─────┬──────────────────────────────┘  │
                     │        │                                  │
                     │  ┌─────▼──────────────────────────────┐  │
                     │  │ Aggregator                          │  │
                     │  │   - read latest readings from DB    │  │
                     │  │   - drop outliers (>N% off median)  │  │
                     │  │   - cross-rate via USD              │  │
                     │  │   - median across providers         │  │
                     │  └─────┬──────────────────────────────┘  │
                     │        │                                  │
                     │  ┌─────▼──────────────────────────────┐  │
                     │  │ SQLite store                        │  │
                     │  │   providers, readings, usage        │  │
                     │  └─────────────────────────────────────┘ │
                     │                                           │
                     │  ┌────────────────────────────────────┐  │
                     │  │ Scheduler                          │  │
                     │  │   - one goroutine per provider     │  │
                     │  │   - cron or auto-spaced budget    │  │
                     │  │   - budget check before fetch      │  │
                     │  │   - on success: write readings     │  │
                     │  │   - on failure: log + keep going   │  │
                     │  └────────────────────────────────────┘  │
                     │                                           │
                     │  ┌────────────────────────────────────┐  │
                     │  │ Provider adapters                  │  │
                     │  │   - frankfurter                    │  │
                     │  │   - moneyconvert                   │  │
                     │  │   - yahoo                          │  │
                     │  └────────────────────────────────────┘  │
                     └───────────────────────────────────────────┘
```

**How it works:**

1. **Polling** — on startup, one goroutine per provider is registered in cron. Each tick: budget check → fetch from upstream → INSERT readings → bump daily usage counter. Skips + warns when the daily budget is exhausted.
2. **Read-only requests** — HTTP handlers never fetch upstream. They read `readings` from SQLite, compute a median across providers for the requested currency, drop outliers >N% off the median, and return the result. Cross rates are computed via USD bridge.
3. **Two cache layers** — an in-process LRU keyed `(from, to)` with default 60s TTL absorbs repeated requests; Caddy's response cache at the edge serves hot keys with stale-while-revalidate.
4. **Persistence** — every reading is retained in SQLite. A retention job prunes rows older than `retention_days` (default 365).

## Deployment

| Service | Base Image | Notes |
| --- | --- | --- |
| `argosfx` | `gcr.io/distroless/static-debian12:nonroot` | Static Go binary, non-root, multi-stage build, no shell |
| `argosfx-caddy` | `caddy:2` + `cache-handler` + `ratelimit` | Reverse proxy, response cache, rate limit, gzip |

Images publish to `ghcr.io/macedot/argosfx` and `ghcr.io/macedot/argosfx-caddy` on tagged releases.

### Security

- **Static binary** — no interpreter, no dynamic loader on the Go side
- **Distroless base** — minimal attack surface, no shell, no package manager in the argosfx container
- **Non-root user** — runs as `nonroot` (uid 65532) inside the argosfx container
- **Caddy edge** — per-IP rate limit, response cache with stale-while-revalidate, gzip, security headers
- **No upstream credentials stored today** — all providers are keyless; if you add a keyed provider, use env vars (`ARGOSFX_*_API_KEY`), never commit them
- **Internal-only API** — no auth on `/v1/*` today; restrict at the network layer (Caddy rate limit + firewall)

## CI/CD

GitHub Actions runs on every push and PR to `main`:

- **Test** — `go vet`, `go build`, `go test -race -coverprofile`
- **Lint** — `golangci-lint` with default rules
- **Docker** — verifies both Dockerfiles build cleanly (no push)

Dependabot opens weekly PRs for Go modules and GitHub Actions. Container publishing to GHCR is wired up as a separate release workflow (not yet enabled in this repo).

## Roadmap

Deferred features are tracked in [`docs/FUTURE.md`](./docs/FUTURE.md): webhooks, rate-move alerts, admin UI, bearer-token auth, multi-base triangulation, volatility analytics, `/metrics`, and a Postgres migration path.

## License

This project is licensed under the [GNU Affero General Public License v3.0 or later](LICENSE) (SPDX: `AGPL-3.0-or-later`). See [`LICENSE`](./LICENSE) for the full text.
