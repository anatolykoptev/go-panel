package email

import (
	"context"
	"crypto/rand"
	"crypto/tls"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"mime"
	"net"
	"net/mail"
	"net/smtp"
	"strconv"
	"strings"
	"time"
)

// implicitTLSPort is the SMTPS port: the transport is TLS from the first byte
// (no STARTTLS upgrade). Any other port uses mandatory STARTTLS.
const implicitTLSPort = 465

// SMTPConfig holds the SMTP delivery settings, injected by go-grad from env.
//
// Transport encryption is MANDATORY and not configurable: magic-link emails carry
// a bearer token and the SMTP credentials are sent during AUTH, so neither may
// ever traverse a cleartext hop. On port 465 the sender uses implicit TLS; on any
// other port it REQUIRES STARTTLS and fails closed if the server does not offer
// it (SEC-CR-002 - a MITM that strips the STARTTLS advertisement must not be able
// to downgrade the session to plaintext).
type SMTPConfig struct {
	Host     string
	Port     int
	Username string
	Password string
	From     string
	// FromName is an optional display name for the From HEADER
	// (`"piter.now" <noreply@send.piter.now>`). A bare-address From with no
	// display name is a bulk-mail fingerprint for inbox classifiers; naming
	// the sender is what keeps transactional mail out of the newsletter tab.
	// Non-ASCII names are RFC 2047-encoded by net/mail. The SMTP envelope
	// (MAIL FROM) always stays the bare From address (RFC 5321 addr-spec).
	// Empty preserves the previous bare-address header.
	FromName string
}

// smtpClient is the subset of *smtp.Client that Send drives. It is an interface
// so tests can substitute a fake and assert that STARTTLS runs before AUTH and
// that a server without STARTTLS support is rejected.
type smtpClient interface {
	Extension(name string) (bool, string)
	StartTLS(config *tls.Config) error
	Auth(a smtp.Auth) error
	Mail(from string) error
	Rcpt(to string) error
	Data() (io.WriteCloser, error)
	Quit() error
	Close() error
}

// dialFunc opens a connection to the SMTP server and reports whether the
// transport is ALREADY encrypted (implicit TLS / port 465). It is a seam for
// tests.
type dialFunc func(cfg SMTPConfig) (client smtpClient, encrypted bool, err error)

// SMTPSender delivers mail via net/smtp with mandatory transport encryption.
type SMTPSender struct {
	cfg  SMTPConfig
	dial dialFunc
}

// NewSMTPSender returns an SMTPSender that enforces TLS on every send.
func NewSMTPSender(cfg SMTPConfig) *SMTPSender {
	return &SMTPSender{cfg: cfg, dial: dialSMTP}
}

// dialSMTP is the production dialer: implicit TLS on port 465, otherwise a
// plaintext TCP connection that Send upgrades via mandatory STARTTLS.
func dialSMTP(cfg SMTPConfig) (smtpClient, bool, error) {
	addr := net.JoinHostPort(cfg.Host, strconv.Itoa(cfg.Port))
	if cfg.Port == implicitTLSPort {
		conn, err := tls.Dial("tcp", addr, tlsConfigFor(cfg.Host)) //nolint:noctx // net/smtp (smtp.Dial + client ops below) has no context-aware API; a ctx-aware TLS dial alone yields no cancellation benefit. See Send.
		if err != nil {
			return nil, false, fmt.Errorf("identity/email: tls dial: %w", err)
		}
		c, err := smtp.NewClient(conn, cfg.Host)
		if err != nil {
			_ = conn.Close()
			return nil, false, fmt.Errorf("identity/email: smtp client: %w", err)
		}
		return c, true, nil
	}
	c, err := smtp.Dial(addr)
	if err != nil {
		return nil, false, fmt.Errorf("identity/email: dial: %w", err)
	}
	return c, false, nil
}

// tlsConfigFor is the shared TLS posture: verified server identity, TLS 1.2 floor.
func tlsConfigFor(host string) *tls.Config {
	return &tls.Config{ServerName: host, MinVersion: tls.VersionTLS12}
}

// Send composes a multipart/alternative message and delivers it over a TLS-
// protected SMTP session. The context is accepted for interface symmetry;
// net/smtp has no context-aware API.
func (s *SMTPSender) Send(ctx context.Context, to, subject, htmlBody, textBody string) error {
	return s.SendWithReplyTo(ctx, to, "", subject, htmlBody, textBody)
}

