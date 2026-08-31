// SPDX-FileCopyrightText: 2026 ArgosFX contributors
// SPDX-License-Identifier: AGPL-3.0-or-later

package aggregator

import (
	"context"
	"testing"
	"time"

	"github.com/macedot/ArgosFX/internal/store"
)

func TestHistory_USDDirect(t *testing.T) {
	db, a := setup(t)
	insert(t, db, pidByName(t, db, "ff"), "EUR", 0.92, 0)
	insert(t, db, pidByName(t, db, "ff"), "EUR", 0.93, time.Hour)
	insert(t, db, pidByName(t, db, "ff"), "EUR", 0.91, 2*time.Hour)
	ctx := context.Background()
	end := time.Now().UTC()
	start := end.Add(-3 * time.Hour)
	points, err := a.History(ctx, "USD", "EUR", start, end, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if len(points) == 0 {
		t.Fatal("expected at least 1 history point")
	}
	for _, p := range points {
		if p.Rate <= 0 {
			t.Errorf("rate non-positive: %v", p.Rate)
		}
	}
}

func TestHistory_Inverted(t *testing.T) {
	db, a := setup(t)
	insert(t, db, pidByName(t, db, "ff"), "EUR", 2.0, 0)
	ctx := context.Background()
	end := time.Now().UTC()
	start := end.Add(-time.Hour)
	points, err := a.History(ctx, "EUR", "USD", start, end, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if len(points) == 0 {
		t.Fatal("expected at least 1 inverted point")
	}
	if points[0].Rate < 0.49 || points[0].Rate > 0.51 {
		t.Errorf("EUR→USD expected ~0.5, got %v", points[0].Rate)
	}
}

func TestHistory_CrossRate(t *testing.T) {
	db, a := setup(t)
	insert(t, db, pidByName(t, db, "ff"), "EUR", 2.0, 0)
	insert(t, db, pidByName(t, db, "ff"), "BRL", 10.0, 0)
	ctx := context.Background()
	end := time.Now().UTC()
	start := end.Add(-time.Hour)
	points, err := a.History(ctx, "EUR", "BRL", start, end, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if len(points) == 0 {
		t.Fatal("expected at least 1 cross-rate point")
	}
	if points[0].Rate < 4.99 || points[0].Rate > 5.01 {
		t.Errorf("EUR→BRL expected ~5.0, got %v", points[0].Rate)
	}
}

func TestHistory_OutOfRange(t *testing.T) {
	db, a := setup(t)
	insert(t, db, pidByName(t, db, "ff"), "EUR", 0.92, time.Hour)
	ctx := context.Background()
	now := time.Now().UTC()
	points, err := a.History(ctx, "USD", "EUR", now.Add(-time.Minute), now, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if len(points) != 0 {
		t.Errorf("expected no points in range, got %d", len(points))
	}
}

func TestHistory_NoData(t *testing.T) {
	_, a := setup(t)
	ctx := context.Background()
	now := time.Now().UTC()
	pts, err := a.History(ctx, "USD", "EUR", now.Add(-time.Hour), now, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if len(pts) != 0 {
		t.Errorf("expected no points, got %d", len(pts))
	}
}

func TestPruneOlderThan(t *testing.T) {
	db, a := setup(t)
	calls := 1000
	pid, _ := db.UpsertProvider(context.Background(), store.Provider{
		Name: "ff", Type: "frankfurter", CallsPerDay: &calls, Enabled: true,
	})
	now := time.Now().UTC()
	_ = db.InsertReading(context.Background(), store.Reading{ProviderID: pid, Base: "USD", Quote: "EUR", Rate: 1, FetchedAt: now.Add(-48 * time.Hour)})
	_ = db.InsertReading(context.Background(), store.Reading{ProviderID: pid, Base: "USD", Quote: "EUR", Rate: 2, FetchedAt: now})
	n, err := a.Prune(context.Background(), 24*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Errorf("pruned: got %d, want 1", n)
	}
}
