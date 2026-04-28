package main

import (
	"encoding/json"
	"fmt"
	"html/template"
	"math"
	"os"
	"sort"
	"time"

	"github.com/enork/alpaca-trader/internal/broker"
	"github.com/enork/alpaca-trader/internal/config"
	"github.com/enork/alpaca-trader/internal/options"
)

// ── Data structures ───────────────────────────────────────────────────────────

// ReportData is the top-level template context.
type ReportData struct {
	GeneratedAt   time.Time
	Days          int
	PaperTrading  bool
	AccountNumber string

	// Current account snapshot
	PortfolioValue  float64
	Equity          float64
	Cash            float64
	BuyingPower     float64
	LongMarketValue float64
	DayPL           float64

	// Period-level figures (from portfolio history)
	PeriodPL    float64
	PeriodPLPct float64

	// Chart payloads — template.JS prevents HTML-escaping of embedded JSON
	HistoryJSON      template.JS
	PremiumChartJSON template.JS

	// Premium analytics
	TotalPremium      float64
	PremiumTradeCount int
	PremiumBySymbol   []SymbolPremium

	// Tables
	Positions  []ReportPosition
	Activities []ReportActivity

	// Navigation
	OtherReports []ReportLink
}

// SymbolPremium holds per-ticker premium aggregates.
type SymbolPremium struct {
	Symbol  string
	Premium float64
	Count   int
	Pct     float64 // share of |total| for the period
}

// ReportPosition is a single open position row.
type ReportPosition struct {
	Symbol          string
	IsOption        bool
	OptionSide      string // "PUT" or "CALL"
	Strike          float64
	Expiry          string
	Qty             float64
	AvgEntryPrice   float64
	CurrentPrice    float64
	MarketValue     float64
	UnrealizedPL    float64
	UnrealizedPLPct float64
}

// ReportActivity is a single activity-feed row.
type ReportActivity struct {
	Time         string
	Type         string
	Symbol       string
	Side         string
	Qty          float64
	Price        float64
	NetAmount    float64
	Description  string
	IsOptionFill bool
}

// ReportLink is a navigation link to a previous report.
type ReportLink struct {
	Date string
	Path string
}

// historyPoint is the per-data-point payload embedded in the portfolio chart.
type historyPoint struct {
	T  string  `json:"t"`  // formatted label, e.g. "Apr 1"
	V  float64 `json:"v"`  // equity
	PL float64 `json:"pl"` // cumulative P&L
}

// premiumChartPayload drives the doughnut chart.
type premiumChartPayload struct {
	Labels []string  `json:"labels"`
	Values []float64 `json:"values"`
}

// ── Main build function ───────────────────────────────────────────────────────

