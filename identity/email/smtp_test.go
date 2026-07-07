package email

import (
	"context"
	"crypto/tls"
	"errors"
	"io"
	"net"
	"net/smtp"
	"regexp"
	"strings"
	"testing"
	"time"
)

// mimeBoundaryRE matches the random per-message MIME boundary (boundaryBytes
// hex-encoded), so tests can normalize it out before comparing two otherwise-
// identical messages built in separate buildMessage calls.
var mimeBoundaryRE = regexp.MustCompile(`[0-9a-f]{32}`)

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
	for _, want := range []string{"From: noreply@piter.now\r\n", "Subject: Sign in to piter.now", "text/plain", "text/html", "Click https://piter.now/x"} {
		if !strings.Contains(msg, want) {
			t.Fatalf("message missing %q:\n%s", want, msg)
		}
	}
}

// TestSendWithReplyToDeliversHeader locks requirement 3 end-to-end through
// SMTPSender: SendWithReplyTo puts an exact "Reply-To: <addr>" line in the
// wire message alongside the existing From/To/Subject.
func TestSendWithReplyToDeliversHeader(t *testing.T) {
	c := &fakeSMTPClient{hasSTARTTLS: true}
	s := senderWith(c, false)
	s.cfg.From = "noreply@piter.now"
	err := s.SendWithReplyTo(context.Background(), "alice@example.com", "lead@example.com", "Sign in to piter.now",
		`<p>Click <a href="https://piter.now/x">here</a></p>`, "Click https://piter.now/x")
	if err != nil {
		t.Fatalf("SendWithReplyTo: %v", err)
	}
	if !strings.Contains(string(c.data), "Reply-To: lead@example.com\r\n") {
		t.Fatalf("message missing Reply-To header:\n%s", c.data)
	}
}

// TestSendWithReplyToRejectsHeaderInjection locks requirement 3's CRLF guard on
// the wire path (not just the internal buildMessage unit test): a malicious
// replyTo must abort delivery, never reach c.Data().
func TestSendWithReplyToRejectsHeaderInjection(t *testing.T) {
	c := &fakeSMTPClient{hasSTARTTLS: true}
	s := senderWith(c, false)
	err := s.SendWithReplyTo(context.Background(), "a@b.com", "lead@example.com\r\nBcc: x@evil.com", "s", "<p>h</p>", "h")
	if err == nil {
		t.Fatal("SendWithReplyTo accepted a CR/LF replyTo value (header injection)")
	}
	if called(c, "Data") {
		t.Fatal("SendWithReplyTo reached the SMTP conversation despite a rejected header")
	}
}

