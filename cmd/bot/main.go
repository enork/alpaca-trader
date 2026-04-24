package main

import (
	"log/slog"
	"os"

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

	if err := engine.Run(); err != nil {
		log.Error("trading cycle failed", "error", err)
		os.Exit(1)
	}
}
