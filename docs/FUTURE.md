# Roadmap (deferred features)

These were agreed to stay out of v1; tracked here so we don't lose them.

## Webhooks

Push notifications on fresh readings or rate moves >X%. Subscribers register
a URL; ArgosFX POSTs `{provider, quote, rate, as_of, sources}` on each
event. Auth via shared secret in URL or header.

## Rate-move alerts

Configurable thresholds per currency pair (`{base}/{quote} > N% in Y window`
→ fire webhook / email). Configurable globally in `config.yaml`.

## CSV / JSON streaming export

Bulk export of `readings` table for batch consumers. Accepts `from`, `to`,
`start`, `end` query params.

## Historical downsampling

`/v1/rates/{b}/{q}/history?step=1m|1h|1d` — currently raw, future versions
pre-aggregate buckets and serve from a downsampled view.

## Bearer-token auth

Internal-only today. Add `ARGOSFX_API_TOKEN` once we expose beyond the LAN.
Enforced in a Go middleware before cache.

## Admin UI

Single static HTML page served by Caddy: provider status, recent readings,
manual refresh button.

## Provider plugin SDK

Let users add their own adapters without forking. Currently adding a
provider = new file in `internal/provider/adapters/` + case in
`internal/provider/registry.go`.

## MoneyConvert + Yahoo adapters

In Phase 6 of the original plan. Both adapters are interface-only stubs;
once we confirm URL shapes with their free tier we'll fill them in.

## Multi-base triangulation

Currently USD-only bridge. Future versions allow other anchor bases
(`Compute(b, q)` would try `(USD→q)/(USD→b)` first, then try alternate
bridges).

## Volatility analytics

Rolling stats (1h, 24h, 7d), VaR-style metrics. Compute on demand from the
raw readings table.

## /readyz + /metrics

`/readyz` returns 200 only if at least one provider has a reading within
`max_age_seconds`. `/metrics` exposes Prometheus counters: provider fetch
latency, fetch errors by provider, cache hit ratio, budget used today.

## Migration path to Postgres

If write contention ever bites (SQLite's single writer), a swap-in
`store.DB` interface would let us port without changing the rest of the
codebase.
