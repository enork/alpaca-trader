package main

import (
	"log/slog"
	"os"

	"github.com/enork/alpaca-trader/internal/config"
	"github.com/enork/alpaca-trader/internal/notify"
)

func main() {
	log := slog.New(slog.NewJSONHandler(os.Stdout, nil))

	cfg, err := config.Load("config.yaml")
	if err != nil {
		log.Error("failed to load config", "error", err)
		os.Exit(1)
	}
	n, err := notify.New(cfg.Notify)
	if err != nil {
		log.Error("failed to create notifier", "error", err)
		os.Exit(1)
	}
	err = n.SendCashGuardAlert(notify.CashGuardAlert{
		SkippedTickers:   []string{"PLUG", "IBIT"},
		Cash:             511.94,
		ExistingExposure: 300.00,
		AdditionalTotal:  88.06,
		AdditionalPerPut: 44.03,
	})
	if err != nil {
		log.Error("send failed", "error", err)
		os.Exit(1)
	}
	log.Info("test email sent")
}
