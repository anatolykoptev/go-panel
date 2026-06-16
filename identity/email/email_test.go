package email

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"net/smtp"
	"strings"
	"testing"
)

func TestLogSenderReturnsNilAndLogs(t *testing.T) {
	var buf bytes.Buffer
	log := slog.New(slog.NewTextHandler(&buf, nil))
	s := NewLogSender(log)

	err := s.Send(context.Background(), "a@b.com", "Sign in", "<p>link</p>", "link")
	if err != nil {
		t.Fatalf("LogSender.Send: %v", err)
	}
	if !strings.Contains(buf.String(), "Sign in") {
		t.Fatalf("LogSender did not log the subject; got %q", buf.String())
	}
}

func TestLogSenderImplementsEmailSender(t *testing.T) {
	var _ EmailSender = NewLogSender(slog.Default())
	var _ EmailSender = NewSMTPSender(SMTPConfig{})
}

func TestSMTPSenderSendsToRecipient(t *testing.T) {
	var (
		gotAddr string
		gotFrom string
		gotTo   []string
		gotMsg  []byte
	)
	s := NewSMTPSender(SMTPConfig{
		Host: "smtp.example.com", Port: 587,
		Username: "u", Password: "p", From: "noreply@piter.now",
	})
	s.send = func(addr string, _ smtp.Auth, from string, to []string, msg []byte) error {
		gotAddr, gotFrom, gotTo, gotMsg = addr, from, to, msg
		return nil
	}

	err := s.Send(context.Background(), "alice@example.com", "Sign in to piter.now",
		"<p>Click <a href=\"https://piter.now/x\">here</a></p>", "Click https://piter.now/x")
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	if gotAddr != "smtp.example.com:587" {
		t.Fatalf("addr = %q, want smtp.example.com:587", gotAddr)
	}
	if gotFrom != "noreply@piter.now" {
		t.Fatalf("from = %q", gotFrom)
	}
	if len(gotTo) != 1 || gotTo[0] != "alice@example.com" {
		t.Fatalf("to = %v", gotTo)
	}
	msg := string(gotMsg)
	for _, want := range []string{"Subject: Sign in to piter.now", "text/plain", "text/html", "Click https://piter.now/x"} {
		if !strings.Contains(msg, want) {
			t.Fatalf("message missing %q:\n%s", want, msg)
		}
	}
}

// TestBuildMessageRejectsHeaderInjection locks the defense-in-depth guard against
// SMTP header injection: a CR/LF in any header value must error, never produce a
// message with a smuggled extra header. Falsifiability: removing the CR/LF check
// in buildMessage makes this return a nil error and an injected message.
func TestBuildMessageRejectsHeaderInjection(t *testing.T) {
	cases := []struct{ name, from, to, subject string }{
		{"crlf in to", "f@x.com", "victim@x.com\r\nBcc: attacker@evil.com", "Sign in"},
		{"lf in subject", "f@x.com", "a@b.com", "Sign in\nBcc: attacker@evil.com"},
		{"cr in from", "f@x.com\rX-Spoof: 1", "a@b.com", "Sign in"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := buildMessage(tc.from, tc.to, tc.subject, "<p>x</p>", "x"); err == nil {
				t.Fatal("buildMessage accepted a CR/LF header value (header injection)")
			}
		})
	}
}

func TestSMTPSenderPropagatesError(t *testing.T) {
	s := NewSMTPSender(SMTPConfig{Host: "h", Port: 25, From: "f@x"})
	sentinel := errors.New("smtp down")
	s.send = func(string, smtp.Auth, string, []string, []byte) error { return sentinel }

	if err := s.Send(context.Background(), "a@b.com", "s", "<p>h</p>", "h"); !errors.Is(err, sentinel) {
		t.Fatalf("Send err = %v, want wrapped %v", err, sentinel)
	}
}
