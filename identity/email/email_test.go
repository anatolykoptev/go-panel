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

// TestSendersImplementReplyToSender locks the extension-interface contract:
// both concrete senders must satisfy ReplyToSender (mirrors the existing
// MagicProvider type-assertion pattern in identity/auth.go), so a consumer's
// `sender.(ReplyToSender)` type assertion succeeds for either implementation.
func TestSendersImplementReplyToSender(t *testing.T) {
	var _ ReplyToSender = NewLogSender(slog.Default())
	var _ ReplyToSender = NewSMTPSender(SMTPConfig{})
}

// TestLogSenderSendWithReplyToLogsField locks that LogSender logs the replyTo
// value alongside the other fields (requirement 4: LogSender parity).
func TestLogSenderSendWithReplyToLogsField(t *testing.T) {
	var buf bytes.Buffer
	log := slog.New(slog.NewTextHandler(&buf, nil))
	s := NewLogSender(log)

	err := s.SendWithReplyTo(context.Background(), "a@b.com", "lead@example.com", "Sign in", "<p>link</p>", "link")
	if err != nil {
		t.Fatalf("LogSender.SendWithReplyTo: %v", err)
	}
	if !strings.Contains(buf.String(), "lead@example.com") {
		t.Fatalf("LogSender did not log the reply-to address; got %q", buf.String())
	}
}

// TestLogSenderSendWithReplyToEmptyMatchesSend locks requirement 3's "empty
// replyTo == plain Send" equivalence for LogSender too: an empty replyTo must
// produce the same observable log content (to/subject/text_body) as Send.
func TestLogSenderSendWithReplyToEmptyMatchesSend(t *testing.T) {
	var bufSend, bufReplyTo bytes.Buffer
	sSend := NewLogSender(slog.New(slog.NewTextHandler(&bufSend, nil)))
	sReplyTo := NewLogSender(slog.New(slog.NewTextHandler(&bufReplyTo, nil)))

	if err := sSend.Send(context.Background(), "a@b.com", "Sign in", "<p>link</p>", "link"); err != nil {
		t.Fatalf("Send: %v", err)
	}
	if err := sReplyTo.SendWithReplyTo(context.Background(), "a@b.com", "", "Sign in", "<p>link</p>", "link"); err != nil {
		t.Fatalf("SendWithReplyTo: %v", err)
	}
	for _, want := range []string{"a@b.com", "Sign in", "link"} {
		if !strings.Contains(bufReplyTo.String(), want) {
			t.Fatalf("SendWithReplyTo(empty) missing %q that plain Send would log", want)
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
			if _, err := buildMessage(tc.from, tc.to, "", tc.subject, "<p>x</p>", "x"); err == nil {
				t.Fatal("buildMessage accepted a CR/LF header value (header injection)")
			}
		})
	}
}

// TestBuildMessageEmitsReplyToHeader locks requirement 3: a non-empty replyTo
// must produce an exact "Reply-To: <addr>" header line.
func TestBuildMessageEmitsReplyToHeader(t *testing.T) {
	msg, err := buildMessage("f@x.com", "a@b.com", "lead@example.com", "Sign in", "<p>x</p>", "x")
	if err != nil {
		t.Fatalf("buildMessage: %v", err)
	}
	if !strings.Contains(string(msg), "Reply-To: lead@example.com\r\n") {
		t.Fatalf("message missing exact Reply-To header line:\n%s", msg)
	}
}

// TestBuildMessageOmitsReplyToWhenEmpty locks requirement 3's "empty replyTo ==
// plain Send" equivalence: no Reply-To header at all when replyTo is "".
func TestBuildMessageOmitsReplyToWhenEmpty(t *testing.T) {
	msg, err := buildMessage("f@x.com", "a@b.com", "", "Sign in", "<p>x</p>", "x")
	if err != nil {
		t.Fatalf("buildMessage: %v", err)
	}
	if strings.Contains(string(msg), "Reply-To:") {
		t.Fatalf("message has a Reply-To header despite empty replyTo:\n%s", msg)
	}
}

// TestBuildMessageRejectsReplyToHeaderInjection extends the CR/LF guard to the
// new replyTo parameter (requirement 3: same guard as To/Subject).
func TestBuildMessageRejectsReplyToHeaderInjection(t *testing.T) {
	_, err := buildMessage("f@x.com", "a@b.com", "lead@example.com\r\nBcc: attacker@evil.com", "Sign in", "<p>x</p>", "x")
	if err == nil {
		t.Fatal("buildMessage accepted a CR/LF replyTo value (header injection)")
	}
}
