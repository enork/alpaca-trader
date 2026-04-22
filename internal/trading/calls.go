package trading

func (e *Engine) placeCalls(ticker string, contracts int, costBasis float64) error {
	opt, err := e.sel.SelectCall(ticker, costBasis, e.cfg.Trading.MaxDTE)
	if err != nil {
		return err
	}

	order, err := e.bc.PlaceOptionOrder(opt.Symbol, contracts, opt.BidPrice)
	if err != nil {
		return err
	}

	e.log.Info("covered call order placed",
		"ticker", ticker,
		"order_id", order.ID,
		"option_symbol", opt.Symbol,
		"strike", opt.Strike,
		"expiry", opt.Expiry,
		"bid", opt.BidPrice,
		"contracts", contracts,
	)
	return nil
}
