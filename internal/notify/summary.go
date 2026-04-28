package notify

import (
	"bytes"
	"fmt"
	"html/template"
	"math"
	"time"
)

// RunSummary carries all data for the end-of-cycle summary email.
type RunSummary struct {
	RunAt        time.Time
	PaperTrading bool

	// Account
	AccountNumber  string
	PortfolioValue float64
	Equity         float64
	LastEquity     float64
	Cash           float64
	BuyingPower    float64
	LongMarketValue float64

	// Positions (all open)
	Positions []SummaryPosition

	// Open orders (after cycle completes)
	OpenOrders []SummaryOrder

	// This cycle
	PlacedOrders    []SummaryPlacedOrder
	CashGuardBlocks []string // tickers blocked by cash guard
	ActivitySkips   []string // tickers skipped — already had open activity

	// Activities since last trading session
	Activities []SummaryActivity
}

type SummaryPosition struct {
	Symbol          string
	Qty             float64
	AvgEntryPrice   float64
	CurrentPrice    float64
	MarketValue     float64
	UnrealizedPL    float64
	UnrealizedPLPct float64
	IsOption        bool
	OptionSide      string // "CALL" / "PUT"
	Strike          float64
	Expiry          string
}

type SummaryOrder struct {
	Symbol      string
	Side        string
	Qty         float64
	LimitPrice  float64
	Status      string
	SubmittedAt time.Time
	IsOption    bool
	OptionSide  string
	Strike      float64
	Expiry      string
}

type SummaryPlacedOrder struct {
	Ticker    string
	Symbol    string
	Side      string // "PUT" / "CALL"
	Strike    float64
	Expiry    string
	BidPrice  float64
	Contracts int
	OrderID   string
}

type SummaryActivity struct {
	Time        time.Time
	Type        string
	Symbol      string
	Side        string
	Qty         float64
	Price       float64
	NetAmount   float64
	Description string
}

// SendRunSummary emails the end-of-cycle HTML summary report.
func (n *Notifier) SendRunSummary(s RunSummary) error {
	label := "Live"
	if s.PaperTrading {
		label = "Paper"
	}
	subject := fmt.Sprintf("[alpaca-trader] Run Summary (%s) — %s",
		label, s.RunAt.Format("Mon Jan 2, 3:04 PM MST"))

	html, err := renderSummary(s)
	if err != nil {
		return fmt.Errorf("render summary: %w", err)
	}
	return n.sendHTML(subject, html)
}

// ── Template helpers ──────────────────────────────────────────────────────────

func money(v float64) string  { return fmt.Sprintf("$%s", commaf(math.Abs(v))) }
func signedMoney(v float64) string {
	if v >= 0 {
		return fmt.Sprintf("+$%s", commaf(v))
	}
	return fmt.Sprintf("-$%s", commaf(-v))
}
func signedPct(v float64) string {
	if v >= 0 {
		return fmt.Sprintf("+%.2f%%", v)
	}
	return fmt.Sprintf("%.2f%%", v)
}
func plColor(v float64) string {
	if v >= 0 {
		return "#16a34a"
	}
	return "#dc2626"
}
func plBg(v float64) string {
	if v >= 0 {
		return "#f0fdf4"
	}
	return "#fef2f2"
}
func dayPL(equity, last float64) float64 { return equity - last }
func dayPLPct(equity, last float64) float64 {
	if last == 0 {
		return 0
	}
	return (equity - last) / last * 100
}
func rowBg(i int) string {
	if i%2 == 0 {
		return "#ffffff"
	}
	return "#f8fafc"
}
func fmtTime(t time.Time) string  { return t.Format("Mon Jan 2, 2006 at 3:04 PM MST") }
func fmtShortTime(t time.Time) string { return t.Format("Jan 2, 3:04 PM") }
func fmtFloat(v float64) string   { return fmt.Sprintf("%.2f", v) }

// commaf formats a float with comma separators, e.g. 1234567.89 → "1,234,567.89"
func commaf(v float64) string {
	s := fmt.Sprintf("%.2f", v)
	if len(s) <= 6 {
		return s
	}
	// split integer and decimal
	dot := len(s) - 3
	intPart, dec := s[:dot], s[dot:]
	var out []byte
	for i, c := range intPart {
		if i > 0 && (len(intPart)-i)%3 == 0 {
			out = append(out, ',')
		}
		out = append(out, byte(c))
	}
	return string(out) + dec
}

