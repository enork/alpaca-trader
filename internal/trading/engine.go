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
// Before scanning for new orders, any short option positions that have reached
// the 50% profit target are closed (buy-to-close). Symbols are then evaluated
// in a round-robin loop. Each round visits every ticker still in the pending
// pool and removes it when either:
//
//	A) The put/call count has reached the per-symbol contracts target.
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

	// Check existing short option positions for early-close opportunities
	e.checkEarlyClose(positions, orders)

	pending := e.cfg.EnabledSymbols()

	var cycleOrders []cycleOrder
	var skips []cashGuardSkip
	var activitySkips []string

	for len(pending) > 0 {
		var nextRound []config.Symbol
		roundPlaced := 0

		for _, sym := range pending {
			ticker := sym.Ticker
			params := e.selectionParams(sym)

			// Determine how many puts/calls already exist for this ticker
			// (positions, pending orders, and orders placed earlier this cycle).
			existingPuts := countExistingContracts(ticker, "P", "put", positions, orders, cycleOrders)
			existingCalls := countExistingContracts(ticker, "C", "call", positions, orders, cycleOrders)

			// ── Covered-call path ──────────────────────────────────────────────
			stockPos := findStockPosition(ticker, positions)
			if stockPos != nil && stockPos.Qty.GreaterThanOrEqual(decimal.NewFromInt(100)) {
				totalLots := int(stockPos.Qty.Div(decimal.NewFromInt(100)).IntPart())
				uncoveredLots := totalLots - existingCalls
				if uncoveredLots <= 0 {
					e.log.Info("removing from pool: all lots already covered", "ticker", ticker, "lots", totalLots)
					activitySkips = append(activitySkips, ticker)
					continue
				}
				costBasis, _ := stockPos.AvgEntryPrice.Float64()

				if sym.Ladder {
					opts, lerr := e.sel.SelectCallLadder(ticker, uncoveredLots, costBasis, params)
					if lerr != nil {
						e.log.Warn("covered call ladder skipped", "ticker", ticker, "error", lerr)
					} else {
						for _, opt := range opts {
							order, oerr := e.bc.PlaceOptionOrder(opt.Symbol, 1, opt.LimitPrice)
							if oerr != nil {
								e.log.Warn("ladder call order failed", "ticker", ticker, "expiry", opt.Expiry, "error", oerr)
								break
							}
							e.log.Info("ladder call order placed",
								"ticker", ticker, "order_id", order.ID,
								"option_symbol", opt.Symbol, "strike", opt.Strike,
								"expiry", opt.Expiry, "limit", opt.LimitPrice, "dte", opt.DTE,
							)
							cycleOrders = append(cycleOrders, cycleOrder{
								ticker: ticker, optType: "call", symbol: opt.Symbol,
								strike: opt.Strike, expiry: opt.Expiry.String(),
								bidPrice: opt.LimitPrice, contracts: 1, orderID: order.ID,
							})
							roundPlaced++
						}
					}
				} else {
					co, cerr := e.placeCalls(ticker, uncoveredLots, costBasis, params)
					if cerr != nil {
						e.log.Warn("covered call skipped", "ticker", ticker, "error", cerr)
					} else {
						cycleOrders = append(cycleOrders, *co)
						roundPlaced++
					}
				}
				continue
			}

			// ── Put path ───────────────────────────────────────────────────────
			putsToAdd := sym.Contracts - existingPuts
			if putsToAdd <= 0 {
				e.log.Info("removing from pool: puts at target",
					"ticker", ticker, "existing", existingPuts, "target", sym.Contracts)
				activitySkips = append(activitySkips, ticker)
				continue
			}

			if sym.Ladder {
				opts, lerr := e.sel.SelectPutLadder(ticker, putsToAdd, params)
				if lerr != nil {
					e.log.Warn("put ladder skipped", "ticker", ticker, "error", lerr)
					continue
				}
				anyPlaced := false
				for _, opt := range opts {
					// Per-contract cash guard check for each ladder leg.
					obligation := opt.Strike * 100
					exposure := existingPutExposure(positions, orders, cycleOrders)
					cash, _ := acct.Cash.Float64()
					effectiveCash := cash * (1 - e.cfg.Trading.CashReservePct)
					if effectiveCash-exposure < obligation {
						skips = append(skips, cashGuardSkip{Ticker: ticker, Strike: opt.Strike, Obligation: obligation})
						e.log.Info("ladder put blocked by cash guard",
							"ticker", ticker, "expiry", opt.Expiry, "strike", opt.Strike)
						break
					}
					order, oerr := e.bc.PlaceOptionOrder(opt.Symbol, 1, opt.LimitPrice)
					if oerr != nil {
						e.log.Warn("ladder put order failed", "ticker", ticker, "expiry", opt.Expiry, "error", oerr)
						break
					}
					e.log.Info("ladder put order placed",
						"ticker", ticker, "order_id", order.ID,
						"option_symbol", opt.Symbol, "strike", opt.Strike,
						"expiry", opt.Expiry, "limit", opt.LimitPrice, "dte", opt.DTE,
					)
					cycleOrders = append(cycleOrders, cycleOrder{
						ticker: ticker, optType: "put", symbol: opt.Symbol,
						strike: opt.Strike, expiry: opt.Expiry.String(),
						bidPrice: opt.LimitPrice, contracts: 1, orderID: order.ID,
					})
					roundPlaced++
					anyPlaced = true
				}
				if anyPlaced {
					nextRound = append(nextRound, sym)
				}
				continue
			}

			co, skip, err := e.placePut(ticker, putsToAdd, params, acct, positions, orders, cycleOrders)
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
			// Stay in pool: next round the updated count may still be below target
			// (e.g. cash guard capped this placement below requested).
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

// countExistingContracts returns the total number of contracts of the given OCC
// type ("P" or "C") already open for ticker, including live positions, pending
// sell-to-open orders, and orders placed earlier in the current cycle.
// cycleType is the matching optType string used in cycleOrder ("put" or "call").
func countExistingContracts(
	ticker, occType, cycleType string,
	positions []alpaca.Position,
	orders []alpaca.Order,
	cycle []cycleOrder,
) int {
	count := 0
	for _, p := range positions {
		root, _, ot, _, err := options.ParseSymbol(p.Symbol)
		if err != nil || root != ticker || ot != occType {
			continue
		}
		qty, _ := p.Qty.Float64()
		count += int(math.Round(math.Abs(qty)))
	}
	for _, o := range orders {
		if o.PositionIntent != alpaca.SellToOpen {
			continue
		}
		root, _, ot, _, err := options.ParseSymbol(o.Symbol)
		if err != nil || root != ticker || ot != occType {
			continue
		}
		if o.Qty != nil {
			qty, _ := o.Qty.Float64()
			count += int(math.Round(math.Abs(qty)))
		}
	}
	for _, co := range cycle {
		if co.ticker == ticker && co.optType == cycleType {
			count += co.contracts
		}
	}
	return count
}

// selectionParams builds a SelectionParams from the trading-level defaults merged
// with any symbol-level overrides (bid_ask_range, min_delta, max_delta).
func (e *Engine) selectionParams(sym config.Symbol) options.SelectionParams {
	bidAskRange := e.cfg.Trading.BidAskRange
	if sym.BidAskRange != nil {
		bidAskRange = *sym.BidAskRange
	}
	minDelta := e.cfg.Trading.MinDelta
	if sym.MinDelta != nil {
		minDelta = *sym.MinDelta
	}
	maxDelta := e.cfg.Trading.MaxDelta
	if sym.MaxDelta != nil {
		maxDelta = *sym.MaxDelta
	}
	return options.SelectionParams{
		ScanDays:        e.cfg.Trading.ScanDays,
		MaxDTE:          e.cfg.Trading.MaxDTE,
		MinPremiumPct:   e.cfg.Trading.MinPremiumPct,
		MinPremiumPrice: e.cfg.Trading.MinPremiumPrice,
		MinDelta:        minDelta,
		MaxDelta:        maxDelta,
		BidAskRange:     bidAskRange,
	}
}

// checkEarlyClose scans open short option positions and places a buy-to-close
// limit order for any that have reached the 50% profit target. Positions with
// an existing BTC order are skipped to avoid duplicates.
func (e *Engine) checkEarlyClose(positions []alpaca.Position, orders []alpaca.Order) {
	const profitTarget = 0.50

	// Build a set of symbols that already have a pending BTC order.
	pendingBTC := make(map[string]bool)
	for _, o := range orders {
		if o.PositionIntent == alpaca.BuyToClose {
			pendingBTC[o.Symbol] = true
		}
	}

	for _, p := range positions {
		// Only consider option positions (ParseSymbol succeeds).
		if _, _, _, _, err := options.ParseSymbol(p.Symbol); err != nil {
			continue
		}
		if pendingBTC[p.Symbol] {
			continue
		}
		if p.CurrentPrice == nil {
			continue
		}

		openingCredit := math.Abs(p.AvgEntryPrice.InexactFloat64())
		currentMark := math.Abs(p.CurrentPrice.InexactFloat64())
		if openingCredit <= 0 {
			continue
		}

		profitPct := 1 - (currentMark / openingCredit)
		if profitPct < profitTarget {
			continue
		}

		qty, _ := p.Qty.Float64()
		contracts := int(math.Round(math.Abs(qty)))
		if contracts <= 0 {
			continue
		}

		e.log.Info("early close target reached",
			"symbol", p.Symbol,
			"opening_credit", openingCredit,
			"current_mark", currentMark,
			"profit_pct", profitPct,
		)
		if _, err := e.bc.PlaceBuyToClose(p.Symbol, contracts, currentMark); err != nil {
			e.log.Warn("buy-to-close order failed", "symbol", p.Symbol, "error", err)
		}
	}
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
