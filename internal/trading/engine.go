package trading

import (
	"log/slog"

	"github.com/alpacahq/alpaca-trade-api-go/v3/alpaca"
	"github.com/shopspring/decimal"

	"github.com/enork/alpaca-trader/internal/broker"
	"github.com/enork/alpaca-trader/internal/config"
	"github.com/enork/alpaca-trader/internal/options"
)

// Engine orchestrates the per-symbol trading cycle.
type Engine struct {
	cfg *config.Config
	bc  *broker.Client
	sel *options.Selector
	log *slog.Logger
}

// New creates a trading Engine.
func New(cfg *config.Config, bc *broker.Client, sel *options.Selector, log *slog.Logger) *Engine {
	return &Engine{cfg: cfg, bc: bc, sel: sel, log: log}
}

// Run executes one full trading cycle: fetch state, iterate enabled symbols, place orders.
func (e *Engine) Run() error {
	acct, err := e.bc.GetAccount()
	if err != nil {
		return err
	}
	e.log.Info("account state", "cash", acct.Cash, "buying_power", acct.BuyingPower)

	positions, err := e.bc.GetPositions()
	if err != nil {
		return err
	}

	orders, err := e.bc.GetOpenOrders()
	if err != nil {
		return err
	}

	var skips []cashGuardSkip

	for _, sym := range e.cfg.EnabledSymbols() {
		ticker := sym.Ticker

		if hasOpenOptionActivity(ticker, positions, orders) {
			e.log.Info("skipping: open option activity", "ticker", ticker)
			continue
		}

		stockPos := findStockPosition(ticker, positions)
		if stockPos != nil && stockPos.Qty.GreaterThanOrEqual(decimal.NewFromInt(100)) {
			contracts := int(stockPos.Qty.Div(decimal.NewFromInt(100)).IntPart())
			costBasis, _ := stockPos.AvgEntryPrice.Float64()
			if err := e.placeCalls(ticker, contracts, costBasis); err != nil {
				e.log.Warn("covered call skipped", "ticker", ticker, "error", err)
			}
		} else {
			skip, err := e.placePut(ticker, acct, positions, orders)
			if err != nil {
				e.log.Warn("put skipped", "ticker", ticker, "error", err)
			}
			if skip != nil {
				skips = append(skips, *skip)
			}
		}
	}

	if len(skips) > 0 {
		e.logCashGuardSummary(acct, positions, skips)
		// Phase 4: send email notification here.
	}

	return nil
}

// hasOpenOptionActivity returns true if any open option position or order exists for ticker.
func hasOpenOptionActivity(ticker string, positions []alpaca.Position, orders []alpaca.Order) bool {
	for _, p := range positions {
		if root, _, _, _, err := options.ParseSymbol(p.Symbol); err == nil && root == ticker {
			return true
		}
	}
	for _, o := range orders {
		if root, _, _, _, err := options.ParseSymbol(o.Symbol); err == nil && root == ticker {
			return true
		}
	}
	return false
}

// findStockPosition returns the equity position for ticker, or nil if none.
func findStockPosition(ticker string, positions []alpaca.Position) *alpaca.Position {
	for i := range positions {
		p := &positions[i]
		if p.Symbol == ticker && p.AssetClass == alpaca.USEquity {
			return p
		}
	}
	return nil
}
