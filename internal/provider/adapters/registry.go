package adapters

import (
	"fmt"
	"net/http"
	"time"

	"github.com/tmacedo/fxrate/internal/config"
	"github.com/tmacedo/fxrate/internal/provider"
)

func NewFromConfig(p config.Provider) (provider.Provider, error) {
	switch p.Type {
	case "frankfurter":
		baseURL := stringConfig(p.Config, "base_url", "https://api.frankfurter.app")
		return NewFrankfurter(baseURL), nil
	default:
		return nil, fmt.Errorf("unknown provider type %q", p.Type)
	}
}

func stringConfig(m map[string]any, key, fallback string) string {
	if m == nil {
		return fallback
	}
	v, ok := m[key]
	if !ok {
		return fallback
	}
	s, ok := v.(string)
	if !ok {
		return fallback
	}
	return s
}

func defaultHTTPClient() *http.Client {
	return &http.Client{Timeout: 15 * time.Second}
}