var summaryTmpl = template.Must(template.New("summary").Funcs(template.FuncMap{
	"money":       money,
	"signedMoney": signedMoney,
	"signedPct":   signedPct,
	"plColor":     plColor,
	"plBg":        plBg,
	"dayPL":       dayPL,
	"dayPLPct":    dayPLPct,
	"rowBg":       rowBg,
	"fmtTime":     fmtTime,
	"fmtShort":    fmtShortTime,
	"fmtFloat":    fmtFloat,
}).Parse(summaryHTML))

func renderSummary(s RunSummary) (string, error) {
	var buf bytes.Buffer
	if err := summaryTmpl.Execute(&buf, s); err != nil {
		return "", err
	}
	return buf.String(), nil
}

// ── HTML template ─────────────────────────────────────────────────────────────

const summaryHTML = `<!DOCTYPE html>
<html lang="en">
<head><meta charset="UTF-8"><meta name="viewport" content="width=device-width,initial-scale=1"></head>
<body style="margin:0;padding:0;background:#f1f5f9;font-family:-apple-system,BlinkMacSystemFont,'Segoe UI',Roboto,Arial,sans-serif;color:#1e293b;">
<div style="max-width:680px;margin:0 auto;padding:20px 12px;">

  {{/* ── Header ── */}}
  <table width="100%" cellpadding="0" cellspacing="0" style="background:#0f172a;border-radius:12px 12px 0 0;">
    <tr><td style="padding:28px 32px;">
      <table width="100%" cellpadding="0" cellspacing="0"><tr>
        <td>
          <div style="color:#94a3b8;font-size:11px;font-weight:700;text-transform:uppercase;letter-spacing:1.5px;margin-bottom:8px;">Alpaca Trading Bot</div>
          <div style="color:#f8fafc;font-size:26px;font-weight:700;margin-bottom:6px;">Run Summary</div>
          <div style="color:#64748b;font-size:13px;">{{fmtTime .RunAt}}</div>
        </td>
        <td align="right" valign="top">
          {{if .PaperTrading}}
          <div style="display:inline-block;background:#78350f;color:#fde68a;padding:5px 14px;border-radius:20px;font-size:12px;font-weight:700;letter-spacing:0.5px;">PAPER TRADING</div>
          {{else}}
          <div style="display:inline-block;background:#14532d;color:#bbf7d0;padding:5px 14px;border-radius:20px;font-size:12px;font-weight:700;letter-spacing:0.5px;">&#x2713; LIVE</div>
          {{end}}
        </td>
      </tr></table>
    </td></tr>
  </table>

  {{/* ── Account Overview ── */}}
  <table width="100%" cellpadding="0" cellspacing="0" style="background:#fff;border-left:1px solid #e2e8f0;border-right:1px solid #e2e8f0;border-bottom:1px solid #e2e8f0;">
    <tr><td style="padding:24px 32px 8px;">
      <div style="font-size:11px;font-weight:700;color:#94a3b8;text-transform:uppercase;letter-spacing:1.2px;margin-bottom:20px;">
        Account Overview &nbsp;·&nbsp; {{.AccountNumber}}
      </div>
      <table width="100%" cellpadding="0" cellspacing="0">
        <tr>
          <td width="25%" style="padding-bottom:20px;vertical-align:top;">
            <div style="font-size:11px;color:#64748b;margin-bottom:5px;">Portfolio Value</div>
            <div style="font-size:20px;font-weight:700;color:#0f172a;">{{money .PortfolioValue}}</div>
          </td>
          <td width="25%" style="padding-bottom:20px;vertical-align:top;">
            <div style="font-size:11px;color:#64748b;margin-bottom:5px;">Day P&amp;L</div>
            {{$pl := dayPL .Equity .LastEquity}}
            {{$pct := dayPLPct .Equity .LastEquity}}
            <div style="font-size:20px;font-weight:700;color:{{plColor $pl}};">
              {{signedMoney $pl}}
            </div>
            <div style="font-size:12px;color:{{plColor $pl}};">{{signedPct $pct}}</div>
          </td>
          <td width="25%" style="padding-bottom:20px;vertical-align:top;">
            <div style="font-size:11px;color:#64748b;margin-bottom:5px;">Cash</div>
            <div style="font-size:20px;font-weight:700;color:#0f172a;">{{money .Cash}}</div>
          </td>
          <td width="25%" style="padding-bottom:20px;vertical-align:top;">
            <div style="font-size:11px;color:#64748b;margin-bottom:5px;">Buying Power</div>
            <div style="font-size:20px;font-weight:700;color:#0f172a;">{{money .BuyingPower}}</div>
          </td>
        </tr>
      </table>
    </td></tr>
  </table>

  <div style="height:12px;"></div>

  {{/* ── This Cycle ── */}}
  <table width="100%" cellpadding="0" cellspacing="0" style="background:#fff;border:1px solid #e2e8f0;border-radius:8px;margin-bottom:12px;">
    <tr><td style="padding:20px 24px 4px;">
      <div style="font-size:11px;font-weight:700;color:#94a3b8;text-transform:uppercase;letter-spacing:1.2px;margin-bottom:16px;">This Cycle</div>

      {{if .PlacedOrders}}
      {{range $i, $o := .PlacedOrders}}
      <table width="100%" cellpadding="0" cellspacing="0" style="background:#f0fdf4;border:1px solid #bbf7d0;border-radius:6px;margin-bottom:8px;">
        <tr><td style="padding:10px 14px;">
          <table width="100%" cellpadding="0" cellspacing="0"><tr>
            <td>
              <span style="color:#15803d;font-size:13px;font-weight:700;">&#x2713; Order Placed</span>
              <span style="color:#166534;font-size:13px;"> &nbsp;{{$o.Ticker}} &mdash; Sell {{$o.Contracts}}x {{$o.Side}} @ ${{fmtFloat $o.Strike}} exp {{$o.Expiry}}</span>
            </td>
            <td align="right">
              <span style="color:#16a34a;font-size:13px;font-weight:600;">bid {{money $o.BidPrice}}</span>
            </td>
          </tr></table>
          <div style="color:#64748b;font-size:11px;margin-top:4px;">{{$o.Symbol}} &nbsp;·&nbsp; order {{$o.OrderID}}</div>
        </td></tr>
      </table>
      {{end}}
      {{else}}
      <p style="color:#64748b;font-size:13px;margin:0 0 16px;">No orders placed this cycle.</p>
      {{end}}

      {{if .CashGuardBlocks}}
      <table width="100%" cellpadding="0" cellspacing="0" style="background:#fffbeb;border:1px solid #fde68a;border-radius:6px;margin-bottom:8px;">
        <tr><td style="padding:10px 14px;">
          <span style="color:#b45309;font-size:13px;font-weight:700;">&#x26A0; Cash Guard</span>
          <span style="color:#92400e;font-size:13px;"> &nbsp;Insufficient cash to place puts for: {{range $i, $t := .CashGuardBlocks}}{{if $i}}, {{end}}{{$t}}{{end}}</span>
        </td></tr>
      </table>
      {{end}}

      {{if .ActivitySkips}}
      <table width="100%" cellpadding="0" cellspacing="0" style="background:#eff6ff;border:1px solid #bfdbfe;border-radius:6px;margin-bottom:8px;">
        <tr><td style="padding:10px 14px;">
          <span style="color:#1d4ed8;font-size:13px;font-weight:700;">&#x2139; Skipped</span>
          <span style="color:#1e40af;font-size:13px;"> &nbsp;Open activity already exists for: {{range $i, $t := .ActivitySkips}}{{if $i}}, {{end}}{{$t}}{{end}}</span>
        </td></tr>
      </table>
      {{end}}

    </td></tr>
    <tr><td style="height:12px;"></td></tr>
  </table>

  {{/* ── Positions ── */}}
  {{if .Positions}}
  <table width="100%" cellpadding="0" cellspacing="0" style="background:#fff;border:1px solid #e2e8f0;border-radius:8px;margin-bottom:12px;">
    <tr><td style="padding:20px 24px 0;">
      <div style="font-size:11px;font-weight:700;color:#94a3b8;text-transform:uppercase;letter-spacing:1.2px;margin-bottom:14px;">
        Open Positions &nbsp;({{len .Positions}})
      </div>
    </td></tr>
    <tr><td style="padding:0 0 4px;">
      <table width="100%" cellpadding="0" cellspacing="0">
        <tr style="background:#f1f5f9;">
          <td style="padding:8px 24px;font-size:11px;font-weight:700;color:#64748b;text-transform:uppercase;">Symbol</td>
          <td style="padding:8px 8px;font-size:11px;font-weight:700;color:#64748b;text-transform:uppercase;" align="right">Qty</td>
          <td style="padding:8px 8px;font-size:11px;font-weight:700;color:#64748b;text-transform:uppercase;" align="right">Entry</td>
          <td style="padding:8px 8px;font-size:11px;font-weight:700;color:#64748b;text-transform:uppercase;" align="right">Current</td>
          <td style="padding:8px 8px;font-size:11px;font-weight:700;color:#64748b;text-transform:uppercase;" align="right">Mkt Value</td>
          <td style="padding:8px 24px 8px 8px;font-size:11px;font-weight:700;color:#64748b;text-transform:uppercase;" align="right">Unreal. P&amp;L</td>
        </tr>
        {{range $i, $p := .Positions}}
        <tr style="background:{{rowBg $i}};border-top:1px solid #f1f5f9;">
          <td style="padding:10px 24px;">
            <div style="font-size:13px;font-weight:600;color:#0f172a;">{{$p.Symbol}}</div>
            {{if $p.IsOption}}<div style="font-size:11px;color:#64748b;">{{$p.OptionSide}} · strike ${{fmtFloat $p.Strike}} · exp {{$p.Expiry}}</div>{{end}}
          </td>
          <td style="padding:10px 8px;font-size:13px;color:#1e293b;" align="right">{{fmtFloat $p.Qty}}</td>
          <td style="padding:10px 8px;font-size:13px;color:#1e293b;" align="right">{{money $p.AvgEntryPrice}}</td>
          <td style="padding:10px 8px;font-size:13px;color:#1e293b;" align="right">{{money $p.CurrentPrice}}</td>
          <td style="padding:10px 8px;font-size:13px;color:#1e293b;" align="right">{{money $p.MarketValue}}</td>
          <td style="padding:10px 24px 10px 8px;" align="right">
            <div style="font-size:13px;font-weight:600;color:{{plColor $p.UnrealizedPL}};">{{signedMoney $p.UnrealizedPL}}</div>
            <div style="font-size:11px;color:{{plColor $p.UnrealizedPL}};">{{signedPct $p.UnrealizedPLPct}}</div>
          </td>
        </tr>
        {{end}}
      </table>
    </td></tr>
    <tr><td style="height:8px;"></td></tr>
  </table>
  {{end}}

  {{/* ── Open Orders ── */}}
  {{if .OpenOrders}}
  <table width="100%" cellpadding="0" cellspacing="0" style="background:#fff;border:1px solid #e2e8f0;border-radius:8px;margin-bottom:12px;">
    <tr><td style="padding:20px 24px 0;">
      <div style="font-size:11px;font-weight:700;color:#94a3b8;text-transform:uppercase;letter-spacing:1.2px;margin-bottom:14px;">
        Open Orders &nbsp;({{len .OpenOrders}})
      </div>
    </td></tr>
    <tr><td style="padding:0 0 4px;">
      <table width="100%" cellpadding="0" cellspacing="0">
        <tr style="background:#f1f5f9;">
          <td style="padding:8px 24px;font-size:11px;font-weight:700;color:#64748b;text-transform:uppercase;">Symbol</td>
          <td style="padding:8px 8px;font-size:11px;font-weight:700;color:#64748b;text-transform:uppercase;" align="right">Side</td>
          <td style="padding:8px 8px;font-size:11px;font-weight:700;color:#64748b;text-transform:uppercase;" align="right">Qty</td>
          <td style="padding:8px 8px;font-size:11px;font-weight:700;color:#64748b;text-transform:uppercase;" align="right">Limit</td>
          <td style="padding:8px 24px 8px 8px;font-size:11px;font-weight:700;color:#64748b;text-transform:uppercase;" align="right">Submitted</td>
        </tr>
        {{range $i, $o := .OpenOrders}}
        <tr style="background:{{rowBg $i}};border-top:1px solid #f1f5f9;">
          <td style="padding:10px 24px;">
            <div style="font-size:13px;font-weight:600;color:#0f172a;">{{$o.Symbol}}</div>
            {{if $o.IsOption}}<div style="font-size:11px;color:#64748b;">{{$o.OptionSide}} · strike ${{fmtFloat $o.Strike}} · exp {{$o.Expiry}}</div>{{end}}
          </td>
          <td style="padding:10px 8px;font-size:13px;text-transform:capitalize;" align="right">{{$o.Side}}</td>
          <td style="padding:10px 8px;font-size:13px;" align="right">{{fmtFloat $o.Qty}}</td>
          <td style="padding:10px 8px;font-size:13px;" align="right">{{money $o.LimitPrice}}</td>
          <td style="padding:10px 24px 10px 8px;font-size:12px;color:#64748b;" align="right">{{fmtShort $o.SubmittedAt}}</td>
        </tr>
        {{end}}
      </table>
    </td></tr>
    <tr><td style="height:8px;"></td></tr>
  </table>
  {{end}}

  {{/* ── Recent Activity ── */}}
  {{if .Activities}}
  <table width="100%" cellpadding="0" cellspacing="0" style="background:#fff;border:1px solid #e2e8f0;border-radius:8px;margin-bottom:12px;">
    <tr><td style="padding:20px 24px 0;">
      <div style="font-size:11px;font-weight:700;color:#94a3b8;text-transform:uppercase;letter-spacing:1.2px;margin-bottom:14px;">
        Recent Activity
      </div>
    </td></tr>
    <tr><td style="padding:0 0 4px;">
      <table width="100%" cellpadding="0" cellspacing="0">
        <tr style="background:#f1f5f9;">
          <td style="padding:8px 24px;font-size:11px;font-weight:700;color:#64748b;text-transform:uppercase;">Time</td>
          <td style="padding:8px 8px;font-size:11px;font-weight:700;color:#64748b;text-transform:uppercase;">Type</td>
          <td style="padding:8px 8px;font-size:11px;font-weight:700;color:#64748b;text-transform:uppercase;">Symbol</td>
          <td style="padding:8px 8px;font-size:11px;font-weight:700;color:#64748b;text-transform:uppercase;" align="right">Side</td>
          <td style="padding:8px 8px;font-size:11px;font-weight:700;color:#64748b;text-transform:uppercase;" align="right">Qty</td>
          <td style="padding:8px 24px 8px 8px;font-size:11px;font-weight:700;color:#64748b;text-transform:uppercase;" align="right">Price / Amount</td>
        </tr>
        {{range $i, $a := .Activities}}
        <tr style="background:{{rowBg $i}};border-top:1px solid #f1f5f9;">
          <td style="padding:10px 24px;font-size:12px;color:#64748b;white-space:nowrap;">{{fmtShort $a.Time}}</td>
          <td style="padding:10px 8px;">
            <span style="background:#f1f5f9;color:#475569;font-size:11px;font-weight:600;padding:2px 7px;border-radius:4px;">{{$a.Type}}</span>
          </td>
          <td style="padding:10px 8px;font-size:13px;font-weight:600;color:#0f172a;">{{$a.Symbol}}</td>
          <td style="padding:10px 8px;font-size:13px;text-transform:capitalize;" align="right">{{$a.Side}}</td>
          <td style="padding:10px 8px;font-size:13px;" align="right">{{if $a.Qty}}{{fmtFloat $a.Qty}}{{end}}</td>
          <td style="padding:10px 24px 10px 8px;font-size:13px;font-weight:600;" align="right">
            {{if $a.Price}}{{money $a.Price}}{{else if $a.NetAmount}}<span style="color:{{plColor $a.NetAmount}};">{{signedMoney $a.NetAmount}}</span>{{end}}
          </td>
        </tr>
        {{end}}
      </table>
    </td></tr>
    <tr><td style="height:8px;"></td></tr>
  </table>
  {{end}}

  {{/* ── Footer ── */}}
  <table width="100%" cellpadding="0" cellspacing="0">
    <tr><td style="padding:16px 0;text-align:center;">
      <div style="font-size:11px;color:#94a3b8;">alpaca-trader &nbsp;·&nbsp; {{fmtTime .RunAt}}</div>
    </td></tr>
  </table>

</div>
</body>
</html>`
