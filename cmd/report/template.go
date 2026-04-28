package main

const reportTemplate = `<!DOCTYPE html>
<html lang="en">
<head>
  <meta charset="UTF-8">
  <meta name="viewport" content="width=device-width, initial-scale=1.0">
  <title>Trading Report · {{.GeneratedAt.Format "Jan 2, 2006"}}</title>
  <script src="https://cdn.jsdelivr.net/npm/chart.js@4.4.0/dist/chart.umd.min.js"></script>
  <style>
    :root {
      --bg:      #0d1117;
      --surface: #161b22;
      --surface2:#21262d;
      --border:  #30363d;
      --text:    #e6edf3;
      --muted:   #8b949e;
      --dim:     #6e7681;
      --blue:    #58a6ff;
      --green:   #3fb950;
      --red:     #f85149;
      --yellow:  #d29922;
      --purple:  #bc8cff;
    }
    *, *::before, *::after { box-sizing: border-box; margin: 0; padding: 0; }
    html { scroll-behavior: smooth; }
    body {
      background: var(--bg);
      color: var(--text);
      font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", Helvetica, Arial, sans-serif;
      font-size: 14px;
      line-height: 1.6;
      min-height: 100vh;
    }
    a { color: var(--blue); text-decoration: none; }
    a:hover { text-decoration: underline; }

    /* ── Header ── */
    .hdr {
      background: var(--surface);
      border-bottom: 1px solid var(--border);
      padding: 14px 32px;
      display: flex;
      align-items: center;
      justify-content: space-between;
      position: sticky;
      top: 0;
      z-index: 20;
    }
    .hdr-left  { display: flex; align-items: center; gap: 12px; }
    .hdr-title { font-size: 17px; font-weight: 600; }
    .hdr-acct  { color: var(--muted); font-size: 12px; font-family: monospace; }
    .hdr-right { display: flex; align-items: center; gap: 16px; }
    .hdr-meta  { color: var(--muted); font-size: 12px; }
    .badge {
      border-radius: 4px; padding: 2px 8px;
      font-size: 10px; font-weight: 700;
      letter-spacing: 0.07em; text-transform: uppercase;
    }
    .badge-paper { background:#d2992212; color:var(--yellow); border:1px solid var(--yellow); }
    .badge-live  { background:#3fb95012; color:var(--green);  border:1px solid var(--green);  }

    /* ── Layout ── */
    .main { max-width: 1440px; margin: 0 auto; padding: 28px 32px 48px; }

    /* ── KPI grid ── */
    .kpi-grid {
      display: grid;
      grid-template-columns: repeat(5, 1fr);
      gap: 16px;
      margin-bottom: 24px;
    }
    .kpi {
      background: var(--surface);
      border: 1px solid var(--border);
      border-radius: 8px;
      padding: 18px 20px;
    }
    .kpi-label {
      font-size: 10px; font-weight: 700;
      text-transform: uppercase; letter-spacing: 0.09em;
      color: var(--muted); margin-bottom: 8px;
    }
    .kpi-value { font-size: 22px; font-weight: 700; line-height: 1.2; }
    .kpi-sub   { font-size: 12px; color: var(--muted); margin-top: 4px; }

    /* ── Cards / sections ── */
    .card {
      background: var(--surface);
      border: 1px solid var(--border);
      border-radius: 8px;
      margin-bottom: 24px;
      overflow: hidden;
    }
    .card-head {
      padding: 13px 20px;
      border-bottom: 1px solid var(--border);
      font-size: 13px; font-weight: 600;
      display: flex; align-items: center; justify-content: space-between;
    }
    .card-head-note { font-size: 12px; font-weight: 400; color: var(--muted); }
    .card-body  { padding: 20px; }
    .card-flush { /* no padding, used for tables */ }

    /* ── Two-column grid ── */
    .grid2 { display: grid; grid-template-columns: 1fr 1fr; gap: 24px; margin-bottom: 24px; }

    /* ── Color helpers ── */
    .pos { color: var(--green); }
    .neg { color: var(--red);   }
    .neu { color: var(--muted); }

    /* ── Premium panel ── */
    .prem-total { font-size: 30px; font-weight: 700; line-height: 1.1; margin-bottom: 4px; }
    .prem-sub   { font-size: 13px; color: var(--muted); margin-bottom: 20px; }

    /* ── Tables ── */
    .tbl-wrap { overflow-x: auto; }
    table { width: 100%; border-collapse: collapse; }
    thead th {
      padding: 10px 16px;
      text-align: left;
      font-size: 10px; font-weight: 700;
      text-transform: uppercase; letter-spacing: 0.08em;
      color: var(--muted);
      border-bottom: 1px solid var(--border);
      white-space: nowrap;
    }
    tbody td {
      padding: 10px 16px;
      border-bottom: 1px solid #1c2026;
      font-size: 13px;
      vertical-align: middle;
    }
    tbody tr:last-child td { border-bottom: none; }
    tbody tr:hover td      { background: var(--surface2); }
    .num { text-align: right; font-variant-numeric: tabular-nums; }

    /* ── Pills ── */
    .pill {
      border-radius: 4px; padding: 2px 7px;
      font-size: 10px; font-weight: 700;
      letter-spacing: 0.04em; white-space: nowrap;
    }
    .pill-opt  { background:#3fb95018; color:var(--green);  }
    .pill-eq   { background:#58a6ff18; color:var(--blue);   }
    .pill-fill { background:#58a6ff18; color:var(--blue);   }
    .pill-misc { background:#6e768118; color:var(--dim);    }
    .pill-put  { background:#f8514918; color:var(--red);    }
    .pill-call { background:#3fb95018; color:var(--green);  }
    .side-sell { color: var(--red);   font-weight: 600; text-transform: uppercase; font-size: 11px; }
    .side-buy  { color: var(--green); font-weight: 600; text-transform: uppercase; font-size: 11px; }

    /* ── Symbol cell ── */
    .sym-main { font-weight: 600; }
    .sym-sub  { font-size: 11px; color: var(--muted); }

    /* ── Chart wrapper ── */
    .chart-wrap { position: relative; }

    /* ── Previous reports nav ── */
    .nav-list  { display: flex; flex-wrap: wrap; gap: 8px; padding: 16px 20px; }
    .nav-item  {
      background: var(--surface2);
      border: 1px solid var(--border);
      border-radius: 6px;
      padding: 5px 14px;
      font-size: 13px;
      color: var(--muted);
      transition: border-color .15s, color .15s;
    }
    .nav-item:hover          { border-color: var(--blue); color: var(--blue); text-decoration: none; }
    .nav-item.current        { border-color: var(--blue); color: var(--blue); background: #58a6ff12; }

    /* ── Footer ── */
    .footer { text-align: center; color: var(--dim); font-size: 12px; padding: 32px; }

    /* ── Responsive ── */
    @media (max-width: 960px) {
      .kpi-grid { grid-template-columns: repeat(2, 1fr); }
      .grid2    { grid-template-columns: 1fr; }
      .main     { padding: 16px; }
      .hdr      { padding: 12px 16px; }
    }
    @media (max-width: 480px) {
      .kpi-grid { grid-template-columns: 1fr; }
    }
  </style>
</head>
<body>

<!-- ═══════════════════ HEADER ═══════════════════ -->
<header class="hdr">
  <div class="hdr-left">
    <span class="hdr-title">Trading Report</span>
    <span class="hdr-acct">{{.AccountNumber}}</span>
  </div>
  <div class="hdr-right">
    {{if .PaperTrading}}<span class="badge badge-paper">Paper</span>{{else}}<span class="badge badge-live">Live</span>{{end}}
    <span class="hdr-meta">{{.Days}}-day window &middot; {{.GeneratedAt.Format "Jan 2, 2006 3:04 PM"}}</span>
  </div>
</header>

<div class="main">

  <!-- ═══════════════════ KPI CARDS ═══════════════════ -->
  <div class="kpi-grid">
    <div class="kpi">
      <div class="kpi-label">Portfolio Value</div>
      <div class="kpi-value">${{printf "%.2f" .PortfolioValue}}</div>
      <div class="kpi-sub">Equity: ${{printf "%.2f" .Equity}}</div>
    </div>
    <div class="kpi">
      <div class="kpi-label">Cash</div>
      <div class="kpi-value">${{printf "%.2f" .Cash}}</div>
      <div class="kpi-sub">BP: ${{printf "%.2f" .BuyingPower}}</div>
    </div>
    <div class="kpi">
      <div class="kpi-label">Day Change</div>
      <div class="kpi-value {{colorClass .DayPL}}">{{fmtMoney .DayPL}}</div>
    </div>
    <div class="kpi">
      <div class="kpi-label">Period P&amp;L ({{.Days}}d)</div>
      <div class="kpi-value {{colorClass .PeriodPL}}">{{fmtMoney .PeriodPL}}</div>
      <div class="kpi-sub {{colorClass .PeriodPLPct}}">{{fmtPct .PeriodPLPct}}</div>
    </div>
    <div class="kpi">
      <div class="kpi-label">Net Premium</div>
      <div class="kpi-value {{colorClass .TotalPremium}}">{{fmtMoney .TotalPremium}}</div>
      <div class="kpi-sub">{{.PremiumTradeCount}} option trade{{if ne .PremiumTradeCount 1}}s{{end}}</div>
    </div>
  </div>

  <!-- ═══════════════════ PORTFOLIO HISTORY CHART ═══════════════════ -->
  <div class="card">
    <div class="card-head">Portfolio Value History</div>
    <div class="card-body">
      <div class="chart-wrap" style="height:300px">
        <canvas id="portfolioChart"></canvas>
      </div>
    </div>
  </div>

  <!-- ═══════════════════ PREMIUM ANALYTICS (2-col) ═══════════════════ -->
  <div class="grid2">
    <!-- Left: breakdown table -->
    <div class="card" style="margin-bottom:0">
      <div class="card-head">Premium Breakdown by Symbol</div>
      <div class="card-body">
        <div class="prem-total {{colorClass .TotalPremium}}">{{fmtMoney .TotalPremium}}</div>
        <div class="prem-sub">Net option premium collected over {{.Days}} days</div>
        {{if .PremiumBySymbol}}
        <table>
          <thead>
            <tr><th>Symbol</th><th class="num">Trades</th><th class="num">Net Premium</th><th class="num">Share</th></tr>
          </thead>
          <tbody>
            {{range .PremiumBySymbol}}
            <tr>
              <td class="sym-main">{{.Symbol}}</td>
              <td class="num neu">{{.Count}}</td>
              <td class="num {{colorClass .Premium}}">{{fmtMoney .Premium}}</td>
              <td class="num neu">{{printf "%.1f" .Pct}}%</td>
            </tr>
            {{end}}
          </tbody>
        </table>
        {{else}}
        <p style="color:var(--dim);text-align:center;padding:32px 0">No option premium activity in this period.</p>
        {{end}}
      </div>
    </div>

    <!-- Right: doughnut chart -->
    <div class="card" style="margin-bottom:0">
      <div class="card-head">Premium Distribution</div>
      <div class="card-body">
        <div class="chart-wrap" style="height:280px">
          <canvas id="premiumChart"></canvas>
        </div>
      </div>
    </div>
  </div>
  <div style="margin-bottom:24px"></div>

  <!-- ═══════════════════ OPEN POSITIONS ═══════════════════ -->
  {{if .Positions}}
  <div class="card">
    <div class="card-head">
      Open Positions
      <span class="card-head-note">{{len .Positions}} position{{if ne (len .Positions) 1}}s{{end}}</span>
    </div>
    <div class="card-flush">
      <div class="tbl-wrap">
        <table>
          <thead>
            <tr>
              <th>Symbol</th>
              <th>Type</th>
              <th class="num">Qty</th>
              <th class="num">Entry</th>
              <th class="num">Current</th>
              <th class="num">Mkt Value</th>
              <th class="num">Unrealized P&amp;L</th>
              <th class="num">Return</th>
            </tr>
          </thead>
          <tbody>
            {{range .Positions}}
            <tr>
              <td>
                <div class="sym-main">
                  {{if .IsOption}}{{.Symbol}} <span class="pill {{if eq .OptionSide "PUT"}}pill-put{{else}}pill-call{{end}}">{{.OptionSide}}</span>{{else}}{{.Symbol}}{{end}}
                </div>
                {{if .IsOption}}<div class="sym-sub">${{printf "%.2f" .Strike}} &middot; exp {{.Expiry}}</div>{{end}}
              </td>
              <td>{{if .IsOption}}<span class="pill pill-opt">Option</span>{{else}}<span class="pill pill-eq">Equity</span>{{end}}</td>
              <td class="num">{{printf "%.0f" .Qty}}</td>
              <td class="num">${{printf "%.4f" .AvgEntryPrice}}</td>
              <td class="num">${{printf "%.4f" .CurrentPrice}}</td>
              <td class="num">${{printf "%.2f" .MarketValue}}</td>
              <td class="num {{colorClass .UnrealizedPL}}">{{fmtMoney .UnrealizedPL}}</td>
              <td class="num {{colorClass .UnrealizedPLPct}}">{{fmtPct .UnrealizedPLPct}}</td>
            </tr>
            {{end}}
          </tbody>
        </table>
      </div>
    </div>
  </div>
  {{end}}

  <!-- ═══════════════════ ACTIVITY FEED ═══════════════════ -->
  <div class="card">
    <div class="card-head">
      Activity Feed
      <span class="card-head-note">{{len .Activities}} event{{if ne (len .Activities) 1}}s{{end}} &middot; {{.Days}}-day window</span>
    </div>
    <div class="card-flush">
      {{if .Activities}}
      <div class="tbl-wrap" style="max-height:520px;overflow-y:auto">
        <table>
          <thead style="position:sticky;top:0;background:var(--surface)">
            <tr>
              <th>Date</th>
              <th>Type</th>
              <th>Symbol</th>
              <th>Side</th>
              <th class="num">Qty</th>
              <th class="num">Price</th>
              <th class="num">Net Amount</th>
              <th>Description</th>
            </tr>
          </thead>
          <tbody>
            {{range .Activities}}
            <tr>
              <td style="white-space:nowrap;color:var(--muted)">{{.Time}}</td>
              <td>
                {{if .IsOptionFill}}<span class="pill pill-opt">Option</span>
                {{else if eq .Type "FILL"}}<span class="pill pill-fill">Fill</span>
                {{else}}<span class="pill pill-misc">{{.Type}}</span>
                {{end}}
              </td>
              <td>{{.Symbol}}</td>
              <td>
                {{if eq .Side "sell"}}<span class="side-sell">Sell</span>
                {{else if eq .Side "buy"}}<span class="side-buy">Buy</span>
                {{else}}<span class="neu">{{.Side}}</span>
                {{end}}
              </td>
              <td class="num">{{if gt .Qty 0.0}}{{printf "%.0f" .Qty}}{{end}}</td>
              <td class="num">{{if gt .Price 0.0}}${{printf "%.4f" .Price}}{{end}}</td>
              <td class="num {{colorClass .NetAmount}}">{{if ne .NetAmount 0.0}}{{fmtMoney .NetAmount}}{{end}}</td>
              <td style="color:var(--muted);font-size:12px;max-width:240px">{{.Description}}</td>
            </tr>
            {{end}}
          </tbody>
        </table>
      </div>
      {{else}}
      <div style="text-align:center;padding:48px;color:var(--dim)">No activity found in this period.</div>
      {{end}}
    </div>
  </div>

  <!-- ═══════════════════ PREVIOUS REPORTS ═══════════════════ -->
  {{if .OtherReports}}
  <div class="card">
    <div class="card-head">Previous Reports</div>
    <div class="nav-list">
      <a href="./report.html" class="nav-item current">{{.GeneratedAt.Format "Jan 2, 2006"}} (current)</a>
      {{range .OtherReports}}
      <a href="{{.Path}}" class="nav-item">{{.Date}}</a>
      {{end}}
    </div>
  </div>
  {{end}}

</div><!-- /main -->

<footer class="footer">
  alpaca-trader report &middot; {{.GeneratedAt.Format "Jan 2, 2006 3:04:05 PM MST"}}
  {{if .PaperTrading}}&middot; <span style="color:var(--yellow)">Paper Trading</span>{{end}}
</footer>

<script>
  Chart.defaults.color = '#8b949e';
  Chart.defaults.borderColor = '#30363d';

  const PALETTE = [
    '#58a6ff','#3fb950','#d29922','#f85149','#bc8cff',
    '#79c0ff','#56d364','#e3b341','#ff7b72','#d2a8ff',
    '#ffa657','#89d4f5','#7ee787','#f2cc60','#ffa198',
  ];

  const tooltipDefaults = {
    backgroundColor: '#161b22',
    borderColor: '#30363d',
    borderWidth: 1,
    titleColor: '#e6edf3',
    bodyColor: '#8b949e',
    padding: 10,
  };

  // ── Portfolio History Chart ──────────────────────────────────────────────
  (function () {
    const raw = {{.HistoryJSON}};
    if (!raw || raw.length === 0) {
      document.getElementById('portfolioChart').closest('.card-body').innerHTML =
        '<p style="text-align:center;color:#6e7681;padding:40px">Portfolio history unavailable.</p>';
      return;
    }
    const labels = raw.map(d => d.t);
    const equity = raw.map(d => d.v);
    const pl     = raw.map(d => d.pl);

    new Chart(document.getElementById('portfolioChart'), {
      type: 'line',
      data: {
        labels,
        datasets: [
          {
            label: 'Portfolio Value',
            data: equity,
            borderColor: '#58a6ff',
            backgroundColor: 'rgba(88,166,255,0.07)',
            fill: true,
            tension: 0.35,
            pointRadius: raw.length > 90 ? 0 : 3,
            pointHoverRadius: 5,
            borderWidth: 2,
            yAxisID: 'y',
          },
          {
            label: 'Period P&L',
            data: pl,
            borderColor: '#3fb950',
            backgroundColor: 'transparent',
            borderDash: [5, 4],
            tension: 0.35,
            pointRadius: 0,
            pointHoverRadius: 4,
            borderWidth: 1.5,
            yAxisID: 'y1',
          },
        ],
      },
      options: {
        responsive: true,
        maintainAspectRatio: false,
        interaction: { mode: 'index', intersect: false },
        plugins: {
          legend: { labels: { color: '#e6edf3', usePointStyle: true, boxWidth: 10 } },
          tooltip: {
            ...tooltipDefaults,
            callbacks: {
              label: ctx =>
                ' ' + ctx.dataset.label + ': $' +
                ctx.parsed.y.toLocaleString('en-US', { minimumFractionDigits: 2, maximumFractionDigits: 2 }),
            },
          },
        },
        scales: {
          x: {
            ticks: { maxTicksLimit: 14, color: '#8b949e', maxRotation: 0 },
            grid: { color: 'rgba(48,54,61,0.5)' },
          },
          y: {
            position: 'left',
            ticks: {
              color: '#8b949e',
              callback: v => '$' + v.toLocaleString('en-US', { maximumFractionDigits: 0 }),
            },
            grid: { color: 'rgba(48,54,61,0.5)' },
          },
          y1: {
            position: 'right',
            ticks: {
              color: '#3fb950',
              callback: v => (v >= 0 ? '+' : '') + '$' + v.toLocaleString('en-US', { maximumFractionDigits: 0 }),
            },
            grid: { drawOnChartArea: false },
          },
        },
      },
    });
  })();

  // ── Premium Doughnut Chart ───────────────────────────────────────────────
  (function () {
    const raw = {{.PremiumChartJSON}};
    if (!raw || !raw.labels || raw.labels.length === 0) {
      const wrap = document.getElementById('premiumChart').closest('.card-body');
      wrap.innerHTML = '<p style="text-align:center;color:#6e7681;padding:48px 20px">No premium data for this period.</p>';
      return;
    }
    new Chart(document.getElementById('premiumChart'), {
      type: 'doughnut',
      data: {
        labels: raw.labels,
        datasets: [{
          data: raw.values,
          backgroundColor: PALETTE,
          borderColor: '#161b22',
          borderWidth: 3,
          hoverOffset: 6,
        }],
      },
      options: {
        responsive: true,
        maintainAspectRatio: false,
        plugins: {
          legend: {
            position: 'right',
            labels: { color: '#e6edf3', padding: 14, usePointStyle: true, pointStyle: 'circle' },
          },
          tooltip: {
            ...tooltipDefaults,
            callbacks: {
              label: ctx =>
                ' ' + ctx.label + ': $' + ctx.parsed.toFixed(2) +
                ' (' + ((ctx.parsed / ctx.dataset.data.reduce((a, b) => a + b, 0)) * 100).toFixed(1) + '%)',
            },
          },
        },
        cutout: '62%',
      },
    });
  })();
</script>
</body>
</html>`
