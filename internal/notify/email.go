package notify

import (
	"crypto/tls"
	"fmt"
	"net"
	"net/smtp"
	"os"
	"strings"
	"time"

	"github.com/enork/alpaca-trader/internal/config"
)

// CashGuardAlert holds the data for a cash-guard notification email.
type CashGuardAlert struct {
	SkippedTickers   []string
	Cash             float64
	ExistingExposure float64
	AdditionalTotal  float64
	AdditionalPerPut float64
}

// Notifier sends email alerts via SMTP.
type Notifier struct {
	cfg      config.NotifyConfig
	password string
}

// New returns a Notifier, reading GMAIL_APP_PASSWORD from the environment.
func New(cfg config.NotifyConfig) (*Notifier, error) {
	password := strings.Trim(os.Getenv("GMAIL_APP_PASSWORD"), `"'`)
	if password == "" {
		return nil, fmt.Errorf("GMAIL_APP_PASSWORD env var is required for notifications")
	}
	if cfg.From == "" {
		return nil, fmt.Errorf("notify.from (GMAIL_USER) is required for notifications")
	}
	return &Notifier{cfg: cfg, password: password}, nil
}

// SendCashGuardAlert emails a summary of symbols skipped due to insufficient cash.
func (n *Notifier) SendCashGuardAlert(alert CashGuardAlert) error {
	subject := fmt.Sprintf("[alpaca-trader] Cash guard blocked %d put(s) — %s",
		len(alert.SkippedTickers), time.Now().Format("2006-01-02 15:04 MST"))

	body := buildCashGuardBody(alert)
	return n.send(subject, body)
}

func buildCashGuardBody(a CashGuardAlert) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Cash guard prevented the following put(s) from being placed:\n\n")
	for _, t := range a.SkippedTickers {
		fmt.Fprintf(&b, "  • %s\n", t)
	}
	fmt.Fprintf(&b, "\nAccount summary:\n")
	fmt.Fprintf(&b, "  Cash balance:              $%.2f\n", a.Cash)
	fmt.Fprintf(&b, "  Existing put exposure:     $%.2f\n", a.ExistingExposure)
	fmt.Fprintf(&b, "  Additional cash needed:    $%.2f (total)\n", a.AdditionalTotal)
	fmt.Fprintf(&b, "  Additional cash needed:    $%.2f (per skipped put)\n", a.AdditionalPerPut)
	fmt.Fprintf(&b, "\nAdd funds or close existing put positions to enable these trades.\n")
	return b.String()
}

func (n *Notifier) sendHTML(subject, html string) error {
	addr := fmt.Sprintf("%s:%d", n.cfg.SMTPHost, n.cfg.SMTPPort)
	auth := smtp.PlainAuth("", n.cfg.From, n.password, n.cfg.SMTPHost)
	msg := fmt.Sprintf(
		"From: %s\r\nTo: %s\r\nSubject: %s\r\nMIME-Version: 1.0\r\nContent-Type: text/html; charset=utf-8\r\n\r\n%s",
		n.cfg.From, n.cfg.To, subject, html,
	)
	conn, err := net.DialTimeout("tcp", addr, 10*time.Second)
	if err != nil {
		return fmt.Errorf("smtp dial: %w", err)
	}
	client, err := smtp.NewClient(conn, n.cfg.SMTPHost)
	if err != nil {
		return fmt.Errorf("smtp client: %w", err)
	}
	defer client.Close()
	if err := client.StartTLS(&tls.Config{ServerName: n.cfg.SMTPHost}); err != nil {
		return fmt.Errorf("smtp starttls: %w", err)
	}
	if err := client.Auth(auth); err != nil {
		return fmt.Errorf("smtp auth: %w", err)
	}
	if err := client.Mail(n.cfg.From); err != nil {
		return fmt.Errorf("smtp mail from: %w", err)
	}
	if err := client.Rcpt(n.cfg.To); err != nil {
		return fmt.Errorf("smtp rcpt: %w", err)
	}
	w, err := client.Data()
	if err != nil {
		return fmt.Errorf("smtp data: %w", err)
	}
	if _, err := fmt.Fprint(w, msg); err != nil {
		return fmt.Errorf("smtp write: %w", err)
	}
	if err := w.Close(); err != nil {
		return fmt.Errorf("smtp close data: %w", err)
	}
	return client.Quit()
}

func (n *Notifier) send(subject, body string) error {
	addr := fmt.Sprintf("%s:%d", n.cfg.SMTPHost, n.cfg.SMTPPort)
	auth := smtp.PlainAuth("", n.cfg.From, n.password, n.cfg.SMTPHost)

	msg := buildMIME(n.cfg.From, n.cfg.To, subject, body)

	// Port 587 requires STARTTLS; use a manual dial so we can upgrade the connection.
	conn, err := net.DialTimeout("tcp", addr, 10*time.Second)
	if err != nil {
		return fmt.Errorf("smtp dial: %w", err)
	}
	client, err := smtp.NewClient(conn, n.cfg.SMTPHost)
	if err != nil {
		return fmt.Errorf("smtp client: %w", err)
	}
	defer client.Close()

	if err := client.StartTLS(&tls.Config{ServerName: n.cfg.SMTPHost}); err != nil {
		return fmt.Errorf("smtp starttls: %w", err)
	}
	if err := client.Auth(auth); err != nil {
		return fmt.Errorf("smtp auth: %w", err)
	}
	if err := client.Mail(n.cfg.From); err != nil {
		return fmt.Errorf("smtp mail from: %w", err)
	}
	if err := client.Rcpt(n.cfg.To); err != nil {
		return fmt.Errorf("smtp rcpt: %w", err)
	}
	w, err := client.Data()
	if err != nil {
		return fmt.Errorf("smtp data: %w", err)
	}
	if _, err := fmt.Fprint(w, msg); err != nil {
		return fmt.Errorf("smtp write: %w", err)
	}
	if err := w.Close(); err != nil {
		return fmt.Errorf("smtp close data: %w", err)
	}
	return client.Quit()
}

func buildMIME(from, to, subject, body string) string {
	return fmt.Sprintf(
		"From: %s\r\nTo: %s\r\nSubject: %s\r\nMIME-Version: 1.0\r\nContent-Type: text/plain; charset=utf-8\r\n\r\n%s",
		from, to, subject, body,
	)
}
