package email

import (
	"context"
	"log/slog"
)

// LogSender is a dev/test EmailSender that logs the message instead of sending
// it. It is the default when no SMTP host is configured.
//
// SECURITY: LogSender logs the full text body, which contains the magic-link
// token, and the recipient address. This is intentional for local development
// (you click the link from the log). NEVER wire LogSender in production — doing
// so writes single-use auth tokens and user emails to the log stream.
type LogSender struct {
	log *slog.Logger
}

// NewLogSender returns a LogSender. A nil logger falls back to slog.Default().
func NewLogSender(log *slog.Logger) *LogSender {
	if log == nil {
		log = slog.Default()
	}
	return &LogSender{log: log}
}

// Send logs the would-be email and returns nil. See the type doc for the
// production-use warning.
func (s *LogSender) Send(ctx context.Context, to, subject, htmlBody, textBody string) error {
	return s.SendWithReplyTo(ctx, to, "", subject, htmlBody, textBody)
}

// SendWithReplyTo is Send plus logging the replyTo field alongside (ReplyToSender).
// An empty replyTo logs an empty reply_to, matching Send's observable output.
func (s *LogSender) SendWithReplyTo(ctx context.Context, to, replyTo, subject, _, textBody string) error {
	s.log.InfoContext(ctx, "dev email (not sent — LogSender)",
		slog.String("to", to),
		slog.String("reply_to", replyTo),
		slog.String("subject", subject),
		slog.String("text_body", textBody),
	)
	return nil
}
