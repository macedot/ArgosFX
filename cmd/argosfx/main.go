// SPDX-FileCopyrightText: 2026 ArgosFX contributors
// SPDX-License-Identifier: AGPL-3.0-or-later

// SPDX-FileCopyrightText: 2026 ArgosFX contributors
// SPDX-License-Identifier: AGPL-3.0-or-later

package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/macedot/ArgosFX/internal/aggregator"
	"github.com/macedot/ArgosFX/internal/config"
	"github.com/macedot/ArgosFX/internal/httpapi"
	"github.com/macedot/ArgosFX/internal/ratelookup"
	"github.com/macedot/ArgosFX/internal/store"
)

func main() {
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
	srv := httpapi.New(httpapi.Options{
		Logger:     logger,
		Aggregator: agg,
		Store:      db,
		Cache:      cache,
		Currencies: currencies,
		CacheTTL:   cfg.Cache.HTTPMaxAge(),
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
