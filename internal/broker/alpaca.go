package broker

import (
	"fmt"
	"net/http"
	"time"

	"github.com/alpacahq/alpaca-trade-api-go/v3/alpaca"
	"github.com/enork/alpaca-trader/internal/config"
)

// Client wraps the Alpaca trading client.
type Client struct {
	ac *alpaca.Client
}

// New creates a broker Client from the given Alpaca config.
func New(cfg config.AlpacaConfig) *Client {
	ac := alpaca.NewClient(alpaca.ClientOpts{
		APIKey:    cfg.APIKey,
		APISecret: cfg.APISecret,
		BaseURL:   cfg.BaseURL,
		HTTPClient: &http.Client{
			Timeout: 15 * time.Second,
		},
	})
	return &Client{ac: ac}
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
