package email

import (
	"context"
	"fmt"
	"io"
	"os"
	"regexp"
	"strings"
)

// sensitiveHeaderNames lists headers that LogSender refuses to print.
// Matched case-insensitively against header keys.
var sensitiveHeaderNames = map[string]struct{}{
	"authorization":       {},
	"x-api-key":           {},
	"cookie":              {},
	"set-cookie":          {},
	"proxy-authorization": {},
}

// urlWithSecretPattern matches any URL whose query string carries a
// sensitive parameter (token / code / key / secret / password). The
// whole URL is redacted because both the path (e.g. /reset-password)
// and the secret are sensitive together.
var urlWithSecretPattern = regexp.MustCompile(`(?i)https?://[^\s"'<>]*[?&](?:token|code|key|secret|password|access_token|reset[_-]?token)=[^\s"'<>]*`)

// bearerPattern catches `Bearer <token>` substrings anywhere in the body.
var bearerPattern = regexp.MustCompile(`(?i)Bearer\s+\S+`)

// redactBody scrubs anything resembling a live credential out of a
// rendered email body before it is written to a development log.
func redactBody(s string) string {
	s = urlWithSecretPattern.ReplaceAllString(s, "[REDACTED-URL]")
	s = bearerPattern.ReplaceAllString(s, "Bearer [REDACTED]")
	return s
}

// LogSender is a development email sender that logs email details
// to an io.Writer instead of actually sending them.
type LogSender struct {
	w io.Writer
}

// NewLogSender creates a new LogSender that writes to the provided writer.
// If no writer is provided, it defaults to os.Stdout.
func NewLogSender(w ...io.Writer) *LogSender {
	var writer io.Writer = os.Stdout
	if len(w) > 0 && w[0] != nil {
		writer = w[0]
	}
	return &LogSender{w: writer}
}

// Send implements the Sender interface by logging the email details.
func (l *LogSender) Send(_ context.Context, email Email) error {
	var sb strings.Builder

	sb.WriteString("========== EMAIL ==========\n")
	sb.WriteString(fmt.Sprintf("From:    %s\n", email.From))
	sb.WriteString(fmt.Sprintf("To:      %s\n", strings.Join(email.To, ", ")))
	if len(email.CC) > 0 {
		sb.WriteString(fmt.Sprintf("CC:      %s\n", strings.Join(email.CC, ", ")))
	}
	// BCC is intentionally omitted from dev logs — blind-carbon recipients
	// must not be observable to anyone reading the log (including the dev
	// who sent the message). We do not even emit the label, because the
	// presence of a "BCC:" line in a log is itself an observable signal.
	sb.WriteString(fmt.Sprintf("Subject: %s\n", email.Subject))

	if email.TextBody != "" {
		sb.WriteString("--- Text Body ---\n")
		sb.WriteString(redactBody(email.TextBody))
		sb.WriteString("\n")
	}

	if email.HTMLBody != "" {
		sb.WriteString("--- HTML Body ---\n")
		sb.WriteString(redactBody(email.HTMLBody))
		sb.WriteString("\n")
	}

	for k, v := range email.Headers {
		if _, isSensitive := sensitiveHeaderNames[strings.ToLower(k)]; isSensitive {
			sb.WriteString(fmt.Sprintf("Header:  %s: [REDACTED]\n", "[redacted-header]"))
			continue
		}
		sb.WriteString(fmt.Sprintf("Header:  %s: %s\n", k, v))
	}

	if len(email.Attachments) > 0 {
		sb.WriteString(fmt.Sprintf("Attachments: %d\n", len(email.Attachments)))
		for _, att := range email.Attachments {
			ct := att.ContentType
			if ct == "" {
				ct = "application/octet-stream"
			}
			sb.WriteString(fmt.Sprintf("  - %s (%s, %d bytes)\n", att.Filename, ct, len(att.Content)))
		}
	}

	sb.WriteString("============================\n")

	_, err := fmt.Fprint(l.w, sb.String())
	if err != nil {
		return fmt.Errorf("%w: log write failed: %v", ErrSendFailed, err)
	}

	return nil
}
