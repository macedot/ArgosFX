// SPDX-FileCopyrightText: 2026 ArgosFX contributors
// SPDX-License-Identifier: AGPL-3.0-or-later

package adapters

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/macedot/ArgosFX/internal/provider"
)

func TestYahoo_Fetch_Success(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if !strings.HasPrefix(r.URL.Path, "/v8/finance/chart/") {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		if !strings.Contains(r.URL.RawQuery, "interval=1d") {
			t.Errorf("expected interval=1d, got %s", r.URL.RawQuery)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"chart": map[string]any{
				"error": nil,
				"result": []map[string]any{{
					"meta": map[string]any{
						"symbol":             "EURUSD=X",
						"regularMarketPrice": 1.16,
						"regularMarketTime":  1713000000,
						"currency":           "USD",
					},
					"indicators": map[string]any{
						"quote": []map[string]any{{
							"close": []float64{1.16},
						}},
					},
				}},
			},
		})
	}))
	defer srv.Close()

	y := NewYahoo(srv.URL)
	readings, err := y.Fetch(t.Context(), "USD", []string{"EUR"})
	if err != nil {
		t.Fatal(err)
	}
	if len(readings) != 1 {
		t.Fatalf("readings: got %d", len(readings))
	}
	r := readings[0]
	if r.Quote != "EUR" {
		t.Errorf("quote: got %q", r.Quote)
	}
	if r.Rate != 1.16 {
		t.Errorf("rate: got %v", r.Rate)
	}
	if r.Base != "USD" {
		t.Errorf("base: got %q", r.Base)
	}
	if r.ProviderTS == "" {
		t.Errorf("provider_ts empty")
	}
	if time.Since(r.FetchedAt) > 5*time.Second {
		t.Errorf("fetched_at not recent: %v", r.FetchedAt)
	}
	if calls != 1 {
		t.Errorf("expected 1 upstream call, got %d", calls)
	}
}

func TestYahoo_Fetch_MultipleQuotes(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rate := 0.92
		if strings.Contains(r.URL.Path, "JPY") {
			rate = 150.0
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"chart": map[string]any{
				"result": []map[string]any{{
					"meta": map[string]any{
						"symbol":             "USDXXX=X",
						"regularMarketPrice": rate,
						"regularMarketTime":  1713000000,
						"currency":           "XXX",
					},
				}},
			},
		})
	}))
	defer srv.Close()

	y := NewYahoo(srv.URL)
	got, err := y.Fetch(t.Context(), "USD", []string{"EUR", "JPY"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 readings, got %d", len(got))
	}
	byQuote := map[string]provider.Reading{}
	for _, r := range got {
		byQuote[r.Quote] = r
	}
	if byQuote["EUR"].Rate != 0.92 || byQuote["JPY"].Rate != 150.0 {
		t.Errorf("rates wrong: %+v", byQuote)
	}
}

func TestYahoo_Fetch_NonOK(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "rate limited", http.StatusTooManyRequests)
	}))
	defer srv.Close()
	y := NewYahoo(srv.URL)
	if _, err := y.Fetch(t.Context(), "USD", []string{"EUR"}); err == nil {
		t.Fatal("expected error on 429")
	}
}

func TestYahoo_Fetch_NoResult(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"chart": map[string]any{"result": nil, "error": "not found"},
		})
	}))
	defer srv.Close()
	y := NewYahoo(srv.URL)
	if _, err := y.Fetch(t.Context(), "USD", []string{"EUR"}); err == nil {
		t.Fatal("expected error for empty result")
	}
}

func TestYahoo_Fetch_FallsBackToClose(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"chart": map[string]any{
				"result": []map[string]any{{
					"meta": map[string]any{"regularMarketPrice": 0.0, "regularMarketTime": 0},
					"indicators": map[string]any{
						"quote": []map[string]any{{"close": []float64{0.93}}},
					},
				}},
			},
		})
	}))
	defer srv.Close()
	y := NewYahoo(srv.URL)
	got, err := y.Fetch(t.Context(), "USD", []string{"EUR"})
	if err != nil {
		t.Fatal(err)
	}
	if got[0].Rate != 0.93 {
		t.Errorf("expected fallback to close, got %v", got[0].Rate)
	}
}

func TestYahoo_Name(t *testing.T) {
	y := NewYahoo("")
	if y.Name() != "yahoo" || y.Type() != "yahoo" {
		t.Errorf("name/type: %s/%s", y.Name(), y.Type())
	}
}
