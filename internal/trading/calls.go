package trading

import "github.com/enork/alpaca-trader/internal/options"

func (e *Engine) placeCalls(ticker string, contracts int, costBasis float64, params options.SelectionParams) (*cycleOrder, error) {
	opt, err := e.sel.SelectCall(ticker, costBasis, params)
	if err != nil {
		return nil, err
	}

	order, err := e.bc.PlaceOptionOrder(opt.Symbol, contracts, opt.LimitPrice)
	if err != nil {
		return nil, err
	}

	e.log.Info("covered call order placed",
		"ticker", ticker, "order_id", order.ID,
		"option_symbol", opt.Symbol, "strike", opt.Strike, "expiry", opt.Expiry,
		"bid", opt.BidPrice, "limit", opt.LimitPrice, "dte", opt.DTE,
		"contracts", contracts,
	)

	return &cycleOrder{
		ticker:    ticker,
		optType:   "call",
		symbol:    opt.Symbol,
		strike:    opt.Strike,
		expiry:    opt.Expiry.String(),
		bidPrice:  opt.LimitPrice,
		contracts: contracts,
		orderID:   order.ID,
	}, nil
}
