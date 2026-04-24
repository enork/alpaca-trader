# Alpaca Trading Bot

An automated options trading bot that sells cash-secured puts and covered calls on a configurable watchlist of stocks using the [Alpaca](https://alpaca.markets) brokerage API.

## Strategy

For each enabled symbol the bot runs the following decision tree on every cycle:

1. **Open option activity exists** → skip (do nothing, re-evaluate next cycle)
2. **≥ 100 shares held** → sell covered calls (`floor(shares / 100)` contracts)
3. **Otherwise** → sell 1 cash-secured put (subject to cash guard)

A **cash guard** runs before every put order: the bot confirms that current cash covers all existing put obligations plus the new one. If not, the symbol is skipped and an email alert is sent.

All orders are sell-to-open day limit orders priced at the bid (conservative fill estimate).

## Prerequisites

- Go 1.21+
- An [Alpaca](https://alpaca.markets) account (paper or live)
- A Gmail account with an [App Password](https://support.google.com/accounts/answer/185833) for trade alerts

## Setup

**1. Clone and enter the repo**

```bash
git clone https://github.com/enork/alpaca-trader
cd alpaca-trader
```

**2. Create your `.env` file**

```bash
cp .env.example .env
```

Edit `.env` and fill in your credentials:

```
ALPACA_API_KEY=your_api_key
ALPACA_API_SECRET=your_api_secret
GMAIL_USER=you@gmail.com
GMAIL_APP_PASSWORD=xxxx xxxx xxxx xxxx
```

> `.env` is gitignored and never committed.

**3. Review `config.yaml`**

```yaml
alpaca:
  base_url: "https://paper-api.alpaca.markets"  # swap for live URL

trading:
  max_dte: 2          # expiry window in trading days
  run_on_startup: true

symbols:
  - ticker: PLUG
    enabled: true

notify:
  smtp_host: "smtp.gmail.com"
  smtp_port: 587
  to: "trader@example.com"
```

Set `enabled: true` on each symbol you want traded. The `from` address is read from `GMAIL_USER`.

## Usage

```bash
# Run one trading cycle
make run

# Build a binary
make build
./bin/bot

# Send a test cash-guard alert email
make notify-test

# Run the integration test against the paper account
make test-integration

# Remove build artefacts
make clean
```

## Configuration Reference

### `alpaca`

| Key | Env override | Description |
|---|---|---|
| `api_key` | `ALPACA_API_KEY` | Alpaca API key |
| `api_secret` | `ALPACA_API_SECRET` | Alpaca API secret |
| `base_url` | — | `https://paper-api.alpaca.markets` (paper) or `https://api.alpaca.markets` (live) |

### `trading`

| Key | Description |
|---|---|
| `max_dte` | Option expiry look-ahead window in trading days (default `2`) |
| `run_on_startup` | Run a cycle immediately on startup |
| `run_on_open` | Run a cycle on market open each trading day |
| `run_on_cron` | Cron expression for scheduled runs (e.g. `0 9 * * 1-5`) |

### `symbols`

List of tickers to trade. Set `enabled: false` to pause a symbol without removing it.

### `notify`

| Key | Env override | Description |
|---|---|
| `smtp_host` | — | SMTP server (default `smtp.gmail.com`) |
| `smtp_port` | — | SMTP port (default `587`) |
| `from` | `GMAIL_USER` | Sender address |
| `to` | — | Recipient address for alerts |
| — | `GMAIL_APP_PASSWORD` | Gmail App Password (never stored in config) |

## Architecture

```
cmd/bot/          — entry point
internal/
  config/         — YAML config loading and validation
  broker/         — Alpaca API wrapper (trading + market data, retry logic)
  options/        — option chain queries and strike-selection algorithms
  trading/        — per-symbol decision tree, cash guard, order placement
  notify/         — SMTP email alerts
```

All log output is structured JSON on stdout (`log/slog`).

## Key Rules

1. **Never sell an uncovered put** — cash guard must pass before every put order.
2. **Never sell a call below cost basis** — prevents locking in a loss on assignment.
3. **One put per symbol** — only 1 contract when initiating a new position.
4. **Skip, don't error** — a disqualified symbol logs and continues; the cycle never halts.
5. **Day orders only** — no GTC; re-evaluated each cycle.
