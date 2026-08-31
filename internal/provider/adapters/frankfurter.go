// SPDX-FileCopyrightText: 2026 ArgosFX contributors
// SPDX-License-Identifier: AGPL-3.0-or-later

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

type Frankfurter struct {
	BaseURL    string
	HTTPClient *http.Client
}

func NewFrankfurter(baseURL string) *Frankfurter {
	if baseURL == "" {
		baseURL = "https://api.frankfurter.app"
	}
	return &Frankfurter{
		BaseURL:    strings.TrimRight(baseURL, "/"),
		HTTPClient: &http.Client{Timeout: 15 * time.Second},
	}
}

func (f *Frankfurter) Name() string { return "frankfurter" }
func (f *Frankfurter) Type() string { return "frankfurter" }

type frankfurterResponse struct {
	Amount float64            `json:"amount"`
	Base   string             `json:"base"`
	Date   string             `json:"date"`
	Rates  map[string]float64 `json:"rates"`
}

func (f *Frankfurter) Fetch(ctx context.Context, base string, quotes []string) ([]provider.Reading, error) {
	if base == "" {
		base = "USD"
	}
	if len(quotes) == 0 {
		return nil, nil
	}
	q := strings.Join(quotes, ",")
	url := fmt.Sprintf("%s/latest?from=%s&to=%s", f.BaseURL, base, q)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := f.HTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("frankfurter fetch: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return nil, fmt.Errorf("frankfurter status %d: %s", resp.StatusCode, string(body))
	}
	var out frankfurterResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, fmt.Errorf("frankfurter decode: %w", err)
	}
	if !strings.EqualFold(out.Base, base) {
		return nil, fmt.Errorf("frankfurter base mismatch: want %s got %s", base, out.Base)
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
			ProviderTS: out.Date,
		})
	}
	return readings, nil
}
