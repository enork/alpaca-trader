# Alpaca Trading Bot — Project Plan

## Overview

A Go-based automated options trading bot that uses the Alpaca brokerage API to sell
cash-secured puts and covered calls on a configurable watchlist of stocks. The bot
runs on startup (or on a schedule), evaluates each symbol, and places trades
according to a strict set of rules to avoid uncovered risk and to maintain
adequate cash reserves.

---

## Architecture

```
alpaca-trader/
├── cmd/
│   └── bot/
│       └── main.go          # Entry point, wires dependencies, runs trading cycle
├── internal/
│   ├── config/
│   │   └── config.go        # Loads config.yaml; defines Config, Symbol structs
│   ├── broker/
│   │   └── alpaca.go        # Thin wrapper around alpaca-trade-api-go client
│   ├── trading/
│   │   ├── engine.go        # Orchestrates the per-symbol trading loop
│   │   ├── calls.go         # Covered-call selection and order logic
│   │   └── puts.go          # Secured-put selection and order logic
│   ├── options/
│   │   └── selector.go      # Option chain queries + strike-selection algorithms
│   └── notify/
│       └── email.go         # SMTP email notifications
├── config.yaml              # Runtime configuration (symbols, thresholds, SMTP, etc.)
├── go.mod
└── go.sum
```

### Key Dependencies

| Dependency | Purpose |
|---|---|
| `github.com/alpacahq/alpaca-trade-api-go/v3` | Brokerage API (orders, positions, option chains) |
| `gopkg.in/yaml.v3` | Config file parsing |
| Standard library `net/smtp` | Email notifications |

---

## Configuration (`config.yaml`)

```yaml
alpaca:
  api_key: ""          # or from env: ALPACA_API_KEY
  api_secret: ""       # or from env: ALPACA_API_SECRET
  base_url: "https://paper-api.alpaca.markets"  # swap for live URL

trading:
  max_dte: 2           # Look-ahead window in trading days for option expiry

symbols:
  - ticker: AAPL
    enabled: true
  - ticker: TSLA
    enabled: true

notify:
  smtp_host: "smtp.gmail.com"
  smtp_port: 587
  from: ""             # Gmail address; from env: GMAIL_USER
  to: "trader@example.com"
```

Gmail credentials are provided exclusively via environment variables — do **not** store them in `config.yaml`:

| Variable | Description |
|---|---|
| `GMAIL_USER` | Gmail address used to authenticate and send alerts |
| `GMAIL_APP_PASSWORD` | Google App Password (not the account password) |

All other sensitive values (`ALPACA_API_KEY`, `ALPACA_API_SECRET`) can also be overridden via environment variables.

---

## Trading Logic

### Startup Cycle

On each run (startup or scheduled), the bot:

1. Loads configuration.
2. Fetches current account state (cash balance, positions, open orders).
3. Iterates over every `enabled: true` symbol in order.
4. For each symbol, applies the decision tree below.

### Per-Symbol Decision Tree

```
For each enabled symbol:
│
├─ Is there an open call OR put option order/position for this symbol?
│   └─ YES → skip (do nothing)
│
├─ Do we own >= 100 shares of this symbol?
│   └─ YES → Sell covered calls
│             • Contracts = floor(shares / 100)
│             • Select best call option (see Options Selection)
│             • Place sell-to-open limit order
│
└─ NO shares (< 100) AND no open put position?
    └─ YES → Sell 1 secured put
              • Check cash sufficiency (see Cash Guard)
              • Select best put option (see Options Selection)
              • Place sell-to-open limit order
```

### Cash Guard (before every put sale)

Before placing any put order:

1. Sum the total notional obligation of all existing open put positions:
   `existing_put_exposure = Σ (strike_price × 100 × contracts)` for every open put.
2. Calculate the obligation for the new put being considered:
   `new_put_obligation = strike_price × 100`.
3. Verify: `account_cash >= existing_put_exposure + new_put_obligation`.
4. If the check fails → **do not place the trade** and send an email alert with:
   - Symbol being skipped
   - Current cash balance
   - Total existing put exposure
   - Required cash for the new put
   - Shortfall amount

---

## Options Selection Strategy

### Expiry Window

- Query option chains for expirations from **today** through **today + `max_dte` trading days** (default 2).
- Prefer the nearest expiry with a qualifying strike to maximise theta decay.

### Covered Call Strike Selection

Goal: maximise premium received without risking a net loss if the option is exercised.

1. Retrieve the cost basis of the position from the Alpaca API (average entry price per share).
2. Filter the call chain to strikes **≥ cost basis**.
3. Among qualifying strikes, choose the one with the **highest premium-to-strike-distance ratio** (efficiency):
   ```
   efficiency = bid_price / (strike - cost_basis + 1)
   ```
   Using `bid_price` (conservative fill estimate).
4. If no qualifying strikes exist for the expiry window, skip and log a warning.

### Cash-Secured Put Strike Selection

Goal: sell out-of-the-money puts to collect premium while accepting potential assignment.

1. Fetch the current market price (last trade / mid-quote) for the symbol.
2. Filter the put chain to strikes **< current market price** (out of the money).
3. Among qualifying strikes, choose the one with the **highest premium-to-strike ratio** (efficiency):
   ```
   efficiency = bid_price / strike
   ```
4. If no qualifying strikes exist, skip and log a warning.

---

## Order Placement

- All orders are **limit orders** priced at the **bid price** of the selected option.
- Order `time_in_force`: `day`.
- Orders are sell-to-open (`side: sell`, `position_intent: sell_to_open`).
- After placing, log the order ID, symbol, strike, expiry, and premium.

---

## Error Handling & Observability

| Situation | Behaviour |
|---|---|
| API rate limit / transient error | Exponential back-off, up to 3 retries |
| Insufficient cash for put | Email notification, skip symbol, continue loop |
| No qualifying option found | Log warning, skip symbol, continue loop |
| Order rejection by broker | Log full rejection reason, continue loop |
| Unrecoverable startup error | Log fatal, exit non-zero |

All log output is structured JSON to stdout (`log/slog` standard library package).

---

## Implementation Phases

### Phase 1 — Foundation
- [ ] `go mod init` with module path
- [ ] Add `alpaca-trade-api-go` dependency
- [ ] Implement `config` package (load + validate `config.yaml`)
- [ ] Implement `broker` package (account info, positions, open orders)

### Phase 2 — Option Chain & Selection
- [ ] Implement `options/selector.go` (chain fetching, expiry filtering)
- [ ] Implement covered-call strike selection algorithm
- [ ] Implement cash-secured put strike selection algorithm

### Phase 3 — Trading Engine
- [ ] Implement cash guard check
- [ ] Implement per-symbol decision tree in `trading/engine.go`
- [ ] Wire call and put order placement

### Phase 4 — Notifications
- [ ] Implement `notify/email.go` (SMTP)
- [ ] Integrate cash-guard failure notifications

### Phase 5 — Hardening
- [ ] Structured logging throughout
- [ ] Retry logic for Alpaca API calls
- [ ] End-to-end test against Alpaca paper trading environment
- [ ] README with setup and configuration instructions

---

## Key Rules Summary

1. **Never sell an uncovered put** — cash guard must pass before every put order.
2. **Never sell a call below cost basis** — protects against forced realisation of a loss.
3. **One put per symbol** — only 1 contract when initiating a new put position.
4. **Skip, don't error** — a disqualified symbol logs and continues; it never halts the cycle.
5. **Use bid prices** — conservative pricing reduces the risk of unfilled limit orders sitting open.
6. **Day orders only** — no GTC; re-evaluate each run cycle.
