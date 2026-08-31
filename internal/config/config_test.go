package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoad_ValidConfig(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte(`
server:
  listen: ":8080"
cache:
  compute_ttl_seconds: 60
aggregator:
  outlier_tolerance_pct: 2.0
providers:
  - name: frankfurter
    type: frankfurter
    calls_per_day: 1000
    priority: 1
`), 0o644); err != nil {
		t.Fatal(err)
	}
	c, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if c.Server.Listen != ":8080" {
		t.Errorf("listen: got %q", c.Server.Listen)
	}
	if len(c.Providers) != 1 {
		t.Fatalf("providers: got %d", len(c.Providers))
	}
	if c.Providers[0].Name != "frankfurter" {
		t.Errorf("provider name: got %q", c.Providers[0].Name)
	}
}

func TestLoad_DefaultsApplied(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte(`
server:
  listen: ":9090"
providers: []
`), 0o644); err != nil {
		t.Fatal(err)
	}
	c, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if c.Cache.ComputeTTLSeconds != 60 {
		t.Errorf("compute_ttl default: got %d", c.Cache.ComputeTTLSeconds)
	}
	if c.Aggregator.OutlierTolerancePct != 2.0 {
		t.Errorf("outlier default: got %v", c.Aggregator.OutlierTolerancePct)
	}
	if c.Aggregator.MaxAgeSeconds != 86400 {
		t.Errorf("max_age default: got %d", c.Aggregator.MaxAgeSeconds)
	}
}

func TestLoad_DuplicateProviderName(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte(`
server:
  listen: ":8080"
providers:
  - {name: a, type: x}
  - {name: a, type: y}
`), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path); err == nil {
		t.Fatal("expected error for duplicate provider name")
	}
}

func TestLoad_MissingListen(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte(`providers: []`), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path); err == nil {
		t.Fatal("expected error for missing listen")
	}
}

func TestLoadCurrencies_AlwaysIncludesUSD(t *testing.T) {
	t.Setenv("FX_CURRENCIES", "EUR,BRL,GBP")
	c, err := LoadCurrencies()
	if err != nil {
		t.Fatal(err)
	}
	for _, code := range []string{"USD", "EUR", "BRL", "GBP"} {
		if _, ok := c.Allowed[code]; !ok {
			t.Errorf("missing %s", code)
		}
	}
}

func TestLoadCurrencies_LowercaseNormalized(t *testing.T) {
	t.Setenv("FX_CURRENCIES", "eur, brl")
	c, err := LoadCurrencies()
	if err != nil {
		t.Fatal(err)
	}
	for _, code := range []string{"USD", "EUR", "BRL"} {
		if _, ok := c.Allowed[code]; !ok {
			t.Errorf("missing %s", code)
		}
	}
}

func TestLoadCurrencies_InvalidCode(t *testing.T) {
	t.Setenv("FX_CURRENCIES", "EURO")
	if _, err := LoadCurrencies(); err == nil {
		t.Fatal("expected error for invalid code")
	}
}

func TestLoadCurrencies_Missing(t *testing.T) {
	t.Setenv("FX_CURRENCIES", "")
	if _, err := LoadCurrencies(); err == nil {
		t.Fatal("expected error when FX_CURRENCIES unset")
	}
}
