// Package email defines the EmailSender seam for delivering magic-link messages,
// plus two implementations: SMTPSender (production, net/smtp) and LogSender
// (dev/test, logs the message instead of sending). go-grad selects one at wiring
// time based on whether SMTP credentials are configured.
package email

import "context"

// EmailSender delivers a transactional email. Implementations must not log the
// recipient or body in production (the body carries the magic-link token); see
// LogSender for the deliberate dev-only exception.
type EmailSender interface {
	Send(ctx context.Context, to, subject, htmlBody, textBody string) error
}
