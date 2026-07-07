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

// ReplyToSender is an optional EmailSender extension for setting a Reply-To
// header distinct from the From address (e.g. so a reply to an operator
// notification reaches the lead, not the sending mailbox). Both SMTPSender and
// LogSender implement it. Consumers that need Reply-To should type-assert —
// `if r, ok := sender.(ReplyToSender); ok { ... }` — and fall back to plain
// Send when a sender does not implement it, mirroring the MagicProvider
// type-assertion pattern in identity/auth.go. An empty replyTo behaves
// identically to Send.
type ReplyToSender interface {
	SendWithReplyTo(ctx context.Context, to, replyTo, subject, htmlBody, textBody string) error
}