// TestSendWithReplyToEmptyMatchesSend locks requirement 3's "empty replyTo ==
// plain Send" equivalence: the two must produce byte-identical wire messages.
func TestSendWithReplyToEmptyMatchesSend(t *testing.T) {
	restore := nowFn
	nowFn = func() time.Time { return time.Unix(1751900000, 0).UTC() }
	defer func() { nowFn = restore }()

	cSend := &fakeSMTPClient{hasSTARTTLS: true}
	sSend := senderWith(cSend, false)
	if err := sSend.Send(context.Background(), "a@b.com", "s", "<p>h</p>", "h"); err != nil {
		t.Fatalf("Send: %v", err)
	}

	cReplyTo := &fakeSMTPClient{hasSTARTTLS: true}
	sReplyTo := senderWith(cReplyTo, false)
	if err := sReplyTo.SendWithReplyTo(context.Background(), "a@b.com", "", "s", "<p>h</p>", "h"); err != nil {
		t.Fatalf("SendWithReplyTo(empty): %v", err)
	}

	// Normalize the random 32-hex tokens (MIME boundary and Message-ID) before
	// comparing — with the clock frozen above they are the only fields expected
	// to differ between two independent buildMessage calls with otherwise-
	// identical arguments.
	normSend := mimeBoundaryRE.ReplaceAllString(string(cSend.data), "BOUNDARY")
	normReplyTo := mimeBoundaryRE.ReplaceAllString(string(cReplyTo.data), "BOUNDARY")
	if normSend != normReplyTo {
		t.Fatalf("SendWithReplyTo(empty) produced a different wire message than Send:\nSend: %s\nSendWithReplyTo: %s", cSend.data, cReplyTo.data)
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

// TestSendUsesFromNameInHeaderOnly locks the FromName split: the From HEADER
// carries the display-name form while the SMTP envelope (MAIL FROM) stays the
// bare address — RFC 5321 accepts only an addr-spec there.
func TestSendUsesFromNameInHeaderOnly(t *testing.T) {
	c := &fakeSMTPClient{hasSTARTTLS: true}
	s := senderWith(c, false)
	s.cfg.From = "noreply@piter.now"
	s.cfg.FromName = "piter.now"
	if err := s.Send(context.Background(), "a@b.com", "s", "<p>h</p>", "h"); err != nil {
		t.Fatalf("Send: %v", err)
	}
	if c.mailFrom != "noreply@piter.now" {
		t.Fatalf("MAIL FROM = %q, want the bare address", c.mailFrom)
	}
	if !strings.Contains(string(c.data), "From: \"piter.now\" <noreply@piter.now>\r\n") {
		t.Fatalf("From header missing display-name form:\n%s", c.data)
	}
}

// TestHeaderFromEncodesNonASCIIName locks RFC 2047 encoding of a Cyrillic
// display name: raw UTF-8 must never reach the From header line.
func TestHeaderFromEncodesNonASCIIName(t *testing.T) {
	s := &SMTPSender{cfg: SMTPConfig{From: "noreply@piter.now", FromName: "Питер"}}
	got := s.headerFrom()
	if !strings.HasPrefix(got, "=?utf-8?") || !strings.HasSuffix(got, "<noreply@piter.now>") {
		t.Fatalf("headerFrom() = %q, want RFC 2047-encoded name + bare addr", got)
	}
}

// TestBuildMessageEncodesNonASCIISubject locks that a Cyrillic subject leaves
// the header section as RFC 2047 encoded-words, never raw UTF-8.
func TestBuildMessageEncodesNonASCIISubject(t *testing.T) {
	msg, err := buildMessage("f@x.com", "t@y.com", "", "Заявка: Иван", "<p>h</p>", "h")
	if err != nil {
		t.Fatalf("buildMessage: %v", err)
	}
	headers, _, _ := strings.Cut(string(msg), "\r\n\r\n")
	if !strings.Contains(headers, "Subject: =?utf-8?q?") {
		t.Fatalf("subject not RFC 2047-encoded:\n%s", headers)
	}
	if strings.Contains(headers, "Заявка") {
		t.Fatalf("raw Cyrillic leaked into the header section:\n%s", headers)
	}
}

// TestBuildMessageSubjectPassthrough locks the two no-op cases: pure-ASCII
// subjects and ALREADY-encoded encoded-words (a caller that pre-encodes, as
// go-grad's leadstore did before this landed upstream) pass through unchanged
// — no double encoding.
func TestBuildMessageSubjectPassthrough(t *testing.T) {
	for _, subj := range []string{"Sign in to piter.now", "=?utf-8?q?=D0=97=D0=B0?="} {
		msg, err := buildMessage("f@x.com", "t@y.com", "", subj, "<p>h</p>", "h")
		if err != nil {
			t.Fatalf("buildMessage(%q): %v", subj, err)
		}
		if !strings.Contains(string(msg), "Subject: "+subj+"\r\n") {
			t.Errorf("subject %q did not pass through unchanged:\n%s", subj, msg)
		}
	}
}

// TestBuildMessageDateAndMessageID locks the two deliverability headers: a
// parseable RFC 1123Z Date and a Message-ID scoped to the From domain.
func TestBuildMessageDateAndMessageID(t *testing.T) {
	restore := nowFn
	nowFn = func() time.Time { return time.Unix(1751900000, 0).UTC() }
	defer func() { nowFn = restore }()

	msg, err := buildMessage("\"n\" <f@x.com>", "t@y.com", "", "s", "<p>h</p>", "h")
	if err != nil {
		t.Fatalf("buildMessage: %v", err)
	}
	headers, _, _ := strings.Cut(string(msg), "\r\n\r\n")
	dateVal := ""
	for _, l := range strings.Split(headers, "\r\n") {
		if v, ok := strings.CutPrefix(l, "Date: "); ok {
			dateVal = v
		}
	}
	if dateVal == "" {
		t.Fatalf("no Date header:\n%s", headers)
	}
	if _, err := time.Parse(time.RFC1123Z, dateVal); err != nil {
		t.Errorf("Date %q not RFC 1123Z: %v", dateVal, err)
	}
	if !regexp.MustCompile(`\r\nMessage-ID: <[0-9a-f]{32}@x\.com>\r\n`).Match(msg) {
		t.Errorf("Message-ID missing or malformed:\n%s", headers)
	}
}

// TestFromDomain locks the fallback ladder for the Message-ID domain.
func TestFromDomain(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"f@x.com", "x.com"},
		{"\"piter.now\" <noreply@send.piter.now>", "send.piter.now"},
		{"not-an-address", "localhost"},
	} {
		if got := fromDomain(tc.in); got != tc.want {
			t.Errorf("fromDomain(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}
