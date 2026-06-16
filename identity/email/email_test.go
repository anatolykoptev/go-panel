package email

import (
	"bytes"
	"context"
	"log/slog"
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
