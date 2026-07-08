// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 Inveniam Capital Partners

package mcp

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/smtp"
	"strings"
)

// EmailSender abstracts the act of delivering a transactional email.
// Two implementations are provided: SMTPEmailSender wraps net/smtp for
// production (matches Phase 11 RD2's "provider-agnostic SMTP relay"
// resolution); LogOnlyEmailSender writes the subject and recipient to
// the logger and returns nil so the admin-approval flow stays usable
// when no SMTP credentials are configured (operators can copy the
// generated key out of structured logs).
type EmailSender interface {
	// Send dispatches a single email. Context cancellation does not
	// abort an in-flight SMTP write today; this is a future-proofing
	// signature, not a hard guarantee.
	Send(ctx context.Context, to, subject, body string) error
}

// ErrEmailNotConfigured is returned by SMTPEmailSender constructors
// when the supplied SMTPConfig is incomplete — surfaces the missing
// field through error wrapping so config validation can fail loud.
var ErrEmailNotConfigured = errors.New("smtp email sender: configuration incomplete")

// ErrEmailEmptyRecipient is returned by Send when called with an empty
// To address — defends against a bug upstream silently emailing nobody.
var ErrEmailEmptyRecipient = errors.New("smtp send: empty recipient")

// ErrEmailHeaderInjection is returned by Send when any header-bound
// field contains a CR/LF — defends against SMTP header injection.
var ErrEmailHeaderInjection = errors.New("smtp send: header injection in recipient or subject")

// SMTPConfig holds the wiring for a plain-SMTP-with-STARTTLS relay.
// Username + Password are optional (some local relays accept submissions
// from a network-restricted source without auth); when both are empty
// no AUTH is attempted.
type SMTPConfig struct {
	Host     string
	Port     int
	Username string
	Password string
	From     string
	// FromName is optional. When non-empty, the From header is
	// formatted as "Name <addr>".
	FromName string
}

// SMTPEmailSender delivers email via plain-SMTP-with-STARTTLS. Holds the
// dial parameters and PlainAuth credentials; constructor validates the
// minimum fields (Host, Port, From) and returns ErrEmailNotConfigured
// when anything is missing.
type SMTPEmailSender struct {
	cfg    SMTPConfig
	logger *slog.Logger
}

// NewSMTPEmailSender validates the config and returns a sender bound to
// it. Operators get a fail-loud error pointing at the missing field
// rather than a silent send-noop later.
func NewSMTPEmailSender(cfg *SMTPConfig, logger *slog.Logger) (*SMTPEmailSender, error) {
	if cfg == nil {
		return nil, fmt.Errorf("%w: nil SMTPConfig", ErrEmailNotConfigured)
	}
	switch {
	case cfg.Host == "":
		return nil, fmt.Errorf("%w: NVNM_SMTP_HOST is required", ErrEmailNotConfigured)
	case cfg.Port == 0:
		return nil, fmt.Errorf("%w: NVNM_SMTP_PORT is required", ErrEmailNotConfigured)
	case cfg.From == "":
		return nil, fmt.Errorf("%w: NVNM_SMTP_FROM is required", ErrEmailNotConfigured)
	}
	return &SMTPEmailSender{cfg: *cfg, logger: logger}, nil
}

