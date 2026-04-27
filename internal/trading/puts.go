package trading

import (
	"math"

	"github.com/alpacahq/alpaca-trade-api-go/v3/alpaca"

	"github.com/enork/alpaca-trader/internal/notify"
	"github.com/enork/alpaca-trader/internal/options"
)

// newPut records a put order placed during the current cycle so that subsequent
// cash-guard checks and open-activity checks are accurate even though the orders
// list was fetched before any orders were submitted.
type newPut struct {
	ticker string
	strike float64
}

type cashGuardSkip struct {
	Ticker     string
	Strike     float64
	Obligation float64
}

// placedThisCycle returns true if a put for ticker was already placed in this cycle.
func placedThisCycle(ticker string, placed []newPut) bool {
	for _, p := range placed {
		if p.ticker == ticker {
			return true
		}
	}
	return false
}

// placePut selects and places a single cash-secured put for ticker.
// placed contains any puts already submitted this cycle and is used so the
// cash guard accounts for obligations not yet visible in the orders snapshot.
// Returns (strike, nil, nil) on success; (0, skip, nil) when cash guard fires;
// (0, nil, err) for any other failure.
func (e *Engine) placePut(
	ticker string,
	acct *alpaca.Account,
	positions []alpaca.Position,
	orders []alpaca.Order,
	placed []newPut,
) (float64, *cashGuardSkip, error) {
	opt, err := e.sel.SelectPut(ticker, e.cfg.Trading.MaxDTE)
	if err != nil {
		return 0, nil, err
	}

	obligation := opt.Strike * 100
	exposure := existingPutExposure(positions, orders, placed)
	cash, _ := acct.Cash.Float64()

	if cash < exposure+obligation {
		e.log.Warn("cash guard blocked put",
			"ticker", ticker,
			"strike", opt.Strike,
			"obligation", obligation,
			"existing_exposure", exposure,
			"cash", cash,
		)
		return 0, &cashGuardSkip{Ticker: ticker, Strike: opt.Strike, Obligation: obligation}, nil
	}

	order, err := e.bc.PlaceOptionOrder(opt.Symbol, 1, opt.BidPrice)
	if err != nil {
		return 0, nil, err
	}

	e.log.Info("put order placed",
		"ticker", ticker,
		"order_id", order.ID,
		"option_symbol", opt.Symbol,
		"strike", opt.Strike,
		"expiry", opt.Expiry,
		"bid", opt.BidPrice,
	)
	return opt.Strike, nil, nil
}

// existingPutExposure returns the total notional obligation (strike × 100 × contracts)
// of all open short put positions, pending sell-to-open put orders, and any puts
// placed earlier in the current cycle.
func existingPutExposure(positions []alpaca.Position, orders []alpaca.Order, placed []newPut) float64 {
	var total float64
	for _, p := range positions {
		_, _, optType, strike, err := options.ParseSymbol(p.Symbol)
		if err != nil || optType != "P" {
			continue
		}
		qty, _ := p.Qty.Float64()
		total += strike * 100 * math.Abs(qty)
	}
	for _, o := range orders {
		if o.PositionIntent != alpaca.SellToOpen {
			continue
		}
		_, _, optType, strike, err := options.ParseSymbol(o.Symbol)
		if err != nil || optType != "P" {
			continue
		}
		if o.Qty != nil {
			qty, _ := o.Qty.Float64()
			total += strike * 100 * qty
		}
	}
	for _, p := range placed {
		total += p.strike * 100
	}
	return total
}

// buildCashGuardAlert assembles the alert payload from skipped symbols and account state.
func (e *Engine) buildCashGuardAlert(acct *alpaca.Account, positions []alpaca.Position, placed []newPut, skips []cashGuardSkip) notify.CashGuardAlert {
	cash, _ := acct.Cash.Float64()
	exposure := existingPutExposure(positions, nil, placed)

	var totalObligation float64
	tickers := make([]string, 0, len(skips))
	for _, s := range skips {
		totalObligation += s.Obligation
		tickers = append(tickers, s.Ticker)
	}

	additionalNeeded := exposure + totalObligation - cash
	if additionalNeeded < 0 {
		additionalNeeded = 0
	}

	return notify.CashGuardAlert{
		SkippedTickers:   tickers,
		Cash:             cash,
		ExistingExposure: exposure,
		AdditionalTotal:  additionalNeeded,
		AdditionalPerPut: additionalNeeded / float64(len(skips)),
	}
}

// logCashGuardSummary writes a structured log entry for a cash guard alert.
func (e *Engine) logCashGuardSummary(a notify.CashGuardAlert) {
	e.log.Warn("cash guard summary",
		"skipped_tickers", a.SkippedTickers,
		"cash", a.Cash,
		"existing_put_exposure", a.ExistingExposure,
		"additional_cash_needed_total", a.AdditionalTotal,
		"additional_cash_needed_per_put", a.AdditionalPerPut,
	)
}
