package main

import (
	"log/slog"
	"os"

	"github.com/enork/alpaca-trader/internal/broker"
	"github.com/enork/alpaca-trader/internal/config"
	"github.com/enork/alpaca-trader/internal/options"
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

	sel := options.New(bc)

	for _, sym := range cfg.EnabledSymbols() {
		price, err := bc.GetLatestPrice(sym.Ticker)
		if err != nil {
			log.Warn("skipping symbol: could not fetch price", "ticker", sym.Ticker, "error", err)
			continue
		}
		log.Info("latest price", "ticker", sym.Ticker, "price", price)

		put, err := sel.SelectPut(sym.Ticker, cfg.Trading.MaxDTE)
		if err != nil {
			log.Warn("no put selected", "ticker", sym.Ticker, "error", err)
		} else {
			log.Info("selected put",
				"ticker", sym.Ticker,
				"symbol", put.Symbol,
				"strike", put.Strike,
				"expiry", put.Expiry,
				"bid", put.BidPrice,
			)
		}
	}
}