func buildReport(bc *broker.Client, cfg *config.Config, days int) (*ReportData, error) {
	// ── Account ───────────────────────────────────────────────────────────────
	acct, err := bc.GetAccount()
	if err != nil {
		return nil, fmt.Errorf("get account: %w", err)
	}
	equity, _ := acct.Equity.Float64()
	lastEquity, _ := acct.LastEquity.Float64()
	cash, _ := acct.Cash.Float64()
	bp, _ := acct.BuyingPower.Float64()
	pv, _ := acct.PortfolioValue.Float64()
	lmv, _ := acct.LongMarketValue.Float64()

	// ── Portfolio history ─────────────────────────────────────────────────────
	var histPoints []historyPoint
	var periodPL, periodPLPct float64
	hist, histErr := bc.GetPortfolioHistory(days)
	if histErr != nil {
		fmt.Fprintf(os.Stderr, "warning: portfolio history unavailable: %v\n", histErr)
	} else if hist != nil {
		cutoff := time.Now().AddDate(0, 0, -days).Unix()
		for i, ts := range hist.Timestamp {
			if ts < cutoff || i >= len(hist.Equity) {
				continue
			}
			v, _ := hist.Equity[i].Float64()
			pl, _ := hist.ProfitLoss[i].Float64()
			histPoints = append(histPoints, historyPoint{
				T:  time.Unix(ts, 0).Format("Jan 2"),
				V:  v,
				PL: pl,
			})
		}
		if n := len(hist.ProfitLoss); n > 0 {
			periodPL, _ = hist.ProfitLoss[n-1].Float64()
		}
		if n := len(hist.ProfitLossPct); n > 0 {
			pct, _ := hist.ProfitLossPct[n-1].Float64()
			periodPLPct = pct * 100
		}
	}
	histJSON, _ := json.Marshal(histPoints)

	// ── Activities ────────────────────────────────────────────────────────────
	after := time.Now().AddDate(0, 0, -days)
	rawActs, actsErr := bc.GetActivitiesRange(after)
	if actsErr != nil {
		fmt.Fprintf(os.Stderr, "warning: activities unavailable: %v\n", actsErr)
	}

	var activities []ReportActivity
	premBySymbol := make(map[string]float64)
	premCountBySymbol := make(map[string]int)
	var totalPremium float64
	var premTradeCount int

	for _, a := range rawActs {
		price, _ := a.Price.Float64()
		qty, _ := a.Qty.Float64()
		net, _ := a.NetAmount.Float64()

		isOptFill := false
		displaySym := a.Symbol

		if a.ActivityType == "FILL" && a.Symbol != "" {
			if root, expiry, optType, strike, perr := options.ParseSymbol(a.Symbol); perr == nil {
				isOptFill = true
				side := "PUT"
				if optType == "C" {
					side = "CALL"
				}
				displaySym = fmt.Sprintf("%s %s $%.2f %s", root, side, strike, expiry.String())
				totalPremium += net
				premTradeCount++
				premBySymbol[root] += net
				premCountBySymbol[root]++
			}
		}

		activities = append(activities, ReportActivity{
			Time:         a.TransactionTime.Format("2006-01-02 15:04"),
			Type:         a.ActivityType,
			Symbol:       displaySym,
			Side:         a.Side,
			Qty:          math.Abs(qty),
			Price:        price,
			NetAmount:    net,
			Description:  a.Description,
			IsOptionFill: isOptFill,
		})
	}
	// Reverse so newest is at the top
	for i, j := 0, len(activities)-1; i < j; i, j = i+1, j-1 {
		activities[i], activities[j] = activities[j], activities[i]
	}

	// Build premium breakdown slice, sorted by premium descending
	var premSlice []SymbolPremium
	for sym, prem := range premBySymbol {
		premSlice = append(premSlice, SymbolPremium{
			Symbol:  sym,
			Premium: prem,
			Count:   premCountBySymbol[sym],
		})
	}
	sort.Slice(premSlice, func(i, j int) bool { return premSlice[i].Premium > premSlice[j].Premium })

	absTotal := 0.0
	for _, p := range premSlice {
		absTotal += math.Abs(p.Premium)
	}
	for i := range premSlice {
		if absTotal > 0 {
			premSlice[i].Pct = (math.Abs(premSlice[i].Premium) / absTotal) * 100
		}
	}

	// Doughnut chart: only positive-premium symbols
	var pcLabels []string
	var pcValues []float64
	for _, p := range premSlice {
		if p.Premium > 0 {
			pcLabels = append(pcLabels, p.Symbol)
			pcValues = append(pcValues, p.Premium)
		}
	}
	premChartJSON, _ := json.Marshal(premiumChartPayload{Labels: pcLabels, Values: pcValues})

	// ── Open positions ────────────────────────────────────────────────────────
	rawPos, posErr := bc.GetPositions()
	if posErr != nil {
		fmt.Fprintf(os.Stderr, "warning: positions unavailable: %v\n", posErr)
	}
	var positions []ReportPosition
	for _, p := range rawPos {
		cp, mv, upl, uplPct := 0.0, 0.0, 0.0, 0.0
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

		rp := ReportPosition{
			Symbol:          p.Symbol,
			Qty:             math.Abs(qty),
			AvgEntryPrice:   math.Abs(entry),
			CurrentPrice:    math.Abs(cp),
			MarketValue:     mv,
			UnrealizedPL:    upl,
			UnrealizedPLPct: uplPct,
		}
		if root, expiry, optType, strike, perr := options.ParseSymbol(p.Symbol); perr == nil {
			rp.IsOption = true
			rp.Symbol = root
			rp.Strike = strike
			rp.Expiry = expiry.String()
			if optType == "C" {
				rp.OptionSide = "CALL"
			} else {
				rp.OptionSide = "PUT"
			}
		}
		positions = append(positions, rp)
	}

	return &ReportData{
		GeneratedAt:      time.Now(),
		Days:             days,
		PaperTrading:     cfg.Alpaca.PaperTrading,
		AccountNumber:    acct.AccountNumber,
		PortfolioValue:   pv,
		Equity:           equity,
		Cash:             cash,
		BuyingPower:      bp,
		LongMarketValue:  lmv,
		DayPL:            equity - lastEquity,
		PeriodPL:         periodPL,
		PeriodPLPct:      periodPLPct,
		HistoryJSON:      template.JS(histJSON),
		PremiumChartJSON: template.JS(premChartJSON),
		TotalPremium:     totalPremium,
		PremiumTradeCount: premTradeCount,
		PremiumBySymbol:  premSlice,
		Positions:        positions,
		Activities:       activities,
	}, nil
}

// writeReport renders the HTML template to the given path.
func writeReport(data *ReportData, path string) error {
	fns := template.FuncMap{
		"colorClass": func(v float64) string {
			if v >= 0 {
				return "pos"
			}
			return "neg"
		},
		"fmtMoney": func(v float64) string {
			if v >= 0 {
				return fmt.Sprintf("+$%.2f", v)
			}
			return fmt.Sprintf("-$%.2f", math.Abs(v))
		},
		"fmtPct": func(v float64) string {
			if v >= 0 {
				return fmt.Sprintf("+%.2f%%", v)
			}
			return fmt.Sprintf("%.2f%%", v)
		},
	}

	tmpl, err := template.New("report").Funcs(fns).Parse(reportTemplate)
	if err != nil {
		return fmt.Errorf("parse template: %w", err)
	}
	f, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("create file: %w", err)
	}
	defer f.Close()
	return tmpl.Execute(f, data)
}
