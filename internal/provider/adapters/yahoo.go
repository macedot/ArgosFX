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

type Yahoo struct {
	BaseURL    string
	HTTPClient *http.Client
	UserAgent  string
}

func NewYahoo(baseURL string) *Yahoo {
	if baseURL == "" {
		baseURL = "https://query2.finance.yahoo.com"
	}
	return &Yahoo{
		BaseURL:    strings.TrimRight(baseURL, "/"),
		HTTPClient: &http.Client{Timeout: 15 * time.Second},
		UserAgent:  "Mozilla/5.0 (compatible; ArgosFX/1.0)",
	}
}

func (y *Yahoo) Name() string { return "yahoo" }
func (y *Yahoo) Type() string { return "yahoo" }

type yahooChartResponse struct {
	Chart struct {
		Result []struct {
			Meta struct {
				Symbol             string  `json:"symbol"`
				RegularMarketPrice float64 `json:"regularMarketPrice"`
				RegularMarketTime  int64   `json:"regularMarketTime"`
				Currency           string  `json:"currency"`
			} `json:"meta"`
			Indicators struct {
				Quote []struct {
					Close []float64 `json:"close"`
				} `json:"quote"`
			} `json:"indicators"`
		} `json:"result"`
		Error any `json:"error"`
	} `json:"chart"`
}

func (y *Yahoo) Fetch(ctx context.Context, base string, quotes []string) ([]provider.Reading, error) {
	if base == "" {
		base = "USD"
	}
	if len(quotes) == 0 {
		return nil, nil
	}
	now := time.Now().UTC()
	var readings []provider.Reading
	for _, q := range quotes {
		quote := strings.ToUpper(q)
		pair := base + quote + "=X"
		url := fmt.Sprintf("%s/v8/finance/chart/%s?interval=1d&range=1d", y.BaseURL, pair)
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("User-Agent", y.UserAgent)
		resp, err := y.HTTPClient.Do(req)
		if err != nil {
			return nil, fmt.Errorf("yahoo fetch %s: %w", pair, err)
		}
		if resp.StatusCode/100 != 2 {
			body, _ := io.ReadAll(io.LimitReader(resp.Body, 256))
			resp.Body.Close()
			return nil, fmt.Errorf("yahoo status %d for %s: %s", resp.StatusCode, pair, string(body))
		}
		var out yahooChartResponse
		if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
			resp.Body.Close()
			return nil, fmt.Errorf("yahoo decode %s: %w", pair, err)
		}
		resp.Body.Close()
		if out.Chart.Error != nil {
			return nil, fmt.Errorf("yahoo error for %s: %v", pair, out.Chart.Error)
		}
		if len(out.Chart.Result) == 0 {
			return nil, fmt.Errorf("yahoo: no result for %s", pair)
		}
		meta := out.Chart.Result[0].Meta
		rate := meta.RegularMarketPrice
		if rate == 0 && len(out.Chart.Result[0].Indicators.Quote) > 0 {
			closes := out.Chart.Result[0].Indicators.Quote[0].Close
			if len(closes) > 0 {
				rate = closes[len(closes)-1]
			}
		}
		if rate == 0 {
			return nil, fmt.Errorf("yahoo: zero rate for %s", pair)
		}
		ts := ""
		if meta.RegularMarketTime > 0 {
			ts = time.Unix(meta.RegularMarketTime, 0).UTC().Format(time.RFC3339)
		}
		readings = append(readings, provider.Reading{
			Base:       strings.ToUpper(base),
			Quote:      quote,
			Rate:       rate,
			FetchedAt:  now,
			ProviderTS: ts,
		})
	}
	return readings, nil
}
