package email

import (
	"context"
	"crypto/tls"
	"errors"
	"io"
	"net"
	"net/smtp"
	"strings"
	"testing"
)

// fakeSMTPClient records the SMTP conversation so tests can assert ordering and
// TLS enforcement without a real server.
type fakeSMTPClient struct {
	hasSTARTTLS bool
	startTLSErr error
	mailErr     error
	calls       []string
	mailFrom    string
	rcptTo      []string
	data        []byte
}

func (f *fakeSMTPClient) Extension(name string) (bool, string) {
	f.calls = append(f.calls, "Extension:"+name)
	if name == "STARTTLS" {
		return f.hasSTARTTLS, ""
	}
	return false, ""
}

func (f *fakeSMTPClient) StartTLS(*tls.Config) error {
	f.calls = append(f.calls, "StartTLS")
	return f.startTLSErr
}

func (f *fakeSMTPClient) Auth(smtp.Auth) error {
	f.calls = append(f.calls, "Auth")
	return nil
}

func (f *fakeSMTPClient) Mail(from string) error {
	f.calls = append(f.calls, "Mail")
	f.mailFrom = from
	return f.mailErr
}

func (f *fakeSMTPClient) Rcpt(to string) error {
	f.calls = append(f.calls, "Rcpt")
	f.rcptTo = append(f.rcptTo, to)
	return nil
}

func (f *fakeSMTPClient) Data() (io.WriteCloser, error) {
	f.calls = append(f.calls, "Data")
	return &capturingWriteCloser{f: f}, nil
}

func (f *fakeSMTPClient) Quit() error  { f.calls = append(f.calls, "Quit"); return nil }
func (f *fakeSMTPClient) Close() error { return nil }

type capturingWriteCloser struct{ f *fakeSMTPClient }

func (w *capturingWriteCloser) Write(p []byte) (int, error) {
	w.f.data = append(w.f.data, p...)
	return len(p), nil
}
func (w *capturingWriteCloser) Close() error { return nil }

func senderWith(c *fakeSMTPClient, encrypted bool) *SMTPSender {
	return &SMTPSender{
		cfg: SMTPConfig{Host: "smtp.example.com", Port: 587, Username: "u", Password: "p", From: "from@example.com"},
		dial: func(SMTPConfig) (smtpClient, bool, error) {
			return c, encrypted, nil
		},
	}
}

func called(c *fakeSMTPClient, want string) bool {
	for _, got := range c.calls {
		if got == want {
			return true
		}
	}
	return false
}

func callIndex(c *fakeSMTPClient, want string) int {
	for i, got := range c.calls {
		if got == want {
			return i
		}
	}
	return -1
}

// TestSendRefusesCleartextWhenNoSTARTTLS locks SEC-CR-002: when the (non-implicit-
// TLS) server does not advertise STARTTLS, Send fails closed and NEVER runs AUTH,
// MAIL, RCPT or DATA - so the SMTP credentials and the magic-link token cannot
// leak over a cleartext hop. Falsifiability: reverting to opportunistic
// net/smtp.SendMail (or any silent plaintext fallback) makes those calls appear
// and this test fails.
func TestSendRefusesCleartextWhenNoSTARTTLS(t *testing.T) {
	c := &fakeSMTPClient{hasSTARTTLS: false}
	s := senderWith(c, false)
	err := s.Send(context.Background(), "to@example.com", "subj", "<b>hi</b>", "hi")
	if err == nil {
		t.Fatal("Send succeeded with no STARTTLS support; want a hard failure")
	}
	for _, leaky := range []string{"StartTLS", "Auth", "Mail", "Rcpt", "Data"} {
		if called(c, leaky) {
			t.Fatalf("after cleartext refusal, %q was called (potential creds/token leak): %v", leaky, c.calls)
		}
	}
}

// TestSendStartTLSBeforeAuth locks that on a plaintext connection STARTTLS runs
// BEFORE AUTH, so credentials are only ever sent inside the TLS tunnel.
func TestSendStartTLSBeforeAuth(t *testing.T) {
	c := &fakeSMTPClient{hasSTARTTLS: true}
	s := senderWith(c, false)
	if err := s.Send(context.Background(), "to@example.com", "subj", "<b>hi</b>", "hi"); err != nil {
		t.Fatalf("Send: %v", err)
	}
	st, au := callIndex(c, "StartTLS"), callIndex(c, "Auth")
	if st < 0 || au < 0 || st > au {
		t.Fatalf("StartTLS must run before Auth; calls = %v", c.calls)
	}
}

