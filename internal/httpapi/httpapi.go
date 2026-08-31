// SPDX-FileCopyrightText: 2026 ArgosFX contributors
// SPDX-License-Identifier: AGPL-3.0-or-later

// SPDX-FileCopyrightText: 2026 ArgosFX contributors
// SPDX-License-Identifier: AGPL-3.0-or-later

package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/macedot/ArgosFX/internal/aggregator"
	"github.com/macedot/ArgosFX/internal/config"
	"github.com/macedot/ArgosFX/internal/obs"
	"github.com/macedot/ArgosFX/internal/ratelookup"
	"github.com/macedot/ArgosFX/internal/store"
)

type Server struct {
	mux        *chi.Mux
	log        *slog.Logger
	agg        *aggregator.Aggregator
	store      *store.DB
	cache      *ratelookup.Cache
	currencies config.Currencies
	cacheTTL   time.Duration
	metrics    *obs.Metrics
	maxAge     time.Duration
}

type Options struct {
	Logger     *slog.Logger
	Aggregator *aggregator.Aggregator
	Store      *store.DB
	Cache      *ratelookup.Cache
	Currencies config.Currencies
	CacheTTL   time.Duration
	Metrics    *obs.Metrics
	MaxAge     time.Duration
}

func New(opts Options) *Server {
	if opts.Logger == nil {
		opts.Logger = slog.Default()
	}
	if opts.CacheTTL <= 0 {
		opts.CacheTTL = 60 * time.Second
	}
	s := &Server{
		mux:        chi.NewRouter(),
		log:        opts.Logger,
		agg:        opts.Aggregator,
		store:      opts.Store,
		cache:      opts.Cache,
		currencies: opts.Currencies,
		cacheTTL:   opts.CacheTTL,
		metrics:    opts.Metrics,
		maxAge:     opts.MaxAge,
	}
	s.routes()
	return s
}

func (s *Server) Handler() http.Handler { return s.mux }

func (s *Server) routes() {
	s.mux.Get("/v1/healthz", s.healthz)
	s.mux.Get("/v1/readyz", s.readyz)
	s.mux.Get("/v1/metrics", s.metricsHandler)
	s.mux.Get("/v1/rates", s.allRates)
	s.mux.Get("/v1/rates/{base}", s.ratesFromBase)
	s.mux.Get("/v1/rates/{base}/{quote}", s.singleRate)
	s.mux.Get("/v1/rates/{base}/{quote}/history", s.historyHandler)
	s.mux.Get("/v1/providers", s.handleProviders)
}

