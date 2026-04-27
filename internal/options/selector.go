package options

import (
	"fmt"
	"log/slog"
	"sort"
	"strconv"
	"time"
	"unicode"

	"cloud.google.com/go/civil"
	"github.com/alpacahq/alpaca-trade-api-go/v3/marketdata"
	"github.com/enork/alpaca-trader/internal/broker"
)

// maxScanDays is the calendar-day look-ahead used to discover available expiry dates.
const maxScanDays = 30

// Option holds the details of a selected option contract.
type Option struct {
	Symbol   string
	Type     string // "call" or "put"
	Strike   float64
	Expiry   civil.Date
	BidPrice float64
}

// Selector queries option chains and applies strike-selection algorithms.
type Selector struct {
	bc  *broker.Client
	log *slog.Logger
}

// New returns a Selector backed by the given broker client.
func New(bc *broker.Client, log *slog.Logger) *Selector {
	return &Selector{bc: bc, log: log}
}

// SelectCall returns the best covered-call contract for the given ticker.
// Only strikes >= costBasis are considered to avoid locking in a loss.
// Efficiency metric: bidPrice / (strike - costBasis + 1)
func (s *Selector) SelectCall(ticker string, costBasis float64, maxDTE int) (*Option, error) {
	today := civil.DateOf(time.Now())
	scanCutoff := civil.DateOf(time.Now().AddDate(0, 0, maxScanDays))

	s.log.Debug("querying call chain", "ticker", ticker, "scan_days", maxScanDays, "cost_basis", costBasis)

	snapshots, err := s.bc.GetOptionChain(ticker, marketdata.Call, today, scanCutoff)
	if err != nil {
		return nil, err
	}

	allowed := nearestExpiries(snapshots, maxDTE)
	if len(allowed) == 0 {
		return nil, fmt.Errorf("no call expiry dates found for %s in the next %d days", ticker, maxScanDays)
	}
	s.log.Info("discovered call expiry dates", "ticker", ticker, "expiries", sortedDates(allowed))

	var best *Option
	var bestScore float64
	evaluated, skippedNoQuote, skippedBelowBasis, skippedExpiry := 0, 0, 0, 0

	for sym, snap := range snapshots {
		if snap.LatestQuote == nil || snap.LatestQuote.BidPrice <= 0 {
			skippedNoQuote++
			continue
		}
		_, expiry, optType, strike, err := ParseSymbol(sym)
		if err != nil {
			continue
		}
		if optType != "C" {
			continue
		}
		if _, ok := allowed[expiry]; !ok {
			skippedExpiry++
			continue
		}
		evaluated++
		if strike < costBasis {
			skippedBelowBasis++
			continue
		}
		bid := snap.LatestQuote.BidPrice
		score := bid / (strike - costBasis + 1)
		if best == nil || score > bestScore {
			best = &Option{
				Symbol:   sym,
				Type:     "call",
				Strike:   strike,
				Expiry:   expiry,
				BidPrice: bid,
			}
			bestScore = score
		}
	}

	s.log.Debug("call selection complete",
		"ticker", ticker,
		"evaluated", evaluated,
		"skipped_no_quote", skippedNoQuote,
		"skipped_below_basis", skippedBelowBasis,
		"skipped_wrong_expiry", skippedExpiry,
	)

	if best == nil {
		return nil, fmt.Errorf("no qualifying call found for %s (cost basis %.2f, max_dte %d)", ticker, costBasis, maxDTE)
	}
	s.log.Info("selected call", "ticker", ticker, "symbol", best.Symbol, "strike", best.Strike, "expiry", best.Expiry, "bid", best.BidPrice)
	return best, nil
}

