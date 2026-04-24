package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/enork/alpaca-trader/internal/broker"
	"github.com/enork/alpaca-trader/internal/config"
	"github.com/enork/alpaca-trader/internal/notify"
	"github.com/enork/alpaca-trader/internal/options"
	"github.com/enork/alpaca-trader/internal/trading"
)

func main() {
	log := slog.New(slog.NewJSONHandler(os.Stdout, nil))

	cfg, err := config.Load("config.yaml")
	if err != nil {
		log.Error("failed to load config", "error", err)
		os.Exit(1)
	}

	bc := broker.New(cfg.Alpaca, log)
	sel := options.New(bc, log)

	notifier, err := notify.New(cfg.Notify)
	if err != nil {
		log.Warn("email notifications disabled", "reason", err)
	}

	engine := trading.New(cfg, bc, sel, notifier, log)

	if !cfg.Trading.RunOnStartup && !cfg.Trading.RunOnOpen && cfg.Trading.RunOnCron == "" {
		log.Warn("no run mode enabled; set run_on_startup, run_on_open, or run_on_cron in config.yaml")
		os.Exit(1)
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	r := &runner{cfg: cfg, engine: engine, bc: bc, log: log}
	r.start(ctx)

	log.Info("shutting down")
}
