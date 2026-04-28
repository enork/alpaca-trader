package config

import (
	"fmt"
	"os"
	"strconv"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Alpaca  AlpacaConfig  `yaml:"alpaca"`
	Trading TradingConfig `yaml:"trading"`
	Symbols []Symbol      `yaml:"symbols"`
	Notify  NotifyConfig  `yaml:"notify"`
}

type AlpacaConfig struct {
	APIKey       string `yaml:"api_key"`
	APISecret    string `yaml:"api_secret"`
	BaseURL      string `yaml:"base_url"`
	PaperTrading bool   `yaml:"paper_trading"`
}

type TradingConfig struct {
	MaxDTE       int    `yaml:"max_dte"`
	RunOnStartup bool   `yaml:"run_on_startup"`
	RunOnOpen    bool   `yaml:"run_on_open"`
	RunOnCron    string `yaml:"run_on_cron"`
}

type Symbol struct {
	Ticker  string `yaml:"ticker"`
	Enabled bool   `yaml:"enabled"`
}

type NotifyConfig struct {
	SMTPHost          string `yaml:"smtp_host"`
	SMTPPort          int    `yaml:"smtp_port"`
	From              string `yaml:"from"`
	To                string `yaml:"to"`
	Enabled           bool   `yaml:"enabled"`
	RunSummaryEnabled bool   `yaml:"run_summary_enabled"`
}

func Load(path string) (*Config, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open config: %w", err)
	}
	defer f.Close()

	var cfg Config
	if err := yaml.NewDecoder(f).Decode(&cfg); err != nil {
		return nil, fmt.Errorf("decode config: %w", err)
	}

	applyEnvOverrides(&cfg)

	if err := validate(&cfg); err != nil {
		return nil, err
	}

	return &cfg, nil
}

func applyEnvOverrides(cfg *Config) {
	if v := os.Getenv("ALPACA_API_KEY"); v != "" {
		cfg.Alpaca.APIKey = v
	}
	if v := os.Getenv("ALPACA_API_SECRET"); v != "" {
		cfg.Alpaca.APISecret = v
	}
	if v := os.Getenv("PAPER_TRADING"); v != "" {
		cfg.Alpaca.PaperTrading = v == "true" || v == "1"
	}
	if v := os.Getenv("CASHGUARD_NOTIFICATION_ENABLED"); v != "" {
		cfg.Notify.Enabled = v == "true" || v == "1"
	}
	if v := os.Getenv("RUN_SUMMARY_ENABLED"); v != "" {
		cfg.Notify.RunSummaryEnabled = v == "true" || v == "1"
	}
	if v := os.Getenv("GMAIL_USER"); v != "" {
		cfg.Notify.From = v
	}
	// SMTP port override (optional)
	if v := os.Getenv("SMTP_PORT"); v != "" {
		if p, err := strconv.Atoi(v); err == nil {
			cfg.Notify.SMTPPort = p
		}
	}
}

func validate(cfg *Config) error {
	if cfg.Alpaca.APIKey == "" {
		return fmt.Errorf("alpaca.api_key is required (or set ALPACA_API_KEY)")
	}
	if cfg.Alpaca.APISecret == "" {
		return fmt.Errorf("alpaca.api_secret is required (or set ALPACA_API_SECRET)")
	}
	if cfg.Alpaca.BaseURL == "" {
		return fmt.Errorf("alpaca.base_url is required")
	}
	if cfg.Trading.MaxDTE <= 0 {
		return fmt.Errorf("trading.max_dte must be > 0")
	}
	if cfg.Notify.To == "" {
		return fmt.Errorf("notify.to is required")
	}
	return nil
}

// EnabledSymbols returns only the symbols with enabled: true.
func (c *Config) EnabledSymbols() []Symbol {
	var out []Symbol
	for _, s := range c.Symbols {
		if s.Enabled {
			out = append(out, s)
		}
	}
	return out
}