func (s *Server) readyz(w http.ResponseWriter, r *http.Request) {
	if s.store == nil {
		writeError(w, http.StatusServiceUnavailable, "store not configured")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()
	if s.maxAge > 0 {
		latest, ok, err := s.store.LatestReadingAt(ctx)
		if err != nil {
			writeError(w, http.StatusServiceUnavailable, "store read failed")
			return
		}
		if !ok {
			writeError(w, http.StatusServiceUnavailable, "no readings yet")
			return
		}
		if time.Since(latest) > s.maxAge {
			writeError(w, http.StatusServiceUnavailable,
				fmt.Sprintf("newest reading is %s old (limit %s)", time.Since(latest).Round(time.Second), s.maxAge))
			return
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"status": "ready",
		"time":   time.Now().UTC().Format(time.RFC3339),
	})
}

func (s *Server) metricsHandler(w http.ResponseWriter, r *http.Request) {
	if s.metrics == nil {
		http.Error(w, "metrics not configured", http.StatusServiceUnavailable)
		return
	}
	s.refreshProviderMetrics(r.Context())
	w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
	_, _ = w.Write([]byte(s.metrics.Text()))
}

func (s *Server) refreshProviderMetrics(ctx context.Context) {
	if s.store == nil || s.metrics == nil {
		return
	}
	providers, err := s.store.ListProviders(ctx)
	if err != nil {
		return
	}
	now := time.Now().UTC()
	s.metrics.SetGauge("argosfx_providers_total", float64(len(providers)))
	enabled := 0
	for _, p := range providers {
		if p.Enabled {
			enabled++
		}
		s.metrics.SetLabeled("argosfx_budget_used_today", quote(p.Name), float64(0))
		used, _ := s.store.UsageToday(ctx, p.ID, now)
		s.metrics.SetLabeled("argosfx_budget_used_today", quote(p.Name), float64(used))
		if p.CallsPerDay != nil {
			s.metrics.SetLabeled("argosfx_budget_limit", quote(p.Name), float64(*p.CallsPerDay))
		}
	}
	s.metrics.SetGauge("argosfx_providers_enabled", float64(enabled))
	if s.cache != nil {
		s.metrics.SetGauge("argosfx_cache_size", float64(s.cache.Len()))
	}
}

func quote(s string) string {
	return fmt.Sprintf("provider=%q", s)
}

type providerView struct {
	Name        string `json:"name"`
	Type        string `json:"type"`
	Priority    int    `json:"priority"`
	Enabled     bool   `json:"enabled"`
	CallsPerDay *int   `json:"calls_per_day,omitempty"`
	Schedule    string `json:"schedule,omitempty"`
	UsedToday   int    `json:"used_today"`
}

func (s *Server) handleProviders(w http.ResponseWriter, r *http.Request) {
	if s.store == nil {
		writeError(w, http.StatusServiceUnavailable, "store not configured")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()
	providers, err := s.store.ListProviders(ctx)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	out := make([]providerView, 0, len(providers))
	now := time.Now().UTC()
	for _, p := range providers {
		used, _ := s.store.UsageToday(ctx, p.ID, now)
		out = append(out, providerView{
			Name:        p.Name,
			Type:        p.Type,
			Priority:    p.Priority,
			Enabled:     p.Enabled,
			CallsPerDay: p.CallsPerDay,
			Schedule:    p.ScheduleCron,
			UsedToday:   used,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"providers": out})
}

func (s *Server) healthz(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"status": "ok",
		"time":   time.Now().UTC().Format(time.RFC3339),
	})
}

type ratesResponse struct {
	Base             string             `json:"base"`
	AsOf             string             `json:"as_of"`
	FreshnessSeconds float64            `json:"freshness_seconds"`
	ProvidersUsed    []string           `json:"providers_used"`
	Rates            map[string]float64 `json:"rates"`
}

func (s *Server) allRates(w http.ResponseWriter, r *http.Request) {
	if s.agg == nil {
		writeError(w, http.StatusServiceUnavailable, "aggregator not configured")
		return
	}
	codes := s.allowedCodes()
	if len(codes) == 0 {
		writeJSON(w, http.StatusOK, ratesResponse{
			Base: "USD", AsOf: time.Now().UTC().Format(time.RFC3339),
			Rates: map[string]float64{},
		})
		return
	}
	cacheKey := "USD:*"
	if body, ok := s.cacheLookup(cacheKey); ok {
		writeBytes(w, body)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	results, err := s.agg.AllCurrencies(ctx, codes)
	if err != nil {
		s.log.Warn("all currencies", "err", err)
	}
	rates := map[string]float64{}
	var freshness float64
	providers := map[string]struct{}{}
	for code, res := range results {
		rates[code] = res.Rate
		if res.FreshnessSeconds > freshness {
			freshness = res.FreshnessSeconds
		}
		for _, p := range res.ProvidersUsed {
			providers[p] = struct{}{}
		}
	}
	resp := ratesResponse{
		Base:             "USD",
		AsOf:             time.Now().UTC().Format(time.RFC3339),
		FreshnessSeconds: freshness,
		ProvidersUsed:    sortedKeys(providers),
		Rates:            rates,
	}
	body := s.cacheStore(cacheKey, resp)
	w.Header().Set("Cache-Control", fmt.Sprintf("max-age=%d", int(s.cacheTTL.Seconds())))
	writeBytes(w, body)
}

func (s *Server) ratesFromBase(w http.ResponseWriter, r *http.Request) {
	base := strings.ToUpper(chi.URLParam(r, "base"))
	if !s.isAllowed(base) {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("currency %q not allowed", base))
		return
	}
	if s.agg == nil {
		writeError(w, http.StatusServiceUnavailable, "aggregator not configured")
		return
	}
	codes := s.allowedCodes()
	out := map[string]float64{}
	providers := map[string]struct{}{}
	var freshness float64
	for _, code := range codes {
		if code == base {
			out[code] = 1.0
			continue
		}
		cacheKey := fmt.Sprintf("%s->%s", base, code)
		if body, ok := s.cacheLookup(cacheKey); ok {
			var v singleRateResponse
			if err := json.Unmarshal(body, &v); err == nil {
				out[code] = v.Rate
				if v.FreshnessSeconds > freshness {
					freshness = v.FreshnessSeconds
				}
				for _, p := range v.ProvidersUsed {
					providers[p] = struct{}{}
				}
				continue
			}
		}
		ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
		res, err := s.agg.Compute(ctx, base, code)
		cancel()
		if err != nil {
			continue
		}
		out[code] = res.Rate
		if res.FreshnessSeconds > freshness {
			freshness = res.FreshnessSeconds
		}
		for _, p := range res.ProvidersUsed {
			providers[p] = struct{}{}
		}
		_ = s.cacheStore(cacheKey, singleRateResponse{
			From:             res.From,
			To:               res.To,
			Rate:             res.Rate,
			AsOf:             time.Now().UTC().Format(time.RFC3339),
			FreshnessSeconds: res.FreshnessSeconds,
			ProvidersUsed:    res.ProvidersUsed,
		})
	}
	resp := ratesResponse{
		Base:             base,
		AsOf:             time.Now().UTC().Format(time.RFC3339),
		FreshnessSeconds: freshness,
		ProvidersUsed:    sortedKeys(providers),
		Rates:            out,
	}
	w.Header().Set("Cache-Control", fmt.Sprintf("max-age=%d", int(s.cacheTTL.Seconds())))
	writeJSON(w, http.StatusOK, resp)
}

type singleRateResponse struct {
	From             string   `json:"from"`
	To               string   `json:"to"`
	Rate             float64  `json:"rate"`
	AsOf             string   `json:"as_of"`
	FreshnessSeconds float64  `json:"freshness_seconds"`
	ProvidersUsed    []string `json:"providers_used"`
}

func (s *Server) singleRate(w http.ResponseWriter, r *http.Request) {
	from := strings.ToUpper(chi.URLParam(r, "base"))
	to := strings.ToUpper(chi.URLParam(r, "quote"))
	if !s.isAllowed(from) || !s.isAllowed(to) {
		writeError(w, http.StatusBadRequest, "currency not allowed")
		return
	}
	if s.agg == nil {
		writeError(w, http.StatusServiceUnavailable, "aggregator not configured")
		return
	}
	cacheKey := fmt.Sprintf("%s->%s", from, to)
	if body, ok := s.cacheLookup(cacheKey); ok {
		w.Header().Set("Cache-Control", fmt.Sprintf("max-age=%d", int(s.cacheTTL.Seconds())))
		writeBytes(w, body)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	res, err := s.agg.Compute(ctx, from, to)
	if err != nil {
		if errors.Is(err, aggregator.ErrInsufficientProviders) {
			writeError(w, http.StatusServiceUnavailable, err.Error())
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	resp := singleRateResponse{
		From:             res.From,
		To:               res.To,
		Rate:             res.Rate,
		AsOf:             time.Now().UTC().Format(time.RFC3339),
		FreshnessSeconds: res.FreshnessSeconds,
		ProvidersUsed:    res.ProvidersUsed,
	}
	body := s.cacheStore(cacheKey, resp)
	w.Header().Set("Cache-Control", fmt.Sprintf("max-age=%d", int(s.cacheTTL.Seconds())))
	writeBytes(w, body)
}

func (s *Server) allowedCodes() []string {
	if len(s.currencies.Allowed) == 0 {
		return nil
	}
	out := make([]string, 0, len(s.currencies.Allowed))
	for c := range s.currencies.Allowed {
		out = append(out, c)
	}
	return out
}

func (s *Server) isAllowed(code string) bool {
	if _, ok := s.currencies.Allowed[code]; ok {
		return true
	}
	return false
}

type historyResponse struct {
	From    string                       `json:"from"`
	To      string                       `json:"to"`
	Start   string                       `json:"start"`
	End     string                       `json:"end"`
	Step    string                       `json:"step"`
	Points  []aggregator.HistoryPoint    `json:"points"`
}

func (s *Server) historyHandler(w http.ResponseWriter, r *http.Request) {
	if s.agg == nil {
		writeError(w, http.StatusServiceUnavailable, "aggregator not configured")
		return
	}
	from := strings.ToUpper(chi.URLParam(r, "base"))
	to := strings.ToUpper(chi.URLParam(r, "quote"))
	if !s.isAllowed(from) || !s.isAllowed(to) {
		writeError(w, http.StatusBadRequest, "currency not allowed")
		return
	}
	q := r.URL.Query()
	end := time.Now().UTC()
	start := end.Add(-24 * time.Hour)
	if v := q.Get("end"); v != "" {
		if t, err := time.Parse(time.RFC3339, v); err == nil {
			end = t.UTC()
		} else {
			writeError(w, http.StatusBadRequest, "end must be RFC3339")
			return
		}
	}
	if v := q.Get("start"); v != "" {
		if t, err := time.Parse(time.RFC3339, v); err == nil {
			start = t.UTC()
		} else {
			writeError(w, http.StatusBadRequest, "start must be RFC3339")
			return
		}
	}
	step := time.Hour
	if v := q.Get("step"); v != "" {
		d, err := time.ParseDuration(v)
		if err != nil {
			writeError(w, http.StatusBadRequest, "step must be a Go duration (e.g. 1m, 1h, 24h)")
			return
		}
		if d < time.Minute || d > 24*time.Hour {
			writeError(w, http.StatusBadRequest, "step must be between 1m and 24h")
			return
		}
		step = d
	}
	cacheKey := fmt.Sprintf("hist:%s:%s:%s:%s:%s", from, to,
		start.Format(time.RFC3339), end.Format(time.RFC3339), step)
	if body, ok := s.cacheLookup(cacheKey); ok {
		w.Header().Set("Cache-Control", fmt.Sprintf("max-age=%d", int(s.cacheTTL.Seconds())))
		writeBytes(w, body)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	pts, err := s.agg.History(ctx, from, to, start, end, step)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if pts == nil {
		pts = []aggregator.HistoryPoint{}
	}
	resp := historyResponse{
		From: from, To: to,
		Start: start.Format(time.RFC3339),
		End:   end.Format(time.RFC3339),
		Step:  step.String(),
		Points: pts,
	}
	body := s.cacheStore(cacheKey, resp)
	w.Header().Set("Cache-Control", fmt.Sprintf("max-age=%d", int(s.cacheTTL.Seconds())))
	writeBytes(w, body)
}

func (s *Server) cacheLookup(key string) ([]byte, bool) {
	if s.cache == nil {
		return nil, false
	}
	return s.cache.Get(key)
}

func (s *Server) cacheStore(key string, v any) []byte {
	body, err := json.Marshal(v)
	if err != nil {
		return body
	}
	if s.cache != nil {
		s.cache.Set(key, body)
	}
	return body
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func writeBytes(w http.ResponseWriter, body []byte) {
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(body)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

func sortedKeys(m map[string]struct{}) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	for i := 0; i < len(out); i++ {
		for j := i + 1; j < len(out); j++ {
			if out[j] < out[i] {
				out[i], out[j] = out[j], out[i]
			}
		}
	}
	return out
}
