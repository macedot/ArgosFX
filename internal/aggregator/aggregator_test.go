// SPDX-FileCopyrightText: 2026 ArgosFX contributors
// SPDX-License-Identifier: AGPL-3.0-or-later

// SPDX-FileCopyrightText: 2026 ArgosFX contributors
// SPDX-License-Identifier: AGPL-3.0-or-later

package aggregator

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/macedot/ArgosFX/internal/store"
)

func setup(t *testing.T) (*store.DB, *Aggregator) {
	t.Helper()
	db, err := store.Open(context.Background(), filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	calls := 1000
	_, _ = db.UpsertProvider(context.Background(), store.Provider{Name: "ff", Type: "frankfurter", Priority: 2, CallsPerDay: &calls, Enabled: true})
	_, _ = db.UpsertProvider(context.Background(), store.Provider{Name: "yahoo", Type: "yahoo", Priority: 1, CallsPerDay: &calls, Enabled: true})
	_, _ = db.UpsertProvider(context.Background(), store.Provider{Name: "mc", Type: "moneyconvert", Priority: 0, CallsPerDay: &calls, Enabled: true})
	return db, New(db, Config{OutlierTolerancePct: 2.0, MinProviders: 1, MaxAgeSeconds: 3600})
}

func insert(t *testing.T, db *store.DB, pid int64, quote string, rate float64, age time.Duration) {
	t.Helper()
	if err := db.InsertReading(context.Background(), store.Reading{
		ProviderID: pid, Base: "USD", Quote: quote, Rate: rate,
		FetchedAt: time.Now().UTC().Add(-age),
	}); err != nil {
		t.Fatal(err)
	}
}

func pidByName(t *testing.T, db *store.DB, name string) int64 {
	t.Helper()
	p, err := db.GetProviderByName(context.Background(), name)
	if err != nil {
		t.Fatal(err)
	}
	return p.ID
}

func TestCompute_USDToEUR_DirectMedian(t *testing.T) {
	db, a := setup(t)
	insert(t, db, pidByName(t, db, "ff"), "EUR", 0.92, 0)
	insert(t, db, pidByName(t, db, "yahoo"), "EUR", 0.93, time.Minute)
	res, err := a.Compute(context.Background(), "USD", "EUR")
	if err != nil {
		t.Fatal(err)
	}
	if res.Rate < 0.92 || res.Rate > 0.93 {
		t.Errorf("median rate: got %v", res.Rate)
	}
	if res.From != "USD" || res.To != "EUR" {
		t.Errorf("from/to: %v / %v", res.From, res.To)
	}
	if len(res.ProvidersUsed) != 2 {
		t.Errorf("providers used: %v", res.ProvidersUsed)
	}
}

func TestCompute_EURToUSD_Inverts(t *testing.T) {
	db, a := setup(t)
	insert(t, db, pidByName(t, db, "ff"), "EUR", 2.0, 0)
	res, err := a.Compute(context.Background(), "EUR", "USD")
	if err != nil {
		t.Fatal(err)
	}
	if mathAbs(res.Rate-0.5) > 1e-9 {
		t.Errorf("EUR→USD rate: got %v, want 0.5", res.Rate)
	}
}

func TestCompute_EURToBRL_CrossRate(t *testing.T) {
	db, a := setup(t)
	insert(t, db, pidByName(t, db, "ff"), "EUR", 2.0, 0)
	insert(t, db, pidByName(t, db, "ff"), "BRL", 10.0, 0)
	res, err := a.Compute(context.Background(), "EUR", "BRL")
	if err != nil {
		t.Fatal(err)
	}
	if mathAbs(res.Rate-5.0) > 1e-9 {
		t.Errorf("EUR→BRL: got %v, want 5.0", res.Rate)
	}
}

func TestCompute_SameCurrencyIsOne(t *testing.T) {
	_, a := setup(t)
	res, err := a.Compute(context.Background(), "USD", "USD")
	if err != nil {
		t.Fatal(err)
	}
	if res.Rate != 1.0 {
		t.Errorf("USD→USD: got %v", res.Rate)
	}
}

func TestCompute_InsufficientProviders(t *testing.T) {
	db, a := setup(t)
	a.cfg.MinProviders = 3
	insert(t, db, pidByName(t, db, "ff"), "EUR", 0.92, 0)
	_, err := a.Compute(context.Background(), "USD", "EUR")
	if !errors.Is(err, ErrInsufficientProviders) {
		t.Errorf("expected ErrInsufficientProviders, got %v", err)
	}
}

func TestCompute_StaleDataExcluded(t *testing.T) {
	db, a := setup(t)
	a.cfg.MaxAgeSeconds = 60
	insert(t, db, pidByName(t, db, "ff"), "EUR", 0.92, 2*time.Hour)
	_, err := a.Compute(context.Background(), "USD", "EUR")
	if !errors.Is(err, ErrInsufficientProviders) {
		t.Errorf("expected ErrInsufficientProviders, got %v", err)
	}
}

func TestCompute_OutlierDropped(t *testing.T) {
	db, a := setup(t)
	a.cfg.OutlierTolerancePct = 5.0
	insert(t, db, pidByName(t, db, "ff"), "EUR", 0.92, 0)
	insert(t, db, pidByName(t, db, "yahoo"), "EUR", 0.93, 0)
	insert(t, db, pidByName(t, db, "mc"), "EUR", 5.0, 0)
	res, err := a.Compute(context.Background(), "USD", "EUR")
	if err != nil {
		t.Fatal(err)
	}
	if res.Rate < 0.91 || res.Rate > 0.94 {
		t.Errorf("expected ~0.92-0.93, got %v", res.Rate)
	}
	if len(res.Sources) != 3 {
		t.Errorf("expected 3 sources, got %d", len(res.Sources))
	}
	if len(res.ProvidersUsed) != 2 {
		t.Errorf("expected 2 providers used (outlier dropped), got %d", len(res.ProvidersUsed))
	}
}

func TestCompute_NoData(t *testing.T) {
	_, a := setup(t)
	a.cfg.MinProviders = 1
	_, err := a.Compute(context.Background(), "USD", "XYZ")
	if !errors.Is(err, ErrInsufficientProviders) {
		t.Errorf("expected ErrInsufficientProviders, got %v", err)
	}
}

func TestAllCurrencies(t *testing.T) {
	db, a := setup(t)
	insert(t, db, pidByName(t, db, "ff"), "EUR", 0.92, 0)
	insert(t, db, pidByName(t, db, "ff"), "BRL", 5.0, 0)
	insert(t, db, pidByName(t, db, "ff"), "JPY", 150.0, 0)
	got, err := a.AllCurrencies(context.Background(), []string{"USD", "EUR", "BRL", "JPY", "ZZZ"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 {
		t.Errorf("expected 3 currencies, got %d (%v)", len(got), got)
	}
	if _, ok := got["USD"]; ok {
		t.Error("USD should not appear (it's the base)")
	}
}

func mathAbs(x float64) float64 {
	if x < 0 {
		return -x
	}
	return x
}