// TestSendStartTLSErrorFailsClosed locks that a STARTTLS handshake failure aborts
// the send before AUTH - no downgrade to cleartext.
func TestSendStartTLSErrorFailsClosed(t *testing.T) {
	c := &fakeSMTPClient{hasSTARTTLS: true, startTLSErr: errors.New("handshake boom")}
	s := senderWith(c, false)
	if err := s.Send(context.Background(), "to@example.com", "subj", "<b>hi</b>", "hi"); err == nil {
		t.Fatal("Send succeeded despite a StartTLS failure; want error")
	}
	if called(c, "Auth") {
		t.Fatalf("Auth ran after a StartTLS failure: %v", c.calls)
	}
}

// TestSendImplicitTLSSkipsStartTLS locks that on an already-encrypted (implicit
// TLS / port 465) transport, Send does NOT attempt STARTTLS but still AUTHs and
// delivers.
func TestSendImplicitTLSSkipsStartTLS(t *testing.T) {
	c := &fakeSMTPClient{}
	s := senderWith(c, true)
	if err := s.Send(context.Background(), "to@example.com", "subj", "<b>hi</b>", "hi"); err != nil {
		t.Fatalf("Send: %v", err)
	}
	if called(c, "StartTLS") {
		t.Fatalf("StartTLS called on an already-encrypted transport: %v", c.calls)
	}
	for _, want := range []string{"Auth", "Mail", "Rcpt", "Data", "Quit"} {
		if !called(c, want) {
			t.Fatalf("expected %q in the conversation: %v", want, c.calls)
		}
	}
}

// TestSendDeliversMessageContent ports the pre-refactor delivery assertions onto
// the TLS-enforcing path: the MAIL FROM, RCPT TO and the rendered message body
// reach the server intact.
func TestSendDeliversMessageContent(t *testing.T) {
	c := &fakeSMTPClient{hasSTARTTLS: true}
	s := senderWith(c, false)
	s.cfg.From = "noreply@piter.now"
	err := s.Send(context.Background(), "alice@example.com", "Sign in to piter.now",
		`<p>Click <a href="https://piter.now/x">here</a></p>`, "Click https://piter.now/x")
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	if c.mailFrom != "noreply@piter.now" {
		t.Fatalf("MAIL FROM = %q, want noreply@piter.now", c.mailFrom)
	}
	if len(c.rcptTo) != 1 || c.rcptTo[0] != "alice@example.com" {
		t.Fatalf("RCPT TO = %v, want [alice@example.com]", c.rcptTo)
	}
	msg := string(c.data)
	for _, want := range []string{"Subject: Sign in to piter.now", "text/plain", "text/html", "Click https://piter.now/x"} {
		if !strings.Contains(msg, want) {
			t.Fatalf("message missing %q:\n%s", want, msg)
		}
	}
}

// TestSendPropagatesMailError locks that a server-side error during the
// conversation is wrapped and returned.
func TestSendPropagatesMailError(t *testing.T) {
	sentinel := errors.New("smtp down")
	c := &fakeSMTPClient{hasSTARTTLS: true, mailErr: sentinel}
	s := senderWith(c, false)
	if err := s.Send(context.Background(), "a@b.com", "s", "<p>h</p>", "h"); !errors.Is(err, sentinel) {
		t.Fatalf("Send err = %v, want wrapped %v", err, sentinel)
	}
}

// TestSendPropagatesDialError locks that a dial/connection failure is returned
// (and never silently swallowed into a successful send).
func TestSendPropagatesDialError(t *testing.T) {
	sentinel := errors.New("dial refused")
	s := &SMTPSender{
		cfg:  SMTPConfig{Host: "h", Port: 587, From: "f@x"},
		dial: func(SMTPConfig) (smtpClient, bool, error) { return nil, false, sentinel },
	}
	if err := s.Send(context.Background(), "a@b.com", "s", "<p>h</p>", "h"); !errors.Is(err, sentinel) {
		t.Fatalf("Send err = %v, want wrapped %v", err, sentinel)
	}
}

// TestDialSMTPPlaintextDialError covers the non-implicit-TLS dial branch:
// a refused connection is wrapped and returned (no panic, no silent success).
func TestDialSMTPPlaintextDialError(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	_ = ln.Close()
	if _, _, derr := dialSMTP(SMTPConfig{Host: "127.0.0.1", Port: port}); derr == nil {
		t.Fatal("dialSMTP succeeded against a closed port; want a connection error")
	}
}

// TestDialSMTPImplicitTLSDialError covers the implicit-TLS (port 465) dial
// branch and tlsConfigFor: with no TLS server listening the tls.Dial is
// refused and the error is wrapped.
func TestDialSMTPImplicitTLSDialError(t *testing.T) {
	if _, _, err := dialSMTP(SMTPConfig{Host: "127.0.0.1", Port: implicitTLSPort}); err == nil {
		t.Fatal("dialSMTP(465) succeeded with no TLS server; want a connection error")
	}
}
