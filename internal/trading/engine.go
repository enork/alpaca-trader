package trading

import (
	"log/slog"
	"math"
	"time"

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

	pending := e.cfg.EnabledSymbols()

	var cycleOrders []cycleOrder
	var skips []cashGuardSkip
	var activitySkips []string

	for len(pending) > 0 {
		var nextRound []config.Symbol
		roundPlaced := 0

		for _, sym := range pending {
			ticker := sym.Ticker

			// Condition A — open activity from a prior cycle or this one.
			if hasOpenOptionActivity(ticker, positions, orders) || placedThisCycle(ticker, cycleOrders) {
				e.log.Info("removing from pool: open option activity", "ticker", ticker)
				activitySkips = append(activitySkips, ticker)
				continue
			}

			stockPos := findStockPosition(ticker, positions)
			if stockPos != nil && stockPos.Qty.GreaterThanOrEqual(decimal.NewFromInt(100)) {
				contracts := int(stockPos.Qty.Div(decimal.NewFromInt(100)).IntPart())
				costBasis, _ := stockPos.AvgEntryPrice.Float64()
				co, err := e.placeCalls(ticker, contracts, costBasis)
				if err != nil {
					e.log.Warn("covered call skipped", "ticker", ticker, "error", err)
				} else {
					cycleOrders = append(cycleOrders, *co)
					roundPlaced++
				}
				continue
			}

			co, skip, err := e.placePut(ticker, acct, positions, orders, cycleOrders)
			if skip != nil {
				skips = append(skips, *skip)
				e.log.Info("removing from pool: insufficient cash (condition B)", "ticker", ticker)
				continue
			}
			if err != nil {
				e.log.Warn("put skipped", "ticker", ticker, "error", err)
				continue
			}

			cycleOrders = append(cycleOrders, *co)
			roundPlaced++
			nextRound = append(nextRound, sym)
		}

		pending = nextRound
		if roundPlaced == 0 {
			break
		}
	}

	if len(skips) > 0 {
		alert := e.buildCashGuardAlert(acct, positions, cycleOrders, skips)
		e.logCashGuardSummary(alert)
		if e.notifier != nil && e.cfg.Notify.Enabled {
			if err := e.notifier.SendCashGuardAlert(alert); err != nil {
				e.log.Warn("failed to send cash guard email", "error", err)
			}
		}
	}

	if e.notifier != nil && e.cfg.Notify.RunSummaryEnabled {
		if err := e.sendRunSummary(acct, positions, orders, cycleOrders, skips, activitySkips); err != nil {
			e.log.Warn("failed to send run summary email", "error", err)
		}
	}

	return nil
}

// sendRunSummary builds and sends the end-of-cycle HTML summary email.
func (e *Engine) sendRunSummary(
	acct *alpaca.Account,
	positions []alpaca.Position,
	orders []alpaca.Order,
	cycleOrders []cycleOrder,
	skips []cashGuardSkip,
	activitySkips []string,
) error {
	// Fetch activities from the last 48 hours to cover the previous session.
	activities, err := e.bc.GetRecentActivities(time.Now().Add(-48 * time.Hour))
	if err != nil {
		e.log.Warn("could not fetch recent activities for summary", "error", err)
	}

	pv, _ := acct.PortfolioValue.Float64()
	equity, _ := acct.Equity.Float64()
	lastEquity, _ := acct.LastEquity.Float64()
	cash, _ := acct.Cash.Float64()
	bp, _ := acct.BuyingPower.Float64()
	lmv, _ := acct.LongMarketValue.Float64()

	summary := notify.RunSummary{
		RunAt:           time.Now(),
		PaperTrading:    e.cfg.Alpaca.PaperTrading,
		AccountNumber:   acct.AccountNumber,
		PortfolioValue:  pv,
		Equity:          equity,
		LastEquity:      lastEquity,
		Cash:            cash,
		BuyingPower:     bp,
		LongMarketValue: lmv,
		ActivitySkips:   activitySkips,
	}

	// Positions
	for _, p := range positions {
		cp := float64(0)
		mv := float64(0)
		upl := float64(0)
		uplPct := float64(0)
		if p.CurrentPrice != nil {
			cp, _ = p.CurrentPrice.Float64()
		}
		if p.MarketValue != nil {
			mv, _ = p.MarketValue.Float64()
		}
		if p.UnrealizedPL != nil {
			upl, _ = p.UnrealizedPL.Float64()
		}
		if p.UnrealizedPLPC != nil {
			pct, _ := p.UnrealizedPLPC.Float64()
			uplPct = pct * 100
		}
		qty, _ := p.Qty.Float64()
		entry, _ := p.AvgEntryPrice.Float64()

		sp := notify.SummaryPosition{
			Symbol:          p.Symbol,
			Qty:             math.Abs(qty),
			AvgEntryPrice:   entry,
			CurrentPrice:    cp,
			MarketValue:     mv,
			UnrealizedPL:    upl,
			UnrealizedPLPct: uplPct,
		}
		if root, _, optType, strike, err := options.ParseSymbol(p.Symbol); err == nil {
			sp.IsOption = true
			sp.Symbol = root
			sp.Strike = strike
			if optType == "C" {
				sp.OptionSide = "CALL"
			} else {
				sp.OptionSide = "PUT"
			}
		}
		summary.Positions = append(summary.Positions, sp)
	}

	// Open orders
	for _, o := range orders {
		qty := float64(0)
		lp := float64(0)
		if o.Qty != nil {
			qty, _ = o.Qty.Float64()
		}
		if o.LimitPrice != nil {
			lp, _ = o.LimitPrice.Float64()
		}
		so := notify.SummaryOrder{
			Symbol:      o.Symbol,
			Side:        string(o.Side),
			Qty:         math.Abs(qty),
			LimitPrice:  lp,
			Status:      o.Status,
			SubmittedAt: o.SubmittedAt,
		}
		if root, expiry, optType, strike, err := options.ParseSymbol(o.Symbol); err == nil {
			so.IsOption = true
			so.Symbol = root
			so.Strike = strike
			so.Expiry = expiry.String()
			if optType == "C" {
				so.OptionSide = "CALL"
			} else {
				so.OptionSide = "PUT"
			}
		}
		summary.OpenOrders = append(summary.OpenOrders, so)
	}

	// This cycle's placed orders
	for _, co := range cycleOrders {
		summary.PlacedOrders = append(summary.PlacedOrders, notify.SummaryPlacedOrder{
			Ticker:    co.ticker,
			Symbol:    co.symbol,
			Side:      co.optType,
			Strike:    co.strike,
			Expiry:    co.expiry,
			BidPrice:  co.bidPrice,
			Contracts: co.contracts,
			OrderID:   co.orderID,
		})
	}

	// Cash guard blocks
	for _, s := range skips {
		summary.CashGuardBlocks = append(summary.CashGuardBlocks, s.Ticker)
	}

	// Recent activities
	for _, a := range activities {
		price, _ := a.Price.Float64()
		qty, _ := a.Qty.Float64()
		net, _ := a.NetAmount.Float64()
		sa := notify.SummaryActivity{
			Time:        a.TransactionTime,
			Type:        a.ActivityType,
			Symbol:      a.Symbol,
			Side:        a.Side,
			Qty:         math.Abs(qty),
			Price:       price,
			NetAmount:   net,
			Description: a.Description,
		}
		summary.Activities = append(summary.Activities, sa)
	}

	return e.notifier.SendRunSummary(summary)
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
