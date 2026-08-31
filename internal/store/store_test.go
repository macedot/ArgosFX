package store

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

func openTestDB(t *testing.T) *DB {
	t.Helper()
	path := filepath.Join(t.TempDir(), "test.db")
	db, err := Open(context.Background(), path)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func TestOpen_RunsMigrations(t *testing.T) {
	db := openTestDB(t)
	row := db.sql.QueryRow(`SELECT count(*) FROM schema_migrations`)
	var n int
	if err := row.Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n < 1 {
		t.Errorf("expected at least 1 migration applied, got %d", n)
	}
}

func TestUpsertProvider_InsertsThenUpdates(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	calls := 1000
	id1, err := db.UpsertProvider(ctx, Provider{
		Name: "frankfurter", Type: "frankfurter", Priority: 1,
		CallsPerDay: &calls, Enabled: true, Config: map[string]any{"k": "v"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if id1 == 0 {
		t.Fatal("expected id > 0")
	}
	id2, err := db.UpsertProvider(ctx, Provider{
		Name: "frankfurter", Type: "frankfurter", Priority: 5,
		CallsPerDay: &calls, Enabled: true, Config: map[string]any{"k": "v2"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if id1 != id2 {
		t.Errorf("expected same id, got %d vs %d", id1, id2)
	}
	got, err := db.GetProviderByName(ctx, "frankfurter")
	if err != nil {
		t.Fatal(err)
	}
	if got.Priority != 5 {
		t.Errorf("priority: got %d", got.Priority)
	}
	if got.Config["k"] != "v2" {
		t.Errorf("config not updated: %v", got.Config)
	}
}

func TestInsertAndLatestReadingsPerProvider(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	calls := 1000
	pid, err := db.UpsertProvider(ctx, Provider{Name: "ff", Type: "frankfurter", CallsPerDay: &calls, Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	for i, rate := range []float64{0.9, 0.91, 0.92} {
		err := db.InsertReading(ctx, Reading{
			ProviderID: pid, Base: "USD", Quote: "EUR", Rate: rate,
			FetchedAt: now.Add(time.Duration(i) * time.Minute),
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	readings, pids, err := db.LatestReadingsPerProvider(ctx, "EUR", now.Add(-time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if len(readings) != 1 || readings[0].Rate != 0.92 {
		t.Errorf("expected 1 latest @ 0.92, got %+v", readings)
	}
	if len(pids) != 1 || pids[0] != pid {
		t.Errorf("expected provider id %d, got %v", pid, pids)
	}
}

func TestUsageCounter(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	calls := 1000
	pid, _ := db.UpsertProvider(ctx, Provider{Name: "ff", Type: "frankfurter", CallsPerDay: &calls, Enabled: true})
	day := time.Now().UTC()
	for i := 0; i < 3; i++ {
		if err := db.IncrementUsage(ctx, pid, day); err != nil {
			t.Fatal(err)
		}
	}
	got, err := db.UsageToday(ctx, pid, day)
	if err != nil {
		t.Fatal(err)
	}
	if got != 3 {
		t.Errorf("usage: got %d, want 3", got)
	}
}

func TestHistoryDownsample(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	calls := 1000
	pid, _ := db.UpsertProvider(ctx, Provider{Name: "ff", Type: "frankfurter", CallsPerDay: &calls, Enabled: true})
	now := time.Now().UTC().Truncate(time.Hour)
	for i := 0; i < 6; i++ {
		_ = db.InsertReading(ctx, Reading{
			ProviderID: pid, Base: "USD", Quote: "EUR", Rate: float64(i),
			FetchedAt: now.Add(time.Duration(i*10) * time.Minute),
		})
	}
	points, err := db.History(ctx, "EUR", now.Add(-time.Hour), now.Add(time.Hour), 30*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if len(points) < 1 || len(points) > 3 {
		t.Errorf("expected 1-3 buckets, got %d", len(points))
	}
}

func TestPruneOlderThan(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	calls := 1000
	pid, _ := db.UpsertProvider(ctx, Provider{Name: "ff", Type: "frankfurter", CallsPerDay: &calls, Enabled: true})
	old := time.Now().UTC().Add(-48 * time.Hour)
	recent := time.Now().UTC()
	_ = db.InsertReading(ctx, Reading{ProviderID: pid, Base: "USD", Quote: "EUR", Rate: 1, FetchedAt: old})
	_ = db.InsertReading(ctx, Reading{ProviderID: pid, Base: "USD", Quote: "EUR", Rate: 2, FetchedAt: recent})
	n, err := db.PruneOlderThan(ctx, time.Now().UTC().Add(-24*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Errorf("pruned: got %d, want 1", n)
	}
}
