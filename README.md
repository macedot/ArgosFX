# ArgosFX

A small FX-rate reporting backend. Polls configured third-party providers on a
schedule, stores raw readings in SQLite, aggregates them with a median across
providers, and serves computed rates through a small HTTP API. Sits behind
Caddy for rate limiting and response caching.

## Features

- **Polling** — one goroutine per provider, scheduled by cron or auto-spaced
  from a daily call budget (soft budget: skip + warn when exhausted).
- **Storage** — every reading persisted in SQLite (WAL mode, pure-Go driver).
- **Aggregation** — median across providers, optional outlier filter, cross
  rates computed via USD bridge.
- **HTTP cache** — in-process compute cache (default 60s) + `Cache-Control`
  for downstream Caddy.
- **Providers** — Frankfurter (no key, ECB-backed). Yahoo Finance and
  MoneyConvert adapters land in a future phase.

## Quick start (local)

```sh
ARGOSFX_CURRENCIES=EUR,BRL,GBP,JPY \
ARGOSFX_CONFIG_PATH=./deploy/config.example.yaml \
ARGOSFX_DB_PATH=./tmp/argosfx.db \
go run ./cmd/argosfx
```

## Quick start (docker compose)

```sh
cd deploy && docker compose up --build
```

Caddy listens on `:80` and reverse-proxies to `argosfx:8080`.

## Configuration

`config.yaml`:

```yaml
server:
  listen: ":8080"

cache:
  compute_ttl_seconds: 60         # in-process rate cache
  http_cache_max_age_seconds: 60  # emitted Cache-Control

aggregator:
  outlier_tolerance_pct: 2.0
  min_providers: 1
  max_age_seconds: 86400          # Frankfurter is daily
  retention_days: 365

providers:
  - name: frankfurter
    type: frankfurter
    calls_per_day: 1000
    priority: 1
    enabled: true
```

Env vars:

| Var                       | Default                       |
|---------------------------|-------------------------------|
| `ARGOSFX_CURRENCIES`      | (required) comma-separated    |
| `ARGOSFX_CONFIG_PATH`     | `/etc/argosfx/config.yaml`    |
| `ARGOSFX_DB_PATH`         | `/data/argosfx.db`            |
| `ARGOSFX_LOG_LEVEL`       | `info`                        |

`ARGOSFX_CURRENCIES` is the whitelist; `USD` is always added implicitly.

## API

```
GET /v1/healthz                 liveness
GET /v1/rates                   all configured currencies vs USD
GET /v1/rates/{base}            all quotes from {base}
GET /v1/rates/{base}/{quote}    any pair (computed via USD bridge)
GET /v1/providers               configured providers + today's usage
```

Example:

```sh
$ curl -s localhost:8080/v1/rates | jq
{
  "base": "USD",
  "as_of": "2026-08-31T11:42:00Z",
  "freshness_seconds": 47,
  "providers_used": ["frankfurter"],
  "rates": { "EUR": 0.9213, "BRL": 5.4821 }
}
```

## Architecture

```
Caddy (edge)
 ├─ rate limit, response cache, gzip
 └─ reverse_proxy → argosfx:8080

argosfx (Go)
 ├─ HTTP layer (chi)            read-only path, never triggers providers
 ├─ Compute-rate cache (in-proc LRU)
 ├─ Aggregator                 median + outlier filter + cross-rate via USD
 ├─ SQLite store               providers, readings, usage
 ├─ Scheduler                  one goroutine per provider
 │    ├─ cron OR auto-spaced from calls_per_day
 │    ├─ budget check before fetch
 │    └─ writes readings + bumps usage on success
 └─ Provider adapters          frankfurter (more to come)
```

## Project layout

```
cmd/argosfx/         entrypoint
internal/config/     YAML + env loading
internal/store/      SQLite (modernc.org/sqlite, pure Go)
internal/provider/   Provider interface
internal/provider/adapters/   frankfurter (+ future)
internal/aggregator/  median, outlier filter, cross-rate
internal/scheduler/   cron + soft budget
internal/ratelookup/  in-process compute cache
internal/httpapi/     chi handlers
deploy/              docker-compose, Caddyfile, config.example.yaml
```

## Roadmap

See `docs/FUTURE.md` for the deferred features (webhooks, rate-move alerts,
admin UI, MoneyConvert / Yahoo adapters, history endpoint, /metrics, etc.).

## License

ArgosFX is licensed under the **GNU Affero General Public License v3.0 or
later** (AGPL-3.0-or-later). See [`LICENSE`](./LICENSE) for the full text.

SPDX-License-Identifier: `AGPL-3.0-or-later`