// SendWithReplyTo is Send plus a Reply-To header (ReplyToSender). An empty
// replyTo produces byte-identical output to Send.
func (s *SMTPSender) SendWithReplyTo(_ context.Context, to, replyTo, subject, htmlBody, textBody string) error {
	msg, err := buildMessage(s.headerFrom(), to, replyTo, subject, htmlBody, textBody)
	if err != nil {
		return err
	}

	c, encrypted, err := s.dial(s.cfg)
	if err != nil {
		return err
	}
	defer func() { _ = c.Close() }()

	if !encrypted {
		// Mandatory STARTTLS: refuse to transmit AUTH credentials or the token in
		// cleartext. If the server does not advertise STARTTLS (or a MITM stripped
		// the advertisement), fail closed instead of falling back to plaintext.
		if ok, _ := c.Extension("STARTTLS"); !ok {
			return errors.New("identity/email: server does not support STARTTLS; refusing to send over cleartext")
		}
		if err := c.StartTLS(tlsConfigFor(s.cfg.Host)); err != nil {
			return fmt.Errorf("identity/email: starttls: %w", err)
		}
	}

	if s.cfg.Username != "" {
		auth := smtp.PlainAuth("", s.cfg.Username, s.cfg.Password, s.cfg.Host)
		if err := c.Auth(auth); err != nil {
			return fmt.Errorf("identity/email: auth: %w", err)
		}
	}
	if err := c.Mail(s.cfg.From); err != nil {
		return fmt.Errorf("identity/email: mail: %w", err)
	}
	if err := c.Rcpt(to); err != nil {
		return fmt.Errorf("identity/email: rcpt: %w", err)
	}
	w, err := c.Data()
	if err != nil {
		return fmt.Errorf("identity/email: data: %w", err)
	}
	if _, err := w.Write(msg); err != nil {
		_ = w.Close()
		return fmt.Errorf("identity/email: write: %w", err)
	}
	if err := w.Close(); err != nil {
		return fmt.Errorf("identity/email: close data: %w", err)
	}
	return c.Quit()
}

const crlf = "\r\n"

// nowFn stamps the Date header; a package var so tests can freeze the clock.
var nowFn = time.Now

// headerFrom renders the From header value: the bare configured address, or
// an RFC 5322 display-name form when FromName is set (net/mail quotes and
// RFC 2047-encodes the name as needed). The SMTP envelope keeps the bare
// address either way -- see the c.Mail call in SendWithReplyTo.
func (s *SMTPSender) headerFrom() string {
	if s.cfg.FromName == "" {
		return s.cfg.From
	}
	return (&mail.Address{Name: s.cfg.FromName, Address: s.cfg.From}).String()
}

// fromDomain extracts the From address domain for the Message-ID right-hand
// side; from may be a bare address or a display-name form. Falls back to
// "localhost" rather than failing the send over a cosmetic header part.
func fromDomain(from string) string {
	a, err := mail.ParseAddress(from)
	if err != nil {
		return "localhost"
	}
	if i := strings.LastIndex(a.Address, "@"); i >= 0 && i+1 < len(a.Address) {
		return a.Address[i+1:]
	}
	return "localhost"
}

// boundaryBytes is the entropy of the MIME multipart boundary.
const boundaryBytes = 16

// buildMessage renders an RFC 5322 / MIME multipart/alternative message with a
// plain-text and an HTML part. An empty replyTo omits the Reply-To header
// entirely, so SendWithReplyTo("") stays byte-identical to Send.
//
// Deliverability hygiene: the Subject is RFC 2047 Q-encoded when it contains
// non-ASCII (pure-ASCII subjects -- including already-encoded encoded-words --
// pass through unchanged), and every message carries Date and Message-ID
// headers. Raw UTF-8 in a header violates RFC 5322's 7-bit assumption, and a
// missing Date/Message-ID is a standard spam-score line item; both push
// otherwise-transactional mail into the newsletter/spam bucket.
//
// Header values are rejected if they contain CR or LF: interpolating an
// attacker-controlled address/subject into a header line would otherwise allow
// header injection (RFC 5322 section 2.2). This is defense-in-depth behind
// validEmail. replyTo goes through the same guard.
func buildMessage(from, to, replyTo, subject, htmlBody, textBody string) ([]byte, error) {
	for _, h := range []string{from, to, replyTo, subject} {
		if strings.ContainsAny(h, "\r\n") {
			return nil, errors.New("identity/email: header value contains CR/LF")
		}
	}
	b := make([]byte, boundaryBytes)
	if _, err := rand.Read(b); err != nil {
		return nil, fmt.Errorf("identity/email: boundary: %w", err)
	}
	boundary := hex.EncodeToString(b)
	id := make([]byte, boundaryBytes)
	if _, err := rand.Read(id); err != nil {
		return nil, fmt.Errorf("identity/email: message-id: %w", err)
	}

	var sb strings.Builder
	sb.WriteString("From: " + from + crlf)
	sb.WriteString("To: " + to + crlf)
	if replyTo != "" {
		sb.WriteString("Reply-To: " + replyTo + crlf)
	}
	sb.WriteString("Subject: " + mime.QEncoding.Encode("utf-8", subject) + crlf)
	sb.WriteString("Date: " + nowFn().Format(time.RFC1123Z) + crlf)
	sb.WriteString("Message-ID: <" + hex.EncodeToString(id) + "@" + fromDomain(from) + ">" + crlf)
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
