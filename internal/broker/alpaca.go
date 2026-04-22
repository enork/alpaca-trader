package broker

import (
	"fmt"
	"net/http"
	"time"

	"cloud.google.com/go/civil"
	"github.com/alpacahq/alpaca-trade-api-go/v3/alpaca"
	"github.com/alpacahq/alpaca-trade-api-go/v3/marketdata"
	"github.com/enork/alpaca-trader/internal/config"
)

// Client wraps the Alpaca trading and market-data clients.
type Client struct {
	ac *alpaca.Client
	md *marketdata.Client
}

// New creates a broker Client from the given Alpaca config.
func New(cfg config.AlpacaConfig) *Client {
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

	return &Client{ac: ac, md: md}
}

// GetAccount returns the current account state.
func (c *Client) GetAccount() (*alpaca.Account, error) {
	acct, err := c.ac.GetAccount()
	if err != nil {
		return nil, fmt.Errorf("get account: %w", err)
	}
	return acct, nil
}

// GetPositions returns all current positions.
func (c *Client) GetPositions() ([]alpaca.Position, error) {
	positions, err := c.ac.GetPositions()
	if err != nil {
		return nil, fmt.Errorf("get positions: %w", err)
	}
	return positions, nil
}

// GetOpenOrders returns all open orders.
func (c *Client) GetOpenOrders() ([]alpaca.Order, error) {
	orders, err := c.ac.GetOrders(alpaca.GetOrdersRequest{
		Status: "open",
		Limit:  500,
	})
	if err != nil {
		return nil, fmt.Errorf("get open orders: %w", err)
	}
	return orders, nil
}

// GetLatestPrice returns the last trade price for a stock symbol.
func (c *Client) GetLatestPrice(symbol string) (float64, error) {
	trade, err := c.md.GetLatestTrade(symbol, marketdata.GetLatestTradeRequest{})
	if err != nil {
		return 0, fmt.Errorf("get latest trade %s: %w", symbol, err)
	}
	return trade.Price, nil
}

// GetOptionChain returns snapshots (including bid/ask quotes) for all active option
// contracts on the underlying symbol within the given expiry window and type filter.
func (c *Client) GetOptionChain(
	underlying string,
	optType string,
	expiryGte, expiryLte civil.Date,
) (map[string]marketdata.OptionSnapshot, error) {
	snapshots, err := c.md.GetOptionChain(underlying, marketdata.GetOptionChainRequest{
		Type:              optType,
		ExpirationDateGte: expiryGte,
		ExpirationDateLte: expiryLte,
	})
	if err != nil {
		return nil, fmt.Errorf("get option chain %s: %w", underlying, err)
	}
	return snapshots, nil
}
