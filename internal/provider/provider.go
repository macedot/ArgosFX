package provider

import (
	"context"
	"time"
)

type Reading struct {
	Base       string
	Quote      string
	Rate       float64
	FetchedAt  time.Time
	ProviderTS string
}

type Provider interface {
	Name() string
	Type() string
	Fetch(ctx context.Context, base string, quotes []string) ([]Reading, error)
}