// SelectPut returns the best cash-secured put contract for the given ticker.
// Only OTM strikes (< current market price) are considered.
// Efficiency metric: bidPrice / strike
func (s *Selector) SelectPut(ticker string, maxDTE int) (*Option, error) {
	currentPrice, err := s.bc.GetLatestPrice(ticker)
	if err != nil {
		return nil, err
	}

	today := civil.DateOf(time.Now())
	scanCutoff := civil.DateOf(time.Now().AddDate(0, 0, maxScanDays))

	s.log.Debug("querying put chain", "ticker", ticker, "price", currentPrice, "scan_days", maxScanDays)

	snapshots, err := s.bc.GetOptionChain(ticker, marketdata.Put, today, scanCutoff)
	if err != nil {
		return nil, err
	}

	allowed := nearestExpiries(snapshots, maxDTE)
	if len(allowed) == 0 {
		return nil, fmt.Errorf("no put expiry dates found for %s in the next %d days", ticker, maxScanDays)
	}
	s.log.Info("discovered put expiry dates", "ticker", ticker, "expiries", sortedDates(allowed))

	var best *Option
	var bestScore float64
	evaluated, skippedNoQuote, skippedITM, skippedExpiry := 0, 0, 0, 0

	for sym, snap := range snapshots {
		if snap.LatestQuote == nil || snap.LatestQuote.BidPrice <= 0 {
			skippedNoQuote++
			continue
		}
		_, expiry, optType, strike, err := ParseSymbol(sym)
		if err != nil {
			continue
		}
		if optType != "P" {
			continue
		}
		if _, ok := allowed[expiry]; !ok {
			skippedExpiry++
			continue
		}
		evaluated++
		if strike >= currentPrice {
			skippedITM++
			continue
		}
		bid := snap.LatestQuote.BidPrice
		score := bid / strike
		if best == nil || score > bestScore {
			best = &Option{
				Symbol:   sym,
				Type:     "put",
				Strike:   strike,
				Expiry:   expiry,
				BidPrice: bid,
			}
			bestScore = score
		}
	}

	s.log.Debug("put selection complete",
		"ticker", ticker,
		"evaluated", evaluated,
		"skipped_no_quote", skippedNoQuote,
		"skipped_itm", skippedITM,
		"skipped_wrong_expiry", skippedExpiry,
	)

	if best == nil {
		return nil, fmt.Errorf("no qualifying put found for %s (price %.2f, max_dte %d)", ticker, currentPrice, maxDTE)
	}
	s.log.Info("selected put", "ticker", ticker, "symbol", best.Symbol, "strike", best.Strike, "expiry", best.Expiry, "bid", best.BidPrice)
	return best, nil
}

// nearestExpiries scans the symbols in snapshots, collects unique expiry dates,
// sorts them ascending, and returns the first n as a set. Only expiry dates
// that actually have contracts with a positive bid are included.
func nearestExpiries(snapshots map[string]marketdata.OptionSnapshot, n int) map[civil.Date]struct{} {
	seen := make(map[civil.Date]struct{})
	for sym, snap := range snapshots {
		if snap.LatestQuote == nil || snap.LatestQuote.BidPrice <= 0 {
			continue
		}
		_, expiry, _, _, err := ParseSymbol(sym)
		if err != nil {
			continue
		}
		seen[expiry] = struct{}{}
	}

	dates := make([]civil.Date, 0, len(seen))
	for d := range seen {
		dates = append(dates, d)
	}
	sort.Slice(dates, func(i, j int) bool { return dates[i].Before(dates[j]) })

	result := make(map[civil.Date]struct{}, n)
	for i := 0; i < n && i < len(dates); i++ {
		result[dates[i]] = struct{}{}
	}
	return result
}

// sortedDates returns expiry dates from a set as a sorted slice of strings,
// used for structured logging.
func sortedDates(m map[civil.Date]struct{}) []string {
	dates := make([]civil.Date, 0, len(m))
	for d := range m {
		dates = append(dates, d)
	}
	sort.Slice(dates, func(i, j int) bool { return dates[i].Before(dates[j]) })
	out := make([]string, len(dates))
	for i, d := range dates {
		out[i] = d.String()
	}
	return out
}

// ParseSymbol decodes an OCC option symbol (e.g. PLUG250117C00003000) into its components.
// Strike is returned in dollars (8-digit field: 5 integer digits + 3 decimal digits).
func ParseSymbol(sym string) (root string, expiry civil.Date, optType string, strike float64, err error) {
	i := 0
	for i < len(sym) && !unicode.IsDigit(rune(sym[i])) {
		i++
	}
	if len(sym) < i+15 {
		return "", civil.Date{}, "", 0, fmt.Errorf("symbol too short: %s", sym)
	}

	root = sym[:i]

	year, e1 := strconv.Atoi("20" + sym[i:i+2])
	month, e2 := strconv.Atoi(sym[i+2 : i+4])
	day, e3 := strconv.Atoi(sym[i+4 : i+6])
	if e1 != nil || e2 != nil || e3 != nil {
		return "", civil.Date{}, "", 0, fmt.Errorf("bad date in symbol: %s", sym)
	}
	expiry = civil.Date{Year: year, Month: time.Month(month), Day: day}

	optType = string(sym[i+6])

	strikeInt, e4 := strconv.Atoi(sym[i+7 : i+15])
	if e4 != nil {
		return "", civil.Date{}, "", 0, fmt.Errorf("bad strike in symbol: %s", sym)
	}
	strike = float64(strikeInt) / 1000.0

	return root, expiry, optType, strike, nil
}
