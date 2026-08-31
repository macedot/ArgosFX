// SPDX-FileCopyrightText: 2026 ArgosFX contributors
// SPDX-License-Identifier: AGPL-3.0-or-later

// SPDX-FileCopyrightText: 2026 ArgosFX contributors
// SPDX-License-Identifier: AGPL-3.0-or-later

package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/macedot/ArgosFX/internal/aggregator"
	"github.com/macedot/ArgosFX/internal/config"
	"github.com/macedot/ArgosFX/internal/ratelookup"
	"github.com/macedot/ArgosFX/internal/store"
)

func openStore(t *testing.T) *store.DB {
	t.Helper()
	db, err := store.Open(context.Background(), filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func newServer(t *testing.T) (*Server, *store.DB) {
	db := openStore(t)
	calls := 1000
	pid, err := db.UpsertProvider(context.Background(), store.Provider{
		Name: "ff", Type: "frankfurter", CallsPerDay: &calls, Enabled: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	_ = db.InsertReading(context.Background(), store.Reading{ProviderID: pid, Base: "USD", Quote: "EUR", Rate: 0.92, FetchedAt: now})
	_ = db.InsertReading(context.Background(), store.Reading{ProviderID: pid, Base: "USD", Quote: "BRL", Rate: 5.16, FetchedAt: now})
	agg := aggregator.New(db, aggregator.Config{
		OutlierTolerancePct: 2.0, MinProviders: 1, MaxAgeSeconds: 3600, Base: "USD",
	})
	currencies := config.Currencies{Allowed: map[string]struct{}{"USD": {}, "EUR": {}, "BRL": {}, "GBP": {}}}
	srv := New(Options{
		Aggregator: agg,
		Store:      db,
		Cache:      ratelookup.NewCache(time.Minute),
		Currencies: currencies,
		CacheTTL:   time.Minute,
	})
	return srv, db
}

func TestHealthz(t *testing.T) {
	s, _ := newServer(t)
	req := httptest.NewRequest(http.MethodGet, "/v1/healthz", nil)
	rr := httptest.NewRecorder()
	s.Handler().ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status: got %d", rr.Code)
	}
	var body map[string]any
	if err := json.NewDecoder(rr.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if body["status"] != "ok" {
		t.Errorf("status: got %v", body["status"])
	}
}

func TestRates_All(t *testing.T) {
	s, _ := newServer(t)
	req := httptest.NewRequest(http.MethodGet, "/v1/rates", nil)
	rr := httptest.NewRecorder()
	s.Handler().ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status: got %d body=%s", rr.Code, rr.Body.String())
	}
	var body ratesResponse
	if err := json.NewDecoder(rr.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if body.Base != "USD" {
		t.Errorf("base: got %q", body.Base)
	}
	if rate, ok := body.Rates["EUR"]; !ok || rate < 0.91 || rate > 0.93 {
		t.Errorf("EUR rate: got %v", rate)
	}
	if rate, ok := body.Rates["BRL"]; !ok || rate < 5.15 || rate > 5.17 {
		t.Errorf("BRL rate: got %v", rate)
	}
	if cc := rr.Header().Get("Cache-Control"); cc == "" {
		t.Error("expected Cache-Control header")
	}
}

func TestRates_SinglePair_CachedAfterFirstCall(t *testing.T) {
	s, db := newServer(t)
	calls := 1000
	pid, _ := db.GetProviderByName(context.Background(), "ff")
	_ = pid
	_ = calls
	req := httptest.NewRequest(http.MethodGet, "/v1/rates/USD/EUR", nil)
	rr1 := httptest.NewRecorder()
	s.Handler().ServeHTTP(rr1, req)
	if rr1.Code != http.StatusOK {
		t.Fatalf("first call: %d", rr1.Code)
	}
	req2 := httptest.NewRequest(http.MethodGet, "/v1/rates/USD/EUR", nil)
	rr2 := httptest.NewRecorder()
	s.Handler().ServeHTTP(rr2, req2)
	if rr2.Code != http.StatusOK {
		t.Fatalf("second call: %d", rr2.Code)
	}
	if rr1.Body.String() != rr2.Body.String() {
		t.Error("expected cached body to match")
	}
}

func TestRates_DisallowedCurrency(t *testing.T) {
	s, _ := newServer(t)
	req := httptest.NewRequest(http.MethodGet, "/v1/rates/USD/XYZ", nil)
	rr := httptest.NewRecorder()
	s.Handler().ServeHTTP(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status: got %d", rr.Code)
	}
}

func TestRates_InsufficientData(t *testing.T) {
	db := openStore(t)
	agg := aggregator.New(db, aggregator.Config{
		OutlierTolerancePct: 2.0, MinProviders: 5, MaxAgeSeconds: 3600, Base: "USD",
	})
	currencies := config.Currencies{Allowed: map[string]struct{}{"USD": {}, "EUR": {}}}
	srv := New(Options{
		Aggregator: agg,
		Cache:      ratelookup.NewCache(time.Minute),
		Currencies: currencies,
		CacheTTL:   time.Minute,
	})
	req := httptest.NewRequest(http.MethodGet, "/v1/rates/USD/EUR", nil)
	rr := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rr, req)
	if rr.Code != http.StatusServiceUnavailable {
		t.Fatalf("status: got %d", rr.Code)
	}
}

func TestRates_Invert(t *testing.T) {
	s, _ := newServer(t)
	req := httptest.NewRequest(http.MethodGet, "/v1/rates/EUR/USD", nil)
	rr := httptest.NewRecorder()
	s.Handler().ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status: %d body=%s", rr.Code, rr.Body.String())
	}
	var body singleRateResponse
	if err := json.NewDecoder(rr.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if body.Rate < 1.08 || body.Rate > 1.10 {
		t.Errorf("EUR→USD expected ~1.087, got %v", body.Rate)
	}
}

func TestRates_CrossRate(t *testing.T) {
	s, _ := newServer(t)
	req := httptest.NewRequest(http.MethodGet, "/v1/rates/EUR/BRL", nil)
	rr := httptest.NewRecorder()
	s.Handler().ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status: %d", rr.Code)
	}
	var body singleRateResponse
	if err := json.NewDecoder(rr.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if body.Rate < 5.5 || body.Rate > 6.0 {
		t.Errorf("EUR→BRL expected ~5.6, got %v", body.Rate)
	}
}

func TestProviders(t *testing.T) {
	s, _ := newServer(t)
	req := httptest.NewRequest(http.MethodGet, "/v1/providers", nil)
	rr := httptest.NewRecorder()
	s.Handler().ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status: %d", rr.Code)
	}
	var body struct {
		Providers []providerView `json:"providers"`
	}
	if err := json.NewDecoder(rr.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if len(body.Providers) != 1 {
		t.Fatalf("expected 1 provider, got %d", len(body.Providers))
	}
	if body.Providers[0].Name != "ff" {
		t.Errorf("provider name: got %q", body.Providers[0].Name)
	}
}

func TestHistory_USDDirect(t *testing.T) {
	s, _ := newServer(t)
	req := httptest.NewRequest(http.MethodGet, "/v1/rates/USD/EUR/history", nil)
	rr := httptest.NewRecorder()
	s.Handler().ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status: %d body=%s", rr.Code, rr.Body.String())
	}
	var body historyResponse
	if err := json.NewDecoder(rr.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if body.From != "USD" || body.To != "EUR" {
		t.Errorf("from/to: %s/%s", body.From, body.To)
	}
	if body.Step == "" {
		t.Error("expected step to be set")
	}
}

func TestHistory_BadStep(t *testing.T) {
	s, _ := newServer(t)
	req := httptest.NewRequest(http.MethodGet, "/v1/rates/USD/EUR/history?step=foo", nil)
	rr := httptest.NewRecorder()
	s.Handler().ServeHTTP(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Errorf("status: got %d", rr.Code)
	}
}

func TestHistory_BadEnd(t *testing.T) {
	s, _ := newServer(t)
	req := httptest.NewRequest(http.MethodGet, "/v1/rates/USD/EUR/history?end=not-a-date", nil)
	rr := httptest.NewRecorder()
	s.Handler().ServeHTTP(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Errorf("status: got %d", rr.Code)
	}
}

func TestHistory_DisallowedCurrency(t *testing.T) {
	s, _ := newServer(t)
	req := httptest.NewRequest(http.MethodGet, "/v1/rates/USD/XYZ/history", nil)
	rr := httptest.NewRecorder()
	s.Handler().ServeHTTP(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Errorf("status: got %d", rr.Code)
	}
}
