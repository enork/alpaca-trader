package broker

import (
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"time"

	"cloud.google.com/go/civil"
	"github.com/alpacahq/alpaca-trade-api-go/v3/alpaca"
	"github.com/alpacahq/alpaca-trade-api-go/v3/marketdata"
	"github.com/enork/alpaca-trader/internal/config"
	"github.com/shopspring/decimal"
)

const maxRetries = 3

// Client wraps the Alpaca trading and market-data clients.
type Client struct {
	ac  *alpaca.Client
	md  *marketdata.Client
	log *slog.Logger
}

// New creates a broker Client from the given Alpaca config.
func New(cfg config.AlpacaConfig, log *slog.Logger) *Client {
	httpClient := &http.Client{Timeout: 15 * time.Second}

	ac := alpaca.NewClient(alpaca.ClientOpts{
		APIKey:     cfg.APIKey,
		APISecret:  cfg.APISecret,
		BaseURL:    cfg.BaseURL,
		HTTPClient: httpClient,
	})

	md := marketdata.NewClient(marketdata.ClientOpts{
		APIKey:     cfg.APIKey,
		APISecret:  cfg.APISecret,
		HTTPClient: httpClient,
	})

	return &Client{ac: ac, md: md, log: log}
}

// GetAccount returns the current account state.
func (c *Client) GetAccount() (*alpaca.Account, error) {
	var acct *alpaca.Account
	err := withRetry(c.log, "GetAccount", maxRetries, func() error {
		var e error
		acct, e = c.ac.GetAccount()
		return e
	})
	if err != nil {
		return nil, fmt.Errorf("get account: %w", err)
	}
	c.log.Debug("fetched account", "cash", acct.Cash, "status", acct.Status)
	return acct, nil
}

// GetPositions returns all current positions.
func (c *Client) GetPositions() ([]alpaca.Position, error) {
	var positions []alpaca.Position
	err := withRetry(c.log, "GetPositions", maxRetries, func() error {
		var e error
		positions, e = c.ac.GetPositions()
		return e
	})
	if err != nil {
		return nil, fmt.Errorf("get positions: %w", err)
	}
	c.log.Debug("fetched positions", "count", len(positions))
	return positions, nil
}

// GetOpenOrders returns all open orders.
func (c *Client) GetOpenOrders() ([]alpaca.Order, error) {
	var orders []alpaca.Order
	err := withRetry(c.log, "GetOpenOrders", maxRetries, func() error {
		var e error
		orders, e = c.ac.GetOrders(alpaca.GetOrdersRequest{Status: "open", Limit: 500})
		return e
	})
	if err != nil {
		return nil, fmt.Errorf("get open orders: %w", err)
	}
	c.log.Debug("fetched open orders", "count", len(orders))
	return orders, nil
}

// GetClock returns the current market clock, including whether the market is
// open and the times of the next open and close.
func (c *Client) GetClock() (*alpaca.Clock, error) {
	var clock *alpaca.Clock
	err := withRetry(c.log, "GetClock", maxRetries, func() error {
		var e error
		clock, e = c.ac.GetClock()
		return e
	})
	if err != nil {
		return nil, fmt.Errorf("get clock: %w", err)
	}
	c.log.Debug("fetched clock", "is_open", clock.IsOpen, "next_open", clock.NextOpen)
	return clock, nil
}

// GetLatestPrice returns the last trade price for a stock symbol.
func (c *Client) GetLatestPrice(symbol string) (float64, error) {
	var price float64
	err := withRetry(c.log, "GetLatestPrice:"+symbol, maxRetries, func() error {
		trade, e := c.md.GetLatestTrade(symbol, marketdata.GetLatestTradeRequest{})
		if e != nil {
			return e
		}
		price = trade.Price
		return nil
	})
	if err != nil {
		return 0, fmt.Errorf("get latest trade %s: %w", symbol, err)
	}
	c.log.Debug("fetched latest price", "symbol", symbol, "price", price)
	return price, nil
}

