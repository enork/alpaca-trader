//go:build integration

package trading_test

import (
	"log/slog"
	"os"
	"testing"

	"github.com/enork/alpaca-trader/internal/broker"
	"github.com/enork/alpaca-trader/internal/config"
	"github.com/enork/alpaca-trader/internal/options"
	"github.com/enork/alpaca-trader/internal/trading"
)

// TestTradingCycleE2E runs a full cycle against the Alpaca paper account.
// Requires ALPACA_API_KEY and ALPACA_API_SECRET to be set in the environment.
// Run with: make test-integration
func TestTradingCycleE2E(t *testing.T) {
	cfg, err := config.Load("../../config.yaml")
	if err != nil {
		t.Fatalf("load config: %v", err)
	}

	log := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelDebug}))
	bc := broker.New(cfg.Alpaca, log)

	// Verify connectivity and log account state.
	acct, err := bc.GetAccount()
	if err != nil {
		t.Fatalf("get account: %v", err)
	}
	t.Logf("account id=%s status=%s cash=%s buying_power=%s", acct.ID, acct.Status, acct.Cash, acct.BuyingPower)

	if acct.Status != "ACTIVE" {
		t.Fatalf("account status is %q, expected ACTIVE", acct.Status)
	}

	positions, err := bc.GetPositions()
	if err != nil {
		t.Fatalf("get positions: %v", err)
	}
	t.Logf("positions: %d", len(positions))

	orders, err := bc.GetOpenOrders()
	if err != nil {
		t.Fatalf("get open orders: %v", err)
	}
	t.Logf("open orders: %d", len(orders))

	// Verify option chain and price data for each enabled symbol.
	sel := options.New(bc, log)
	for _, sym := range cfg.EnabledSymbols() {
		price, err := bc.GetLatestPrice(sym.Ticker)
		if err != nil {
			t.Errorf("get latest price %s: %v", sym.Ticker, err)
			continue
		}
		t.Logf("%s latest price: %.4f", sym.Ticker, price)

		put, err := sel.SelectPut(sym.Ticker, cfg.Trading.MaxDTE)
		if err != nil {
			t.Logf("%s: no put selected (%v) — may be expected if market is closed", sym.Ticker, err)
		} else {
			t.Logf("%s best put: symbol=%s strike=%.3f expiry=%s bid=%.4f",
				sym.Ticker, put.Symbol, put.Strike, put.Expiry, put.BidPrice)
		}
	}

	// Run a full cycle (orders are placed only if conditions are met on the paper account).
	engine := trading.New(cfg, bc, sel, nil, log)
	if err := engine.Run(); err != nil {
		t.Fatalf("trading cycle: %v", err)
	}
}
