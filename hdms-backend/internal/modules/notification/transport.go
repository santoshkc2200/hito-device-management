package notification

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"net/smtp"
	"sync"
)

// Message represents an email notification to be delivered.
type Message struct {
	To       string
	Subject  string
	TextBody string
	HTMLBody string
	From     string
	ReplyTo  string
}

// Transport abstracts message dispatch so tests can mock the relay
// and production can connect to the hospital SMTP relay (6.2c).
type Transport interface {
	Send(ctx context.Context, msg Message) error
}

// SMTPConfig holds parameters for the hospital SMTP relay.
type SMTPConfig struct {
	Host         string
	Port         int
	Username     string
	Password     string
	FromAddress  string
	ReplyAddress string
}

// SMTPTransport sends email via standard SMTP.
type SMTPTransport struct {
	cfg SMTPConfig
}

// NewSMTPTransport creates an SMTP transport. If host is empty, it defaults
// to a local development catcher (localhost:1025).
func NewSMTPTransport(cfg SMTPConfig) *SMTPTransport {
	if cfg.Host == "" {
		cfg.Host = "localhost"
	}
	if cfg.Port == 0 {
		cfg.Port = 1025
	}
	if cfg.FromAddress == "" {
		cfg.FromAddress = "hdms@hospital.local"
	}
	return &SMTPTransport{cfg: cfg}
}

// Send delivers one message to the configured SMTP relay.
func (t *SMTPTransport) Send(ctx context.Context, msg Message) error {
	addr := fmt.Sprintf("%s:%d", t.cfg.Host, t.cfg.Port)

	from := msg.From
	if from == "" {
		from = t.cfg.FromAddress
	}

	boundary := "hdms-multipart-boundary-12345"
	rawMessage := fmt.Sprintf("From: %s\r\n"+
		"To: %s\r\n"+
		"Reply-To: %s\r\n"+
		"Subject: %s\r\n"+
		"MIME-Version: 1.0\r\n"+
		"Content-Type: multipart/alternative; boundary=%s\r\n\r\n"+
		"--%s\r\n"+
		"Content-Type: text/plain; charset=utf-8\r\n\r\n"+
		"%s\r\n\r\n"+
		"--%s\r\n"+
		"Content-Type: text/html; charset=utf-8\r\n\r\n"+
		"%s\r\n\r\n"+
		"--%s--\r\n",
		from, msg.To, msg.ReplyTo, msg.Subject, boundary,
		boundary, msg.TextBody,
		boundary, msg.HTMLBody,
		boundary)

	var auth smtp.Auth
	if t.cfg.Username != "" && t.cfg.Password != "" {
		auth = smtp.PlainAuth("", t.cfg.Username, t.cfg.Password, t.cfg.Host)
	}

	// Dial with context timeout
	dialer := net.Dialer{}
	conn, err := dialer.DialContext(ctx, "tcp", addr)
	if err != nil {
		return fmt.Errorf("smtp: dial %s: %w", addr, err)
	}
	defer func() { _ = conn.Close() }()

	c, err := smtp.NewClient(conn, t.cfg.Host)
	if err != nil {
		return fmt.Errorf("smtp: client creation: %w", err)
	}
	defer func() { _ = c.Quit() }()

	if ok, _ := c.Extension("STARTTLS"); ok {
		tlsConfig := &tls.Config{ServerName: t.cfg.Host, MinVersion: tls.VersionTLS12}
		if err := c.StartTLS(tlsConfig); err != nil {
			return fmt.Errorf("smtp: starttls: %w", err)
		}
	}

	if auth != nil {
		if ok, _ := c.Extension("AUTH"); ok {
			if err := c.Auth(auth); err != nil {
				return fmt.Errorf("smtp: auth: %w", err)
			}
		}
	}

	if err := c.Mail(from); err != nil {
		return fmt.Errorf("smtp: mail from: %w", err)
	}
	if err := c.Rcpt(msg.To); err != nil {
		return fmt.Errorf("smtp: rcpt to: %w", err)
	}

	w, err := c.Data()
	if err != nil {
		return fmt.Errorf("smtp: data: %w", err)
	}
	defer func() { _ = w.Close() }()

	if _, err := w.Write([]byte(rawMessage)); err != nil {
		return fmt.Errorf("smtp: write body: %w", err)
	}

	return nil
}

// MemoryTransport is an in-memory transport for testing.
type MemoryTransport struct {
	mu        sync.Mutex
	Messages  []Message
	FailErr   error
	FailCount int
	attempts  int
}

// NewMemoryTransport creates a new test transport.
func NewMemoryTransport() *MemoryTransport {
	return &MemoryTransport{}
}

// Send records msg in memory or simulates failure.
func (m *MemoryTransport) Send(ctx context.Context, msg Message) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.attempts++
	if m.FailErr != nil {
		if m.FailCount == 0 || m.attempts <= m.FailCount {
			return m.FailErr
		}
	}
	m.Messages = append(m.Messages, msg)
	return nil
}

// GetMessages returns all captured messages.
func (m *MemoryTransport) GetMessages() []Message {
	m.mu.Lock()
	defer m.mu.Unlock()
	copied := make([]Message, len(m.Messages))
	copy(copied, m.Messages)
	return copied
}

// Reset clears the recorded messages and failure state.
func (m *MemoryTransport) Reset() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Messages = nil
	m.FailErr = nil
	m.FailCount = 0
	m.attempts = 0
}
