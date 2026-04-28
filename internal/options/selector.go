package options

import (
	"fmt"
	"log/slog"
	"math"
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

// SelectionParams controls how options are filtered, scored, and priced.
type SelectionParams struct {
	ScanDays int // calendar-day window to search for expiry dates (e.g. 30)
	// MaxDTE is the maximum number of distinct expiry dates to consider (e.g. 3 = nearest 3 Fridays).
	MaxDTE int
	// MinPremiumPct is the minimum annualised return as a decimal (e.g. 0.15 = 15%).
	// Takes precedence over MinPremiumPrice. 0 = disabled.
	MinPremiumPct float64
	// MinPremiumPrice is the minimum flat bid price per share. 0 = disabled.
	// Ignored when MinPremiumPct > 0.
	MinPremiumPrice float64
	// MinDelta / MaxDelta are absolute-value delta bounds (0 = disabled).
	// For puts, delta is negative in the market but config uses absolute values (e.g. 0.20–0.35).
	MinDelta float64
	MaxDelta float64
	// BidAskRange controls limit-order price: 0.0 = bid, 1.0 = ask, 0.5 = midpoint.
	BidAskRange float64
}

// Option holds the details of a selected option contract.
type Option struct {
	Symbol     string
	Type       string // "call" or "put"
	Strike     float64
	Expiry     civil.Date
	DTE        int
	BidPrice   float64
	AskPrice   float64
	LimitPrice float64 // interpolated from BidAskRange
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

// ── Public selection methods ──────────────────────────────────────────────────

// SelectPut returns the single best cash-secured put for ticker.
// Efficiency metric: annualised (bid / strike) * (365 / DTE).
func (s *Selector) SelectPut(ticker string, params SelectionParams) (*Option, error) {
	currentPrice, err := s.bc.GetLatestPrice(ticker)
	if err != nil {
		return nil, err
	}

	today := civil.DateOf(time.Now())
	snapshots, err := s.bc.GetOptionChain(ticker, marketdata.Put, today, civil.DateOf(time.Now().AddDate(0, 0, maxScanDays)))
	if err != nil {
		return nil, err
	}

	allowed := nearestExpiries(snapshots, params.MaxDTE)
	if len(allowed) == 0 {
		return nil, fmt.Errorf("no put expiry dates found for %s in the next %d days", ticker, maxScanDays)
	}
	s.log.Info("discovered put expiry dates", "ticker", ticker, "expiries", sortedDates(allowed))

	best, stats := selectBestPutInSet(snapshots, allowed, currentPrice, params, today)
	s.log.Debug("put selection complete",
		"ticker", ticker, "evaluated", stats.evaluated,
		"skipped_no_quote", stats.skippedNoQuote, "skipped_itm", stats.skippedITM,
		"skipped_wrong_expiry", stats.skippedExpiry,
		"skipped_min_premium", stats.skippedPremium, "skipped_delta", stats.skippedDelta,
	)
	if best == nil {
		return nil, fmt.Errorf("no qualifying put found for %s (price %.2f, max_dte %d)", ticker, currentPrice, params.MaxDTE)
	}
	s.log.Info("selected put", "ticker", ticker, "symbol", best.Symbol,
		"strike", best.Strike, "expiry", best.Expiry,
		"bid", best.BidPrice, "limit", best.LimitPrice, "dte", best.DTE)
	return best, nil
}

// SelectPutLadder returns one put per expiry date (up to maxCount), best contract
// within each window. Used when sym.Ladder = true to spread contracts across dates.
func (s *Selector) SelectPutLadder(ticker string, maxCount int, params SelectionParams) ([]*Option, error) {
	currentPrice, err := s.bc.GetLatestPrice(ticker)
	if err != nil {
		return nil, err
	}

	today := civil.DateOf(time.Now())
	snapshots, err := s.bc.GetOptionChain(ticker, marketdata.Put, today, civil.DateOf(time.Now().AddDate(0, 0, maxScanDays)))
	if err != nil {
		return nil, err
	}

	expiries := nearestExpiriesSorted(snapshots, params.MaxDTE)
	if len(expiries) == 0 {
		return nil, fmt.Errorf("no put expiry dates found for %s in the next %d days", ticker, maxScanDays)
	}
	s.log.Info("discovered put expiry dates (ladder)", "ticker", ticker, "expiries", datesToStrings(expiries))

	var results []*Option
	for _, exp := range expiries {
		if len(results) >= maxCount {
			break
		}
		allowed := map[civil.Date]struct{}{exp: {}}
		best, _ := selectBestPutInSet(snapshots, allowed, currentPrice, params, today)
		if best != nil {
			results = append(results, best)
		}
	}
	if len(results) == 0 {
		return nil, fmt.Errorf("no qualifying put contracts for ladder %s (price %.2f, max_dte %d)", ticker, currentPrice, params.MaxDTE)
	}
	return results, nil
}

// SelectCall returns the single best covered-call contract for ticker.
// Only strikes >= costBasis are considered.
// Efficiency metric: annualised bid / (strike - costBasis + 1).
func (s *Selector) SelectCall(ticker string, costBasis float64, params SelectionParams) (*Option, error) {
	today := civil.DateOf(time.Now())
	snapshots, err := s.bc.GetOptionChain(ticker, marketdata.Call, today, civil.DateOf(time.Now().AddDate(0, 0, maxScanDays)))
	if err != nil {
		return nil, err
	}

	allowed := nearestExpiries(snapshots, params.MaxDTE)
	if len(allowed) == 0 {
		return nil, fmt.Errorf("no call expiry dates found for %s in the next %d days", ticker, maxScanDays)
	}
	s.log.Info("discovered call expiry dates", "ticker", ticker, "expiries", sortedDates(allowed))

	best, stats := selectBestCallInSet(snapshots, allowed, costBasis, params, today)
	s.log.Debug("call selection complete",
		"ticker", ticker, "evaluated", stats.evaluated,
		"skipped_no_quote", stats.skippedNoQuote, "skipped_below_basis", stats.skippedBelowBasis,
		"skipped_wrong_expiry", stats.skippedExpiry,
		"skipped_min_premium", stats.skippedPremium, "skipped_delta", stats.skippedDelta,
	)
	if best == nil {
		return nil, fmt.Errorf("no qualifying call found for %s (cost_basis %.2f, max_dte %d)", ticker, costBasis, params.MaxDTE)
	}
	s.log.Info("selected call", "ticker", ticker, "symbol", best.Symbol,
		"strike", best.Strike, "expiry", best.Expiry,
		"bid", best.BidPrice, "limit", best.LimitPrice, "dte", best.DTE)
	return best, nil
}

// SelectCallLadder returns one call per expiry date (up to maxCount).
func (s *Selector) SelectCallLadder(ticker string, maxCount int, costBasis float64, params SelectionParams) ([]*Option, error) {
	today := civil.DateOf(time.Now())
	snapshots, err := s.bc.GetOptionChain(ticker, marketdata.Call, today, civil.DateOf(time.Now().AddDate(0, 0, maxScanDays)))
	if err != nil {
		return nil, err
	}

	expiries := nearestExpiriesSorted(snapshots, params.MaxDTE)
	if len(expiries) == 0 {
		return nil, fmt.Errorf("no call expiry dates found for %s in the next %d days", ticker, maxScanDays)
	}
	s.log.Info("discovered call expiry dates (ladder)", "ticker", ticker, "expiries", datesToStrings(expiries))

	var results []*Option
	for _, exp := range expiries {
		if len(results) >= maxCount {
			break
		}
		allowed := map[civil.Date]struct{}{exp: {}}
		best, _ := selectBestCallInSet(snapshots, allowed, costBasis, params, today)
		if best != nil {
			results = append(results, best)
		}
	}
	if len(results) == 0 {
		return nil, fmt.Errorf("no qualifying call contracts for ladder %s (cost_basis %.2f, max_dte %d)", ticker, costBasis, params.MaxDTE)
	}
	return results, nil
}

// ── Internal selection helpers ────────────────────────────────────────────────

type selectionStats struct {
	evaluated         int
	skippedNoQuote    int
	skippedExpiry     int
	skippedITM        int
	skippedBelowBasis int
	skippedPremium    int
	skippedDelta      int
}

// selectBestPutInSet finds the highest-scoring OTM put across the allowed expiry set.
// Score = annualised yield: (bid / strike) * (365 / DTE).
func selectBestPutInSet(
	snapshots map[string]marketdata.OptionSnapshot,
	allowed map[civil.Date]struct{},
	currentPrice float64,
	params SelectionParams,
	today civil.Date,
) (*Option, selectionStats) {
	var stats selectionStats
	var best *Option
	var bestScore float64

	for sym, snap := range snapshots {
		if snap.LatestQuote == nil || snap.LatestQuote.BidPrice <= 0 {
			stats.skippedNoQuote++
			continue
		}
		_, expiry, optType, strike, err := ParseSymbol(sym)
		if err != nil || optType != "P" {
			continue
		}
		if _, ok := allowed[expiry]; !ok {
			stats.skippedExpiry++
			continue
		}
		if strike >= currentPrice {
			stats.skippedITM++
			continue
		}
		stats.evaluated++

		bid := snap.LatestQuote.BidPrice
		ask := snap.LatestQuote.AskPrice
		dte := daysUntil(today, expiry)

		// Annualised return — used for both the min-premium filter and the score (#2, #3).
		annualReturn := (bid / strike) * (365.0 / float64(dte))

		// Minimum premium filter (#3): percentage takes precedence over flat price.
		if params.MinPremiumPct > 0 {
			if annualReturn < params.MinPremiumPct {
				stats.skippedPremium++
				continue
			}
		} else if params.MinPremiumPrice > 0 {
			if bid < params.MinPremiumPrice {
				stats.skippedPremium++
				continue
			}
		}

		// Delta filter (#4): config values are absolute (e.g. 0.20–0.35).
		if snap.Greeks != nil && (params.MinDelta > 0 || params.MaxDelta > 0) {
			absDelta := math.Abs(snap.Greeks.Delta)
			if params.MinDelta > 0 && absDelta < params.MinDelta {
				stats.skippedDelta++
				continue
			}
			if params.MaxDelta > 0 && absDelta > params.MaxDelta {
				stats.skippedDelta++
				continue
			}
		}

		if best == nil || annualReturn > bestScore {
			best = &Option{
				Symbol:     sym,
				Type:       "put",
				Strike:     strike,
				Expiry:     expiry,
				DTE:        dte,
				BidPrice:   bid,
				AskPrice:   ask,
				LimitPrice: computeLimitPrice(bid, ask, params.BidAskRange),
			}
			bestScore = annualReturn
		}
	}
	return best, stats
}

// selectBestCallInSet finds the highest-scoring covered call across the allowed expiry set.
// Score = annualised yield: (bid / (strike - costBasis + 1)) * (365 / DTE).
func selectBestCallInSet(
	snapshots map[string]marketdata.OptionSnapshot,
	allowed map[civil.Date]struct{},
	costBasis float64,
	params SelectionParams,
	today civil.Date,
) (*Option, selectionStats) {
	var stats selectionStats
	var best *Option
	var bestScore float64

	for sym, snap := range snapshots {
		if snap.LatestQuote == nil || snap.LatestQuote.BidPrice <= 0 {
			stats.skippedNoQuote++
			continue
		}
		_, expiry, optType, strike, err := ParseSymbol(sym)
		if err != nil || optType != "C" {
			continue
		}
		if _, ok := allowed[expiry]; !ok {
			stats.skippedExpiry++
			continue
		}
		if strike < costBasis {
			stats.skippedBelowBasis++
			continue
		}
		stats.evaluated++

		bid := snap.LatestQuote.BidPrice
		ask := snap.LatestQuote.AskPrice
		dte := daysUntil(today, expiry)

		// Annualised return using strike as reference price (#2).
		annualReturn := (bid / strike) * (365.0 / float64(dte))

		// Minimum premium filter (#3).
		if params.MinPremiumPct > 0 {
			if annualReturn < params.MinPremiumPct {
				stats.skippedPremium++
				continue
			}
		} else if params.MinPremiumPrice > 0 {
			if bid < params.MinPremiumPrice {
				stats.skippedPremium++
				continue
			}
		}

		// Delta filter (#4): call delta is positive; use absolute value for consistency.
		if snap.Greeks != nil && (params.MinDelta > 0 || params.MaxDelta > 0) {
			absDelta := math.Abs(snap.Greeks.Delta)
			if params.MinDelta > 0 && absDelta < params.MinDelta {
				stats.skippedDelta++
				continue
			}
			if params.MaxDelta > 0 && absDelta > params.MaxDelta {
				stats.skippedDelta++
				continue
			}
		}

		// Score uses the call-specific efficiency formula (distance above cost basis).
		score := (bid / (strike - costBasis + 1)) * (365.0 / float64(dte))
		if best == nil || score > bestScore {
			best = &Option{
				Symbol:     sym,
				Type:       "call",
				Strike:     strike,
				Expiry:     expiry,
				DTE:        dte,
				BidPrice:   bid,
				AskPrice:   ask,
				LimitPrice: computeLimitPrice(bid, ask, params.BidAskRange),
			}
			bestScore = score
		}
	}
	return best, stats
}

// ── Utility helpers ───────────────────────────────────────────────────────────

// computeLimitPrice interpolates between bid and ask using bidAskRange ∈ [0,1].
// Falls back to bid if ask is unavailable or the spread is inverted.
func computeLimitPrice(bid, ask, bidAskRange float64) float64 {
	if ask <= bid {
		return bid
	}
	return bid + (ask-bid)*bidAskRange
}

// daysUntil returns calendar days from today to expiry, minimum 1.
func daysUntil(today, expiry civil.Date) int {
	t := time.Date(expiry.Year, expiry.Month, expiry.Day, 0, 0, 0, 0, time.UTC)
	n := time.Date(today.Year, today.Month, today.Day, 0, 0, 0, 0, time.UTC)
	dte := int(t.Sub(n).Hours() / 24)
	if dte < 1 {
		return 1
	}
	return dte
}

// nearestExpiries returns the n nearest expiry dates with positive-bid contracts as a set.
func nearestExpiries(snapshots map[string]marketdata.OptionSnapshot, n int) map[civil.Date]struct{} {
	sorted := nearestExpiriesSorted(snapshots, n)
	result := make(map[civil.Date]struct{}, len(sorted))
	for _, d := range sorted {
		result[d] = struct{}{}
	}
	return result
}

// nearestExpiriesSorted returns the n nearest expiry dates as a sorted slice.
func nearestExpiriesSorted(snapshots map[string]marketdata.OptionSnapshot, n int) []civil.Date {
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
	if n < len(dates) {
		return dates[:n]
	}
	return dates
}

// sortedDates returns expiry dates from a set as sorted strings for logging.
func sortedDates(m map[civil.Date]struct{}) []string {
	dates := make([]civil.Date, 0, len(m))
	for d := range m {
		dates = append(dates, d)
	}
	sort.Slice(dates, func(i, j int) bool { return dates[i].Before(dates[j]) })
	return datesToStrings(dates)
}

// datesToStrings converts a slice of civil.Date to a slice of strings.
func datesToStrings(dates []civil.Date) []string {
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
