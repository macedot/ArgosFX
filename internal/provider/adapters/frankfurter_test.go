// SPDX-FileCopyrightText: 2026 ArgosFX contributors
// SPDX-License-Identifier: AGPL-3.0-or-later

// SPDX-FileCopyrightText: 2026 ArgosFX contributors
// SPDX-License-Identifier: AGPL-3.0-or-later

package adapters

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/macedot/ArgosFX/internal/provider"
)

func TestFrankfurter_Name(t *testing.T) {
	f := NewFrankfurter("")
	if f.Name() != "frankfurter" {
		t.Errorf("name: got %q", f.Name())
	}
	if f.Type() != "frankfurter" {
		t.Errorf("type: got %q", f.Type())
	}
}

func TestFrankfurter_Fetch_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, "/latest") {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		if got := r.URL.Query().Get("from"); got != "USD" {
			t.Errorf("from: got %q", got)
		}
		if got := r.URL.Query().Get("to"); !strings.Contains(got, "EUR") || !strings.Contains(got, "BRL") {
			t.Errorf("to: got %q", got)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"amount": 1.0,
			"base":   "USD",
			"date":   "2026-08-28",
			"rates":  map[string]float64{"EUR": 0.92, "BRL": 5.16},
		})
	}))
	defer srv.Close()

	f := NewFrankfurter(srv.URL)
	readings, err := f.Fetch(context.Background(), "USD", []string{"EUR", "BRL"})
	if err != nil {
		t.Fatal(err)
	}
	if len(readings) != 2 {
		t.Fatalf("readings: got %d", len(readings))
	}
	byQuote := map[string]provider.Reading{}
	for _, r := range readings {
		byQuote[r.Quote] = r
	}
	if r, ok := byQuote["EUR"]; !ok || r.Rate != 0.92 {
		t.Errorf("EUR: %+v", r)
	}
	if r, ok := byQuote["BRL"]; !ok || r.Rate != 5.16 {
		t.Errorf("BRL: %+v", r)
	}
	for _, r := range readings {
		if r.Base != "USD" {
			t.Errorf("base: got %q", r.Base)
		}
		if r.ProviderTS != "2026-08-28" {
			t.Errorf("provider_ts: got %q", r.ProviderTS)
		}
		if r.FetchedAt.IsZero() {
			t.Errorf("fetched_at zero")
		}
		if time.Since(r.FetchedAt) > 5*time.Second {
			t.Errorf("fetched_at not recent: %v", r.FetchedAt)
		}
	}
}

func TestFrankfurter_Fetch_BaseMismatch(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"amount": 1.0, "base": "EUR", "date": "2026-08-28",
			"rates": map[string]float64{"USD": 1.1},
		})
	}))
	defer srv.Close()
	f := NewFrankfurter(srv.URL)
	if _, err := f.Fetch(context.Background(), "USD", []string{"EUR"}); err == nil {
		t.Fatal("expected base mismatch error")
	}
}

func TestFrankfurter_Fetch_NonOKStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "rate limited", http.StatusTooManyRequests)
	}))
	defer srv.Close()
	f := NewFrankfurter(srv.URL)
	if _, err := f.Fetch(context.Background(), "USD", []string{"EUR"}); err == nil {
		t.Fatal("expected error on 429")
	}
}

func TestFrankfurter_Fetch_EmptyQuotesReturnsNil(t *testing.T) {
	f := NewFrankfurter("http://localhost")
	got, err := f.Fetch(context.Background(), "USD", nil)
	if err != nil {
		t.Fatal(err)
	}
	if got != nil {
		t.Errorf("expected nil, got %v", got)
	}
}

func TestFrankfurter_Fetch_MissingQuoteSkipped(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"amount": 1.0, "base": "USD", "date": "2026-08-28",
			"rates": map[string]float64{"EUR": 0.92},
		})
	}))
	defer srv.Close()
	f := NewFrankfurter(srv.URL)
	got, err := f.Fetch(context.Background(), "USD", []string{"EUR", "BRL"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Quote != "EUR" {
		t.Errorf("expected only EUR, got %+v", got)
	}
}
