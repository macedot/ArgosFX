// SPDX-FileCopyrightText: 2026 ArgosFX contributors
// SPDX-License-Identifier: AGPL-3.0-or-later

package adapters

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/macedot/ArgosFX/internal/provider"
)

type MoneyConvert struct {
	BaseURL    string
	APIKey     string
	HTTPClient *http.Client
}

func NewMoneyConvert(baseURL, apiKey string) *MoneyConvert {
	if baseURL == "" {
		baseURL = "https://cdn.moneyconvert.net"
	}
	return &MoneyConvert{
		BaseURL:    strings.TrimRight(baseURL, "/"),
		APIKey:     apiKey,
		HTTPClient: &http.Client{Timeout: 15 * time.Second},
	}
}

func (m *MoneyConvert) Name() string { return "moneyconvert" }
func (m *MoneyConvert) Type() string { return "moneyconvert" }

type moneyConvertResponse struct {
	Base      string             `json:"base"`
	Disclaimer string            `json:"disclaimer"`
	License   string             `json:"license"`
	Rates     map[string]float64 `json:"rates"`
}

func (m *MoneyConvert) Fetch(ctx context.Context, base string, quotes []string) ([]provider.Reading, error) {
	if base == "" {
		base = "USD"
	}
	if len(quotes) == 0 {
		return nil, nil
	}
	url := m.BaseURL + "/api/latest.json"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	if m.APIKey != "" {
		q := req.URL.Query()
		q.Set("key", m.APIKey)
		req.URL.RawQuery = q.Encode()
	}
	resp, err := m.HTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("moneyconvert fetch: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return nil, fmt.Errorf("moneyconvert status %d: %s", resp.StatusCode, string(body))
	}
	var out moneyConvertResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, fmt.Errorf("moneyconvert decode: %w", err)
	}
	if !strings.EqualFold(out.Base, base) {
		return nil, fmt.Errorf("moneyconvert base mismatch: want %s got %s", base, out.Base)
	}
	now := time.Now().UTC()
	var readings []provider.Reading
	for _, q := range quotes {
		rate, ok := out.Rates[strings.ToUpper(q)]
		if !ok {
			continue
		}
		readings = append(readings, provider.Reading{
			Base:       strings.ToUpper(out.Base),
			Quote:      strings.ToUpper(q),
			Rate:       rate,
			FetchedAt:  now,
			ProviderTS: "",
		})
	}
	return readings, nil
}
