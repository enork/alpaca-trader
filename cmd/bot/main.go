package main

import (
	"log/slog"
	"os"

	"github.com/enork/alpaca-trader/internal/broker"
	"github.com/enork/alpaca-trader/internal/config"
)

func main() {
	log := slog.New(slog.NewJSONHandler(os.Stdout, nil))

	cfg, err := config.Load("config.yaml")
	if err != nil {
		log.Error("failed to load config", "error", err)
		os.Exit(1)
	}

	log.Info("config loaded", "symbols", len(cfg.EnabledSymbols()), "max_dte", cfg.Trading.MaxDTE)

	bc := broker.New(cfg.Alpaca)

	acct, err := bc.GetAccount()
	if err != nil {
		log.Error("failed to fetch account", "error", err)
		os.Exit(1)
	}

	log.Info("account ready",
		"id", acct.ID,
		"cash", acct.Cash,
		"buying_power", acct.BuyingPower,
		"status", acct.Status,
	)
}
