// SPDX-FileCopyrightText: 2026 ArgosFX contributors
// SPDX-License-Identifier: AGPL-3.0-or-later

package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/macedot/ArgosFX/internal/aggregator"
	"github.com/macedot/ArgosFX/internal/config"
	"github.com/macedot/ArgosFX/internal/httpapi"
	"github.com/macedot/ArgosFX/internal/obs"
	"github.com/macedot/ArgosFX/internal/provider/adapters"
	"github.com/macedot/ArgosFX/internal/ratelookup"
	"github.com/macedot/ArgosFX/internal/scheduler"
	"github.com/macedot/ArgosFX/internal/store"
)

func main() {
	if len(os.Args) > 1 && os.Args[1] == "--healthcheck" {
		runHealthcheck()
		return
	}
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	slog.SetDefault(logger)

	cfgPath := envOr("ARGOSFX_CONFIG_PATH", "/etc/argosfx/config.yaml")
	cfg, err := config.Load(cfgPath)
	if err != nil {
		logger.Error("load config", "err", err)
		os.Exit(1)
	}

	currencies, err := config.LoadCurrencies()
	if err != nil {
		logger.Error("load currencies", "err", err)
		os.Exit(1)
	}
	logger.Info("currencies loaded", "count", len(currencies.Allowed))

	dbPath := envOr("ARGOSFX_DB_PATH", "/data/argosfx.db")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	db, err := store.Open(ctx, dbPath)
	if err != nil {
		logger.Error("open store", "err", err)
		os.Exit(1)
	}
	defer db.Close()
	logger.Info("store opened", "path", dbPath)

	agg := aggregator.New(db, aggregator.Config{
		OutlierTolerancePct: cfg.Aggregator.OutlierTolerancePct,
		MinProviders:        cfg.Aggregator.MinProviders,
		MaxAgeSeconds:       cfg.Aggregator.MaxAgeSeconds,
		Base:                "USD",
	})

	cache := ratelookup.NewCache(cfg.Cache.ComputeTTL())
	metrics := obs.New()

	mgr := scheduler.NewManager(db, logger)
	mgr.SetMetrics(metrics)
	codes := quoteCodes(currencies, "USD")
	added := 0
	for _, pc := range cfg.Providers {
		if !pc.IsEnabled() {
			continue
		}
		prov, err := adapters.NewFromConfig(pc)
		if err != nil {
			logger.Error("create provider", "name", pc.Name, "err", err)
			continue
		}
		stored, err := db.UpsertProvider(ctx, store.Provider{
			Name: pc.Name, Type: pc.Type, Config: pc.Config,
			Priority: pc.Priority, CallsPerDay: pc.CallsPerDay,
			ScheduleCron: pc.ScheduleCron, Enabled: pc.Enabled,
		})
		if err != nil {
			logger.Error("upsert provider", "name", pc.Name, "err", err)
			continue
		}
		sched := scheduler.ComputeSchedule(derefInt(pc.CallsPerDay), pc.ScheduleCron)
		if sched.Kind == scheduler.KindCron {
			logger.Info("scheduled provider (cron)", "name", pc.Name, "expr", sched.Cron)
		} else {
			logger.Info("scheduled provider (every)", "name", pc.Name, "every", sched.Every)
		}
		job := scheduler.ProviderJob{
			ProviderID:    stored,
			Provider:      prov,
			CurrencyCodes: codes,
			Base:          "USD",
			CallsPerDay:   derefInt(pc.CallsPerDay),
		}
		if err := mgr.Add(job, sched); err != nil {
			logger.Error("add job", "name", pc.Name, "err", err)
			continue
		}
		added++
	}
	logger.Info("providers scheduled", "count", added)

	if cfg.Aggregator.RetentionDays > 0 {
		retention := time.Duration(cfg.Aggregator.RetentionDays) * 24 * time.Hour
		if err := mgr.AddFunc("@daily", func() {
			ctxPrune, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
			defer cancel()
			n, err := agg.Prune(ctxPrune, retention)
			if err != nil {
				logger.Warn("retention prune failed", "err", err)
				return
			}
			logger.Info("retention prune", "rows_removed", n)
		}); err != nil {
			logger.Error("schedule retention", "err", err)
		} else {
			logger.Info("retention job scheduled", "retention_days", cfg.Aggregator.RetentionDays)
		}
	}

	mgr.Start(ctx)
	defer mgr.Stop()

	srv := httpapi.New(httpapi.Options{
		Logger:     logger,
		Aggregator: agg,
		Store:      db,
		Cache:      cache,
		Currencies: currencies,
		CacheTTL:   cfg.Cache.HTTPMaxAge(),
		Metrics:    metrics,
		MaxAge:     cfg.Aggregator.MaxAge(),
	})

	httpServer := &http.Server{
		Addr:              cfg.Server.Listen,
		Handler:           srv.Handler(),
		ReadHeaderTimeout: 5 * time.Second,
	}

	idleDone := make(chan struct{})
	go func() {
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
		<-sigCh
		logger.Info("shutting down")
		shutdownCtx, c := context.WithTimeout(context.Background(), 10*time.Second)
		defer c()
		_ = httpServer.Shutdown(shutdownCtx)
		close(idleDone)
	}()

	logger.Info("listening", "addr", cfg.Server.Listen)
	if err := httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		logger.Error("listen", "err", err)
		os.Exit(1)
	}
	<-idleDone
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func derefInt(p *int) int {
	if p == nil {
		return 0
	}
	return *p
}

func quoteCodes(c config.Currencies, base string) []string {
	out := make([]string, 0, len(c.Allowed))
	for code := range c.Allowed {
		if code != base {
			out = append(out, code)
		}
	}
	return out
}

func runHealthcheck() {
	addr := envOr("ARGOSFX_LISTEN", ":8080")
	url := "http://127.0.0.1" + addr + "/v1/healthz"
	client := &http.Client{Timeout: 3 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		fmt.Fprintln(os.Stderr, "healthcheck: dial err:", err)
		os.Exit(1)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		fmt.Fprintln(os.Stderr, "healthcheck: status", resp.StatusCode)
		os.Exit(1)
	}
}