// PlaceOptionOrder submits a sell-to-open day limit order for an option contract.
func (c *Client) PlaceOptionOrder(optionSymbol string, contracts int, limitPrice float64) (*alpaca.Order, error) {
	qty := decimal.NewFromInt(int64(contracts))
	price := decimal.NewFromFloat(limitPrice)
	var order *alpaca.Order
	err := withRetry(c.log, "PlaceOptionOrder:"+optionSymbol, maxRetries, func() error {
		var e error
		order, e = c.ac.PlaceOrder(alpaca.PlaceOrderRequest{
			Symbol:         optionSymbol,
			Qty:            &qty,
			Side:           alpaca.Sell,
			Type:           alpaca.Limit,
			TimeInForce:    alpaca.Day,
			LimitPrice:     &price,
			PositionIntent: alpaca.SellToOpen,
		})
		return e
	})
	if err != nil {
		return nil, fmt.Errorf("place option order %s: %w", optionSymbol, err)
	}
	c.log.Debug("option order submitted", "symbol", optionSymbol, "contracts", contracts, "limit", limitPrice, "order_id", order.ID)
	return order, nil
}

// GetRecentActivities returns account activities (fills, dividends, etc.) after
// the given time, ordered newest-first.
func (c *Client) GetRecentActivities(after time.Time) ([]alpaca.AccountActivity, error) {
	var acts []alpaca.AccountActivity
	err := withRetry(c.log, "GetRecentActivities", maxRetries, func() error {
		var e error
		acts, e = c.ac.GetAccountActivities(alpaca.GetAccountActivitiesRequest{
			After:     after,
			Direction: "desc",
			PageSize:  100,
		})
		return e
	})
	if err != nil {
		return nil, fmt.Errorf("get account activities: %w", err)
	}
	c.log.Debug("fetched recent activities", "count", len(acts), "after", after)
	return acts, nil
}

// GetOptionChain returns snapshots (including bid/ask quotes) for all active option
// contracts on the underlying symbol within the given expiry window and type filter.
func (c *Client) GetOptionChain(
	underlying string,
	optType string,
	expiryGte, expiryLte civil.Date,
) (map[string]marketdata.OptionSnapshot, error) {
	var snapshots map[string]marketdata.OptionSnapshot
	err := withRetry(c.log, "GetOptionChain:"+underlying, maxRetries, func() error {
		var e error
		snapshots, e = c.md.GetOptionChain(underlying, marketdata.GetOptionChainRequest{
			Type:              optType,
			ExpirationDateGte: expiryGte,
			ExpirationDateLte: expiryLte,
		})
		return e
	})
	if err != nil {
		return nil, fmt.Errorf("get option chain %s: %w", underlying, err)
	}
	c.log.Debug("fetched option chain", "underlying", underlying, "type", optType, "contracts", len(snapshots))
	return snapshots, nil
}

// withRetry calls fn up to maxAttempts times with exponential backoff (1s, 2s, 4s…).
// Permanent client errors (4xx other than 429) are not retried.
func withRetry(log *slog.Logger, op string, maxAttempts int, fn func() error) error {
	var err error
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		err = fn()
		if err == nil {
			return nil
		}
		if !isRetryable(err) {
			return err
		}
		if attempt < maxAttempts {
			backoff := time.Duration(1<<(attempt-1)) * time.Second
			log.Warn("retrying after error", "op", op, "attempt", attempt, "backoff", backoff, "error", err)
			time.Sleep(backoff)
		}
	}
	return fmt.Errorf("all %d attempts failed for %s: %w", maxAttempts, op, err)
}

// isRetryable returns false for permanent HTTP client errors (400, 401, 403, 404, 422).
// Everything else — rate limits (429), server errors (5xx), and network errors — is retried.
func isRetryable(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	for _, code := range []string{"400 ", "401 ", "403 ", "404 ", "422 "} {
		if strings.Contains(msg, code) {
			return false
		}
	}
	var netErr net.Error
	if errors.As(err, &netErr) {
		return true
	}
	return true
}