// Send writes a single message to the configured SMTP server. The
// payload is built as a minimal RFC 5322 message (To, From, Subject,
// CRLF-CRLF separator, body). Recipient is honored verbatim — the
// public endpoint validates email syntax upstream, so callers passing
// admin-curated addresses are trusted.
func (s *SMTPEmailSender) Send(_ context.Context, to, subject, body string) error {
	if to == "" {
		return ErrEmailEmptyRecipient
	}
	// Defend against SMTP header injection. The public POST endpoint
	// validates email syntax (mail.ParseAddress rejects CR/LF in
	// addresses), so this is belt-and-suspenders -- if an upstream bug
	// passes a tainted recipient or admin-supplied subject through,
	// reject loud instead of letting the injected SMTP command run.
	if strings.ContainsAny(to, "\r\n") || strings.ContainsAny(subject, "\r\n") {
		return ErrEmailHeaderInjection
	}
	addr := fmt.Sprintf("%s:%d", s.cfg.Host, s.cfg.Port)

	var auth smtp.Auth
	if s.cfg.Username != "" && s.cfg.Password != "" {
		auth = smtp.PlainAuth("", s.cfg.Username, s.cfg.Password, s.cfg.Host)
	}

	fromHeader := s.cfg.From
	if s.cfg.FromName != "" {
		fromHeader = fmt.Sprintf("%s <%s>", s.cfg.FromName, s.cfg.From)
	}

	// CRLF line endings are required by RFC 5321; strings.Builder is
	// the cheapest way to assemble without escaping headaches.
	var msg strings.Builder
	msg.WriteString("From: " + fromHeader + "\r\n")
	msg.WriteString("To: " + to + "\r\n")
	msg.WriteString("Subject: " + subject + "\r\n")
	msg.WriteString("MIME-Version: 1.0\r\n")
	msg.WriteString("Content-Type: text/plain; charset=UTF-8\r\n")
	msg.WriteString("\r\n")
	msg.WriteString(body)

	// gosec G707: `to` and `subject` are validated above for CR/LF
	// (ErrEmailHeaderInjection); body comes from a server-side template
	// in approvalEmailBody / rejectionEmailBody that doesn't reflect
	// caller input. Suppress the taint-analysis flag with rationale.
	if err := smtp.SendMail( //nolint:gosec // G707: inputs validated against header injection above
		addr, auth, s.cfg.From, []string{to}, []byte(msg.String()),
	); err != nil {
		// Do NOT log the body — it may contain the freshly-minted API
		// key the admin just approved. Log enough to triage delivery
		// failures without leaking credential material.
		s.logger.Error("smtp send failed",
			slog.String("error", err.Error()),
			slog.String("smtp_host", s.cfg.Host),
			slog.Int("smtp_port", s.cfg.Port),
			slog.String("to", to),
			slog.String("subject", subject),
		)
		return fmt.Errorf("smtp send: %w", err)
	}
	s.logger.Info("smtp sent",
		slog.String("to", to),
		slog.String("subject", subject),
	)
	return nil
}

// LogOnlyEmailSender is the no-SMTP fallback: when an operator does
// not configure SMTP (open-source operators evaluating, dev/test
// deployments) the approve / reject flow still completes — the email
// body is written to the logger so the operator can copy the key to
// the customer by hand. The body is logged in full (including the key),
// so operators using this path accept that their structured-log store
// is the de-facto secret store for the key for as long as it sits there.
//
// F4: this path is NOT a silent default. config.Validate (via
// validateKeyRequestEmail) rejects KeyRequestEnabled+no-SMTP unless the
// operator explicitly sets NVNM_ALLOW_KEY_IN_LOGS=true, so this sender is
// only ever selected as a deliberate, acknowledged choice.
type LogOnlyEmailSender struct {
	logger *slog.Logger
}

// NewLogOnlyEmailSender returns a sender that logs every message at
// INFO. Useful for dev / closed-network deployments where SMTP isn't
// wired up.
func NewLogOnlyEmailSender(logger *slog.Logger) *LogOnlyEmailSender {
	return &LogOnlyEmailSender{logger: logger}
}

// Send writes the email payload to the logger at WARN. Returns nil
// unconditionally — the operator's structured-log pipeline is the
// "delivery." Logged at WARN (not INFO) because the body includes the
// minted API key: each emission is a credential landing in the log
// store, and should be visible as such in log review (F4).
func (l *LogOnlyEmailSender) Send(_ context.Context, to, subject, body string) error {
	l.logger.Warn("email (log-only, no SMTP configured) — body contains the minted API key",
		slog.String("to", to),
		slog.String("subject", subject),
		slog.String("body", body),
	)
	return nil
}
