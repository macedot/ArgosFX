// SPDX-FileCopyrightText: 2026 ArgosFX contributors
// SPDX-License-Identifier: AGPL-3.0-or-later

// SPDX-FileCopyrightText: 2026 ArgosFX contributors
// SPDX-License-Identifier: AGPL-3.0-or-later

package scheduler

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/robfig/cron/v3"
	"github.com/macedot/ArgosFX/internal/provider"
	"github.com/macedot/ArgosFX/internal/store"
)

type fakeProvider struct {
	fetch    func(ctx context.Context, base string, quotes []string) ([]provider.Reading, error)
	calls    int
	callsMu  sync.Mutex
}

func (f *fakeProvider) Name() string { return "fake" }
func (f *fakeProvider) Type() string { return "fake" }
func (f *fakeProvider) Fetch(ctx context.Context, base string, quotes []string) ([]provider.Reading, error) {
	f.callsMu.Lock()
	f.calls++
	f.callsMu.Unlock()
	return f.fetch(ctx, base, quotes)
}
func (f *fakeProvider) Calls() int {
	f.callsMu.Lock()
	defer f.callsMu.Unlock()
	return f.calls
}

func openTestStore(t *testing.T) *store.DB {
	t.Helper()
	db, err := store.Open(context.Background(), filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func quietLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
}

func TestRunOnce_HappyPath(t *testing.T) {
	db := openTestStore(t)
	ctx := context.Background()
	calls := 1000
	pid, err := db.UpsertProvider(ctx, store.Provider{Name: "ff", Type: "frankfurter", CallsPerDay: &calls, Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	fp := &fakeProvider{fetch: func(_ context.Context, base string, quotes []string) ([]provider.Reading, error) {
		return []provider.Reading{
			{Base: base, Quote: "EUR", Rate: 0.92, FetchedAt: time.Now().UTC()},
			{Base: base, Quote: "BRL", Rate: 5.16, FetchedAt: time.Now().UTC()},
		}, nil
	}}
	m := NewManager(db, quietLogger())
	m.cron = cron.New()
	job := ProviderJob{ProviderID: pid, Provider: fp, CurrencyCodes: []string{"EUR", "BRL"}, Base: "USD", CallsPerDay: calls}
	if err := m.RunOnce(ctx, job); err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	if fp.Calls() != 1 {
		t.Errorf("provider fetch calls: got %d", fp.Calls())
	}
	used, err := db.UsageToday(ctx, pid, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	if used != 1 {
		t.Errorf("usage: got %d, want 1", used)
	}
	readings, _, err := db.LatestReadingsPerProvider(ctx, "EUR", time.Now().UTC().Add(-time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if len(readings) != 1 {
		t.Errorf("expected 1 reading, got %d", len(readings))
	}
}

func TestRunOnce_BudgetExhausted(t *testing.T) {
	db := openTestStore(t)
	ctx := context.Background()
	calls := 2
	pid, _ := db.UpsertProvider(ctx, store.Provider{Name: "ff", Type: "frankfurter", CallsPerDay: &calls, Enabled: true})
	for i := 0; i < calls; i++ {
		_ = db.IncrementUsage(ctx, pid, time.Now().UTC())
	}
	fp := &fakeProvider{fetch: func(_ context.Context, base string, quotes []string) ([]provider.Reading, error) {
		t.Fatal("fetch should not be called when budget exhausted")
		return nil, nil
	}}
	m := NewManager(db, quietLogger())
	job := ProviderJob{ProviderID: pid, Provider: fp, CurrencyCodes: []string{"EUR"}, Base: "USD", CallsPerDay: calls}
	if err := m.RunOnce(ctx, job); err != nil {
		t.Fatalf("RunOnce should not error on budget skip, got %v", err)
	}
	if fp.Calls() != 0 {
		t.Errorf("provider fetch should be skipped, got %d calls", fp.Calls())
	}
	used, _ := db.UsageToday(ctx, pid, time.Now().UTC())
	if used != calls {
		t.Errorf("usage should not change when budget exhausted, got %d", used)
	}
}

func TestRunOnce_FetchErrorDoesNotIncrementUsage(t *testing.T) {
	db := openTestStore(t)
	ctx := context.Background()
	calls := 100
	pid, _ := db.UpsertProvider(ctx, store.Provider{Name: "ff", Type: "frankfurter", CallsPerDay: &calls, Enabled: true})
	fp := &fakeProvider{fetch: func(_ context.Context, _ string, _ []string) ([]provider.Reading, error) {
		return nil, errors.New("upstream down")
	}}
	m := NewManager(db, quietLogger())
	job := ProviderJob{ProviderID: pid, Provider: fp, CurrencyCodes: []string{"EUR"}, Base: "USD", CallsPerDay: calls}
	err := m.RunOnce(ctx, job)
	if err == nil {
		t.Fatal("expected error from RunOnce")
	}
	used, _ := db.UsageToday(ctx, pid, time.Now().UTC())
	if used != 0 {
		t.Errorf("usage should remain 0 after fetch error, got %d", used)
	}
}

func TestRunOnce_FetchSkipsUnknownCurrencies(t *testing.T) {
	db := openTestStore(t)
	ctx := context.Background()
	calls := 100
	pid, _ := db.UpsertProvider(ctx, store.Provider{Name: "ff", Type: "frankfurter", CallsPerDay: &calls, Enabled: true})
	fp := &fakeProvider{fetch: func(_ context.Context, _ string, _ []string) ([]provider.Reading, error) {
		return []provider.Reading{
			{Base: "USD", Quote: "EUR", Rate: 0.92, FetchedAt: time.Now().UTC()},
		}, nil
	}}
	m := NewManager(db, quietLogger())
	job := ProviderJob{ProviderID: pid, Provider: fp, CurrencyCodes: []string{"EUR"}, Base: "USD", CallsPerDay: calls}
	if err := m.RunOnce(ctx, job); err != nil {
		t.Fatal(err)
	}
	used, _ := db.UsageToday(ctx, pid, time.Now().UTC())
	if used != 1 {
		t.Errorf("usage should be 1 (we made one upstream call), got %d", used)
	}
}

func TestManager_AddAndStartRunsJobs(t *testing.T) {
	db := openTestStore(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	calls := 100
	pid, _ := db.UpsertProvider(ctx, store.Provider{Name: "ff", Type: "frankfurter", CallsPerDay: &calls, Enabled: true})
	fp := &fakeProvider{fetch: func(_ context.Context, base string, quotes []string) ([]provider.Reading, error) {
		return []provider.Reading{{Base: base, Quote: "EUR", Rate: 0.92, FetchedAt: time.Now().UTC()}}, nil
	}}
	m := NewManager(db, quietLogger())
	job := ProviderJob{ProviderID: pid, Provider: fp, CurrencyCodes: []string{"EUR"}, Base: "USD", CallsPerDay: calls}
	if err := m.Add(job, Schedule{Kind: KindEvery, Every: 200 * time.Millisecond}); err != nil {
		t.Fatal(err)
	}
	m.Start(ctx)
	defer m.Stop()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) && fp.Calls() == 0 {
		time.Sleep(20 * time.Millisecond)
	}
	if fp.Calls() < 1 {
		t.Errorf("expected at least 1 fetch within 3s, got %d", fp.Calls())
	}
}
