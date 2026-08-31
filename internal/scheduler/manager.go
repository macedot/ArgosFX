// SPDX-FileCopyrightText: 2026 ArgosFX contributors
// SPDX-License-Identifier: AGPL-3.0-or-later

// SPDX-FileCopyrightText: 2026 ArgosFX contributors
// SPDX-License-Identifier: AGPL-3.0-or-later

package scheduler

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/robfig/cron/v3"
	"github.com/macedot/ArgosFX/internal/provider"
	"github.com/macedot/ArgosFX/internal/store"
)

type ProviderJob struct {
	ProviderID    int64
	Provider      provider.Provider
	CurrencyCodes []string
	Base          string
	CallsPerDay   int
}

type Manager struct {
	db  *store.DB
	log *slog.Logger
	cron *cron.Cron
}

func NewManager(db *store.DB, log *slog.Logger) *Manager {
	if log == nil {
		log = slog.Default()
	}
	return &Manager{
		db:   db,
		log:  log,
		cron: cron.New(),
	}
}

func (m *Manager) Start(ctx context.Context) {
	m.cron.Start()
}

func (m *Manager) Stop() {
	m.cron.Stop()
}

func (m *Manager) Add(j ProviderJob, sched Schedule) error {
	job := j
	expr := sched.Cron
	if sched.Kind == KindEvery {
		expr = "@every " + sched.Every.String()
	}
	id, err := m.cron.AddFunc(expr, func() {
		m.runWithTimeout(job)
	})
	if err != nil {
		return err
	}
	m.log.Info("scheduled provider",
		"provider", job.Provider.Name(),
		"expr", expr,
		"entry_id", id)
	return nil
}

func (m *Manager) runWithTimeout(j ProviderJob) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := m.RunOnce(ctx, j); err != nil {
		m.log.Warn("provider fetch failed",
			"provider", j.Provider.Name(),
			"err", err)
	}
}

func (m *Manager) RunOnce(ctx context.Context, j ProviderJob) error {
	used, err := m.db.UsageToday(ctx, j.ProviderID, time.Now().UTC())
	if err != nil {
		return fmt.Errorf("read usage: %w", err)
	}
	if !BudgetAllowed(used, j.CallsPerDay) {
		m.log.Warn("daily budget exhausted, skipping fetch",
			"provider", j.Provider.Name(),
			"used", used,
			"limit", j.CallsPerDay)
		return nil
	}
	readings, err := j.Provider.Fetch(ctx, j.Base, j.CurrencyCodes)
	if err != nil {
		return fmt.Errorf("fetch %s: %w", j.Provider.Name(), err)
	}
	for _, r := range readings {
		if err := m.db.InsertReading(ctx, store.Reading{
			ProviderID: j.ProviderID,
			Base:       r.Base,
			Quote:      r.Quote,
			Rate:       r.Rate,
			FetchedAt:  r.FetchedAt,
			ProviderTS: r.ProviderTS,
		}); err != nil {
			return fmt.Errorf("insert reading: %w", err)
		}
	}
	if err := m.db.IncrementUsage(ctx, j.ProviderID, time.Now().UTC()); err != nil {
		return fmt.Errorf("increment usage: %w", err)
	}
	m.log.Info("provider fetched",
		"provider", j.Provider.Name(),
		"readings", len(readings))
	return nil
}
