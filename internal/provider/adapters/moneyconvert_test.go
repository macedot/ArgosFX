// SPDX-FileCopyrightText: 2026 ArgosFX contributors
// SPDX-License-Identifier: AGPL-3.0-or-later

package adapters

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/macedot/ArgosFX/internal/provider"
)

func TestMoneyConvert_Fetch_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"base":      "USD",
			"disclaimer": "test",
			"license":   "test",
			"rates": map[string]float64{
				"EUR": 0.92,
				"BRL": 5.16,
				"JPY": 150.0,
			},
		})
	}))
	defer srv.Close()

	m := NewMoneyConvert(srv.URL, "")
	readings, err := m.Fetch(context.Background(), "USD", []string{"EUR", "BRL", "JPY"})
	if err != nil {
		t.Fatal(err)
	}
	if len(readings) != 3 {
		t.Fatalf("readings: got %d", len(readings))
	}
	byQuote := map[string]provider.Reading{}
	for _, r := range readings {
		byQuote[r.Quote] = r
	}
	for code, want := range map[string]float64{"EUR": 0.92, "BRL": 5.16, "JPY": 150.0} {
		r, ok := byQuote[code]
		if !ok {
			t.Errorf("missing %s", code)
			continue
		}
		if r.Rate != want {
			t.Errorf("%s: got %v, want %v", code, r.Rate, want)
		}
		if r.Base != "USD" {
			t.Errorf("%s base: got %q", code, r.Base)
		}
		if r.FetchedAt.IsZero() || time.Since(r.FetchedAt) > 5*time.Second {
			t.Errorf("%s fetched_at not recent: %v", code, r.FetchedAt)
		}
	}
}

func TestMoneyConvert_Fetch_BaseMismatch(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"base":  "EUR",
			"rates": map[string]float64{"USD": 1.1},
		})
	}))
	defer srv.Close()
	m := NewMoneyConvert(srv.URL, "")
	if _, err := m.Fetch(context.Background(), "USD", []string{"EUR"}); err == nil {
		t.Fatal("expected base mismatch error")
	}
}

func TestMoneyConvert_Fetch_NonOK(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "down", http.StatusInternalServerError)
	}))
	defer srv.Close()
	m := NewMoneyConvert(srv.URL, "")
	if _, err := m.Fetch(context.Background(), "USD", []string{"EUR"}); err == nil {
		t.Fatal("expected error on 500")
	}
}

func TestMoneyConvert_Fetch_MissingQuoteSkipped(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"base":  "USD",
			"rates": map[string]float64{"EUR": 0.92},
		})
	}))
	defer srv.Close()
	m := NewMoneyConvert(srv.URL, "")
	got, err := m.Fetch(context.Background(), "USD", []string{"EUR", "BRL"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Quote != "EUR" {
		t.Errorf("expected only EUR, got %+v", got)
	}
}

func TestMoneyConvert_Name(t *testing.T) {
	m := NewMoneyConvert("", "")
	if m.Name() != "moneyconvert" || m.Type() != "moneyconvert" {
		t.Errorf("name/type: %s/%s", m.Name(), m.Type())
	}
}
