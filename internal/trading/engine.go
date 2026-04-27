package trading

import (
	"log/slog"

	"github.com/alpacahq/alpaca-trade-api-go/v3/alpaca"
	"github.com/shopspring/decimal"

	"github.com/enork/alpaca-trader/internal/broker"
	"github.com/enork/alpaca-trader/internal/config"
	"github.com/enork/alpaca-trader/internal/notify"
	"github.com/enork/alpaca-trader/internal/options"
)

// Engine orchestrates the per-symbol trading cycle.
type Engine struct {
	cfg      *config.Config
	bc       *broker.Client
	sel      *options.Selector
	notifier *notify.Notifier
	log      *slog.Logger
}

// New creates a trading Engine. notifier may be nil to disable email alerts.
func New(cfg *config.Config, bc *broker.Client, sel *options.Selector, notifier *notify.Notifier, log *slog.Logger) *Engine {
	return &Engine{cfg: cfg, bc: bc, sel: sel, notifier: notifier, log: log}
}

// Run executes one full trading cycle.
//
// Symbols are evaluated in a round-robin loop. Each round visits every ticker
// still in the pending pool and removes it when either:
//
//	A) An open option order or position already exists for that ticker
//	   (including orders placed earlier in this same cycle).
//	B) The cash guard determines there is not enough buying power to cover
//	   an additional put obligation.
//
// The loop exits once the pending pool is empty or a full round produces no
// new order placements (preventing an infinite loop on persistent errors).
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

	for _, pos := range positions {
		e.log.Info("position", "symbol", pos.Symbol, "qty", pos.Qty, "avg_entry_price", pos.AvgEntryPrice)
	}

	orders, err := e.bc.GetOpenOrders()
	if err != nil {
		return err
	}

	for _, order := range orders {
		e.log.Info("open order", "symbol", order.Symbol, "qty", order.Qty, "side", order.Side, "type", order.Type, "status", order.Status)
	}

	// pending holds tickers still to be evaluated.
	pending := e.cfg.EnabledSymbols()

	// placed accumulates puts submitted this cycle so the cash guard and
	// open-activity checks stay accurate without re-fetching from the API.
	var placed []newPut
	var skips []cashGuardSkip

	for len(pending) > 0 {
		var nextRound []config.Symbol
		roundPlaced := 0

		for _, sym := range pending {
			ticker := sym.Ticker

			// Condition A — open activity from a prior cycle or this one.
			if hasOpenOptionActivity(ticker, positions, orders) || placedThisCycle(ticker, placed) {
				e.log.Info("removing from pool: open option activity", "ticker", ticker)
				continue // do not carry forward to nextRound
			}

			stockPos := findStockPosition(ticker, positions)
			if stockPos != nil && stockPos.Qty.GreaterThanOrEqual(decimal.NewFromInt(100)) {
				contracts := int(stockPos.Qty.Div(decimal.NewFromInt(100)).IntPart())
				costBasis, _ := stockPos.AvgEntryPrice.Float64()
				if err := e.placeCalls(ticker, contracts, costBasis); err != nil {
					e.log.Warn("covered call skipped", "ticker", ticker, "error", err)
				} else {
					roundPlaced++
				}
				continue // calls placed (or failed); either way done for this ticker
			}

			// Condition B — cash guard; also handles "no qualifying option" errors.
			strike, skip, err := e.placePut(ticker, acct, positions, orders, placed)
			if skip != nil {
				skips = append(skips, *skip)
				e.log.Info("removing from pool: insufficient cash (condition B)", "ticker", ticker)
				continue
			}
			if err != nil {
				e.log.Warn("put skipped", "ticker", ticker, "error", err)
				continue
			}

			// Put placed — track it so subsequent tickers see the updated exposure.
			placed = append(placed, newPut{ticker: ticker, strike: strike})
			roundPlaced++
			// Ticker intentionally not added to nextRound; next round will detect
			// it via placedThisCycle → condition A and remove it cleanly.
			nextRound = append(nextRound, sym)
		}

		pending = nextRound

		// If nothing was placed this round there can be no forward progress;
		// break to avoid spinning on tickers with persistent errors.
		if roundPlaced == 0 {
			break
		}
	}

	if len(skips) > 0 {
		alert := e.buildCashGuardAlert(acct, positions, placed, skips)
		e.logCashGuardSummary(alert)
		if e.notifier != nil && e.cfg.Notify.Enabled {
			if err := e.notifier.SendCashGuardAlert(alert); err != nil {
				e.log.Warn("failed to send cash guard email", "error", err)
			}
		}
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
