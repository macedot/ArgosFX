// SPDX-FileCopyrightText: 2026 ArgosFX contributors
// SPDX-License-Identifier: AGPL-3.0-or-later

// SPDX-FileCopyrightText: 2026 ArgosFX contributors
// SPDX-License-Identifier: AGPL-3.0-or-later

package config

import (
	"fmt"
	"os"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

type Server struct {
	Listen string `yaml:"listen"`
}

type Cache struct {
	ComputeTTLSeconds    int `yaml:"compute_ttl_seconds"`
	HTTPCacheMaxAgeSec   int `yaml:"http_cache_max_age_seconds"`
}

type Aggregator struct {
	OutlierTolerancePct float64 `yaml:"outlier_tolerance_pct"`
	MinProviders         int     `yaml:"min_providers"`
	MaxAgeSeconds        int     `yaml:"max_age_seconds"`
	RetentionDays        int     `yaml:"retention_days"`
}

type Provider struct {
	Name         string                 `yaml:"name"`
	Type         string                 `yaml:"type"`
	Config       map[string]any         `yaml:"config"`
	Priority     int                    `yaml:"priority"`
	CallsPerDay  *int                   `yaml:"calls_per_day"`
	ScheduleCron string                 `yaml:"schedule_cron"`
	Enabled      bool                   `yaml:"enabled"`
	Extra        map[string]any         `yaml:"-"`
}

type Config struct {
	Server     Server     `yaml:"server"`
	Cache      Cache      `yaml:"cache"`
	Aggregator Aggregator `yaml:"aggregator"`
	Providers  []Provider `yaml:"providers"`
}

type Currencies struct {
	Allowed map[string]struct{}
}

func applyDefaults(c *Config) {
	if c.Cache.ComputeTTLSeconds <= 0 {
		c.Cache.ComputeTTLSeconds = 60
	}
	if c.Cache.HTTPCacheMaxAgeSec <= 0 {
		c.Cache.HTTPCacheMaxAgeSec = c.Cache.ComputeTTLSeconds
	}
	if c.Aggregator.OutlierTolerancePct <= 0 {
		c.Aggregator.OutlierTolerancePct = 2.0
	}
	if c.Aggregator.MinProviders <= 0 {
		c.Aggregator.MinProviders = 1
	}
	if c.Aggregator.MaxAgeSeconds <= 0 {
		c.Aggregator.MaxAgeSeconds = 86400
	}
	if c.Aggregator.RetentionDays <= 0 {
		c.Aggregator.RetentionDays = 365
	}
}

func (c Config) Validate() error {
	if c.Server.Listen == "" {
		return fmt.Errorf("server.listen is required")
	}
	seen := map[string]bool{}
	for i, p := range c.Providers {
		if p.Name == "" {
			return fmt.Errorf("providers[%d]: name is required", i)
		}
		if p.Type == "" {
			return fmt.Errorf("providers[%d] (%s): type is required", i, p.Name)
		}
		if seen[p.Name] {
			return fmt.Errorf("providers[%d] (%s): duplicate name", i, p.Name)
		}
		seen[p.Name] = true
	}
	return nil
}

func Load(path string) (Config, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return Config{}, fmt.Errorf("read config: %w", err)
	}
	var c Config
	if err := yaml.Unmarshal(b, &c); err != nil {
		return Config{}, fmt.Errorf("parse config: %w", err)
	}
	applyDefaults(&c)
	if err := c.Validate(); err != nil {
		return Config{}, err
	}
	return c, nil
}

func LoadCurrencies() (Currencies, error) {
	raw := strings.TrimSpace(os.Getenv("ARGOSFX_CURRENCIES"))
	out := Currencies{Allowed: map[string]struct{}{}}
	if raw == "" {
		return out, fmt.Errorf("ARGOSFX_CURRENCIES env var is required")
	}
	for _, code := range strings.Split(raw, ",") {
		code = strings.TrimSpace(strings.ToUpper(code))
		if code == "" {
			continue
		}
		if len(code) != 3 {
			return out, fmt.Errorf("invalid currency code %q (must be 3 letters)", code)
		}
		out.Allowed[code] = struct{}{}
	}
	if _, ok := out.Allowed["USD"]; !ok {
		out.Allowed["USD"] = struct{}{}
	}
	return out, nil
}

func (c Cache) ComputeTTL() time.Duration {
	return time.Duration(c.ComputeTTLSeconds) * time.Second
}

func (c Cache) HTTPMaxAge() time.Duration {
	return time.Duration(c.HTTPCacheMaxAgeSec) * time.Second
}

func (a Aggregator) MaxAge() time.Duration {
	return time.Duration(a.MaxAgeSeconds) * time.Second
}

func (p Provider) IsEnabled() bool {
	return p.Enabled
}
