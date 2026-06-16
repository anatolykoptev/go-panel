package email

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net"
	"net/smtp"
	"strconv"
	"strings"
)

// SMTPConfig holds the SMTP delivery settings, injected by go-grad from env.
type SMTPConfig struct {
	Host     string
	Port     int
	Username string
	Password string
	From     string
	// TLS requests an explicit TLS posture. net/smtp.SendMail already upgrades
	// to STARTTLS automatically when the server advertises it; this flag is
	// reserved for go-grad to opt into stricter handling and is not load-bearing
	// in the default sender.
	TLS bool
}

// SMTPSender delivers mail via net/smtp. The send func is a seam for tests.
type SMTPSender struct {
	cfg  SMTPConfig
	send func(addr string, a smtp.Auth, from string, to []string, msg []byte) error
}

// NewSMTPSender returns an SMTPSender using net/smtp.SendMail.
func NewSMTPSender(cfg SMTPConfig) *SMTPSender {
	return &SMTPSender{cfg: cfg, send: smtp.SendMail}
}

// Send composes a multipart/alternative message and delivers it. The context is
// accepted for interface symmetry; net/smtp has no context-aware API.
func (s *SMTPSender) Send(_ context.Context, to, subject, htmlBody, textBody string) error {
	msg, err := buildMessage(s.cfg.From, to, subject, htmlBody, textBody)
	if err != nil {
		return err
	}
	addr := net.JoinHostPort(s.cfg.Host, strconv.Itoa(s.cfg.Port))
	auth := smtp.PlainAuth("", s.cfg.Username, s.cfg.Password, s.cfg.Host)
	if err := s.send(addr, auth, s.cfg.From, []string{to}, msg); err != nil {
		return fmt.Errorf("identity/email: smtp send: %w", err)
	}
	return nil
}

const crlf = "\r\n"

// boundaryBytes is the entropy of the MIME multipart boundary.
const boundaryBytes = 16

// buildMessage renders an RFC 5322 / MIME multipart/alternative message with a
// plain-text and an HTML part.
func buildMessage(from, to, subject, htmlBody, textBody string) ([]byte, error) {
	b := make([]byte, boundaryBytes)
	if _, err := rand.Read(b); err != nil {
		return nil, fmt.Errorf("identity/email: boundary: %w", err)
	}
	boundary := hex.EncodeToString(b)

	var sb strings.Builder
	sb.WriteString("From: " + from + crlf)
	sb.WriteString("To: " + to + crlf)
	sb.WriteString("Subject: " + subject + crlf)
	sb.WriteString("MIME-Version: 1.0" + crlf)
	sb.WriteString("Content-Type: multipart/alternative; boundary=\"" + boundary + "\"" + crlf + crlf)

	sb.WriteString("--" + boundary + crlf)
	sb.WriteString("Content-Type: text/plain; charset=UTF-8" + crlf + crlf)
	sb.WriteString(textBody + crlf + crlf)

	sb.WriteString("--" + boundary + crlf)
	sb.WriteString("Content-Type: text/html; charset=UTF-8" + crlf + crlf)
	sb.WriteString(htmlBody + crlf + crlf)

	sb.WriteString("--" + boundary + "--" + crlf)
	return []byte(sb.String()), nil
}
