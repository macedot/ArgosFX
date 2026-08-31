// SPDX-FileCopyrightText: 2026 ArgosFX contributors
// SPDX-License-Identifier: AGPL-3.0-or-later

// SPDX-FileCopyrightText: 2026 ArgosFX contributors
// SPDX-License-Identifier: AGPL-3.0-or-later

package aggregator

import (
	"context"
	"errors"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/macedot/ArgosFX/internal/store"
)

type Config struct {
	OutlierTolerancePct float64
	MinProviders         int
	MaxAgeSeconds        int
	Base                 string // typically "USD"
}

type Source struct {
	ProviderID   int64     `json:"-"`
	ProviderName string    `json:"provider"`
	Rate         float64   `json:"rate"`
	FetchedAt    time.Time `json:"fetched_at"`
	ProviderTS   string    `json:"provider_ts,omitempty"`
}

type Result struct {
	From             string             `json:"from"`
	To               string             `json:"to"`
	Rate             float64            `json:"rate"`
	FreshnessSeconds float64            `json:"freshness_seconds"`
	ProvidersUsed    []string           `json:"providers_used"`
	Sources          map[string]Source  `json:"sources"`
	MedianFrom       float64            `json:"median_from"`
	MedianTo         float64            `json:"median_to"`
	ComputedAt       time.Time          `json:"computed_at"`
}

var ErrNoData = errors.New("no data available")
var ErrInsufficientProviders = errors.New("insufficient providers")

type Aggregator struct {
	db  *store.DB
	cfg Config
}

func New(db *store.DB, cfg Config) *Aggregator {
	if cfg.Base == "" {
		cfg.Base = "USD"
	}
	return &Aggregator{db: db, cfg: cfg}
}

func (a *Aggregator) Compute(ctx context.Context, from, to string) (Result, error) {
	from = strings.ToUpper(from)
	to = strings.ToUpper(to)
	if from == "" || to == "" {
		return Result{}, fmt.Errorf("from and to are required")
	}
	if from == to {
		return Result{
			From: from, To: to, Rate: 1.0,
			ComputedAt: time.Now().UTC(),
			Sources:    map[string]Source{},
		}, nil
	}

	since := time.Now().UTC().Add(-time.Duration(a.cfg.MaxAgeSeconds) * time.Second)
	switch {
	case from == a.cfg.Base:
		return a.computeDirect(ctx, to, since)
	case to == a.cfg.Base:
		r, err := a.computeDirect(ctx, from, since)
		if err != nil {
			return Result{}, err
		}
		return r.invertTo(to)
	default:
		return a.computeCross(ctx, from, to, since)
	}
}

func (a *Aggregator) computeCross(ctx context.Context, from, to string, since time.Time) (Result, error) {
	fromUSD, err := a.computeDirect(ctx, from, since)
	if err != nil {
		return Result{}, fmt.Errorf("from-side: %w", err)
	}
	toUSD, err := a.computeDirect(ctx, to, since)
	if err != nil {
		return Result{}, fmt.Errorf("to-side: %w", err)
	}
	if fromUSD.Rate == 0 {
		return Result{}, fmt.Errorf("from-side rate is zero")
	}
	cross := toUSD.Rate / fromUSD.Rate
	mergedSources := map[string]Source{}
	for k, v := range fromUSD.Sources {
		mergedSources[k+"→"+from] = v
	}
	for k, v := range toUSD.Sources {
		mergedSources[k+"→"+to] = v
	}
	providersUsed := mergeUnique(fromUSD.ProvidersUsed, toUSD.ProvidersUsed)
	freshness := fromUSD.ComputedAt
	if toUSD.ComputedAt.Before(freshness) {
		freshness = toUSD.ComputedAt
	}
	return Result{
		From:             from,
		To:               to,
		Rate:             cross,
		FreshnessSeconds: time.Since(freshness).Seconds(),
		ProvidersUsed:    providersUsed,
		Sources:          mergedSources,
		MedianFrom:       fromUSD.Rate,
		MedianTo:         toUSD.Rate,
		ComputedAt:       time.Now().UTC(),
	}, nil
}

