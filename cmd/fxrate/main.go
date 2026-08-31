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

	"github.com/tmacedo/fxrate/internal/config"
	"github.com/tmacedo/fxrate/internal/httpapi"
	"github.com/tmacedo/fxrate/internal/store"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	slog.SetDefault(logger)

	cfgPath := envOr("FX_CONFIG_PATH", "/etc/fxrate/config.yaml")
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

	dbPath := envOr("FX_DB_PATH", "/data/fxrate.db")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	db, err := store.Open(ctx, dbPath)
	if err != nil {
		logger.Error("open store", "err", err)
		os.Exit(1)
	}
	defer db.Close()
	logger.Info("store opened", "path", dbPath)

	srv := httpapi.New()
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