func mergeUnique(a, b []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(a)+len(b))
	for _, s := range a {
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	for _, s := range b {
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	sort.Strings(out)
	return out
}

type HistoryPoint struct {
	FetchedAt time.Time `json:"fetched_at"`
	Rate      float64   `json:"rate"`
}

func (a *Aggregator) History(ctx context.Context, from, to string, start, end time.Time, step time.Duration) ([]HistoryPoint, error) {
	from = strings.ToUpper(from)
	to = strings.ToUpper(to)
	if from == to {
		return nil, fmt.Errorf("from and to must differ")
	}
	switch {
	case from == a.cfg.Base:
		return a.historySide(ctx, to, start, end, step), nil
	case to == a.cfg.Base:
		pts := a.historySide(ctx, from, start, end, step)
		return invertHistory(pts), nil
	default:
		fromPts := a.historySide(ctx, from, start, end, step)
		toPts := a.historySide(ctx, to, start, end, step)
		return crossHistory(fromPts, toPts), nil
	}
}

func (a *Aggregator) historySide(ctx context.Context, quote string, start, end time.Time, step time.Duration) []HistoryPoint {
	raw, err := a.db.History(ctx, quote, start, end, step)
	if err != nil {
		return nil
	}
	out := make([]HistoryPoint, len(raw))
	for i, p := range raw {
		out[i] = HistoryPoint{FetchedAt: p.FetchedAt, Rate: p.Rate}
	}
	return out
}

func invertHistory(in []HistoryPoint) []HistoryPoint {
	out := make([]HistoryPoint, len(in))
	for i, p := range in {
		if p.Rate != 0 {
			out[i] = HistoryPoint{FetchedAt: p.FetchedAt, Rate: 1 / p.Rate}
		}
	}
	return out
}

func crossHistory(fromSide, toSide []HistoryPoint) []HistoryPoint {
	byT := make(map[int64]float64, len(fromSide))
	for _, p := range fromSide {
		byT[p.FetchedAt.Unix()] = p.Rate
	}
	out := make([]HistoryPoint, 0, len(toSide))
	for _, p := range toSide {
		f, ok := byT[p.FetchedAt.Unix()]
		if !ok || f == 0 || p.Rate == 0 {
			continue
		}
		out = append(out, HistoryPoint{FetchedAt: p.FetchedAt, Rate: p.Rate / f})
	}
	return out
}

func (a *Aggregator) Prune(ctx context.Context, retention time.Duration) (int64, error) {
	cutoff := time.Now().UTC().Add(-retention)
	return a.db.PruneOlderThan(ctx, cutoff)
}

func (a *Aggregator) computeDirect(ctx context.Context, quote string, since time.Time) (Result, error) {
	readings, providerIDs, err := a.db.LatestReadingsPerProvider(ctx, quote, since)
	if err != nil {
		return Result{}, fmt.Errorf("read latest readings: %w", err)
	}
	if len(readings) < a.cfg.MinProviders {
		return Result{}, fmt.Errorf("%w: have %d, need %d", ErrInsufficientProviders, len(readings), a.cfg.MinProviders)
	}
	providers, err := a.db.ListProviders(ctx)
	if err != nil {
		return Result{}, err
	}
	nameByID := map[int64]string{}
	for _, p := range providers {
		nameByID[p.ID] = p.Name
	}

	values := make([]float64, 0, len(readings))
	freshness := time.Time{}
	sources := map[string]Source{}
	for _, r := range readings {
		if r.FetchedAt.After(freshness) {
			freshness = r.FetchedAt
		}
		values = append(values, r.Rate)
		sources[nameByID[r.ProviderID]] = Source{
			ProviderID: r.ProviderID, ProviderName: nameByID[r.ProviderID],
			Rate: r.Rate, FetchedAt: r.FetchedAt, ProviderTS: r.ProviderTS,
		}
	}
	filtered := FilterOutliers(values, a.cfg.OutlierTolerancePct/100.0)
	med, _ := Median(filtered)
	providersUsed := make([]string, 0, len(filtered))
	for _, v := range filtered {
		idx := indexOf(values, v)
		if idx < 0 {
			continue
		}
		pid := providerIDs[idx]
		if name, ok := nameByID[pid]; ok {
			if !contains(providersUsed, name) {
				providersUsed = append(providersUsed, name)
			}
		}
	}
	sort.Strings(providersUsed)
	ageSec := 0.0
	if !freshness.IsZero() {
		ageSec = time.Since(freshness).Seconds()
	}
	return Result{
		From:             a.cfg.Base,
		To:               strings.ToUpper(quote),
		Rate:             med,
		FreshnessSeconds: ageSec,
		ProvidersUsed:    providersUsed,
		Sources:          sources,
		MedianFrom:       1.0,
		MedianTo:         med,
		ComputedAt:       time.Now().UTC(),
	}, nil
}

func (r Result) invertTo(to string) (Result, error) {
	if r.Rate == 0 {
		return r, errors.New("rate is zero, cannot invert")
	}
	newRate := 1.0 / r.Rate
	newSources := map[string]Source{}
	for k, s := range r.Sources {
		newSources[k] = Source{
			ProviderID: s.ProviderID, ProviderName: s.ProviderName,
			Rate: 1.0 / s.Rate, FetchedAt: s.FetchedAt, ProviderTS: s.ProviderTS,
		}
	}
	r.From = r.To
	r.To = strings.ToUpper(to)
	r.Rate = newRate
	r.MedianFrom = r.MedianTo
	r.MedianTo = newRate
	r.Sources = newSources
	return r, nil
}

func (a *Aggregator) AllCurrencies(ctx context.Context, codes []string) (map[string]Result, error) {
	since := time.Now().UTC().Add(-time.Duration(a.cfg.MaxAgeSeconds) * time.Second)
	out := map[string]Result{}
	for _, code := range codes {
		if code == a.cfg.Base {
			continue
		}
		r, err := a.computeDirect(ctx, code, since)
		if err != nil {
			continue
		}
		out[code] = r
	}
	return out, nil
}

func indexOf(s []float64, target float64) int {
	for i, v := range s {
		if math.Abs(v-target) < 1e-12 {
			return i
		}
	}
	return -1
}

func contains(s []string, v string) bool {
	for _, x := range s {
		if x == v {
			return true
		}
	}
	return false
}
