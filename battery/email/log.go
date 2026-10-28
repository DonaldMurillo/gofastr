package email

import (
	"context"
	"fmt"
	"io"
	"maps"
	"os"
	"regexp"
	"slices"
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

// urlWithSecretPattern matches any link, absolute or relative, whose
// query string carries a parameter naming a credential: token, code,
// key, secret or password anywhere in the name, so api_key,
// client_secret, session_token and resetToken count as well as token.
// The whole link is redacted because both the path (e.g.
// /reset-password) and the secret are sensitive together.
//
// The separator before the parameter may be an HTML entity: html/template
// writes '&' inside an href as "&amp;", so ?next=home&token=S reaches the
// log as ?next=home&amp;token=S. The named and numeric spellings of '&'
// (&amp; &#38; &#x26;) all count, or an HTML body logged the live token.
var urlWithSecretPattern = regexp.MustCompile(`(?i)[^\s"'<>]*(?:[?&]|&amp;|&#0*38;|&#x0*26;)[\w.-]*(?:token|code|key|secret|password)[\w.-]*=[^\s"'<>]*`)

// bearerPattern catches `Bearer <token>` substrings anywhere in the body.
var bearerPattern = regexp.MustCompile(`(?i)Bearer\s+\S+`)

// RedactBody scrubs anything resembling a live credential out of a rendered
// body before it is written to a development log: URLs whose query carries
// a token / code / key / secret / password parameter, and any Bearer token
// substring. Exported so sibling dev-log surfaces (notify.LoggerChannel)
// redact the same shapes instead of forking the patterns; pinned by
// TestLogSender_DoesNotExposeLiveResetLinksInTextBody and
// TestLoggerChannelRedactsCredentialURLs.
func RedactBody(s string) string {
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
	// Every caller-derived line passes scrubHeaderValue, the SMTP wire
	// scrub, so an ESC or C1 byte never reaches the dev terminal.
	sb.WriteString(fmt.Sprintf("From:    %s\n", scrubHeaderValue(email.From)))
	sb.WriteString(fmt.Sprintf("To:      %s\n", scrubHeaderValue(strings.Join(email.To, ", "))))
	if len(email.CC) > 0 {
		sb.WriteString(fmt.Sprintf("CC:      %s\n", scrubHeaderValue(strings.Join(email.CC, ", "))))
	}
	// BCC is intentionally omitted from dev logs, blind-carbon recipients
	// must not be observable to anyone reading the log (including the dev
	// who sent the message). We do not even emit the label, because the
	// presence of a "BCC:" line in a log is itself an observable signal.
	sb.WriteString(fmt.Sprintf("Subject: %s\n", scrubHeaderValue(email.Subject)))

	if email.TextBody != "" {
		sb.WriteString("--- Text Body ---\n")
		sb.WriteString(RedactBody(email.TextBody))
		sb.WriteString("\n")
	}

	if email.HTMLBody != "" {
		sb.WriteString("--- HTML Body ---\n")
		sb.WriteString(RedactBody(email.HTMLBody))
		sb.WriteString("\n")
	}

	for _, k := range slices.Sorted(maps.Keys(email.Headers)) {
		if _, isSensitive := sensitiveHeaderNames[strings.ToLower(k)]; isSensitive {
			sb.WriteString(fmt.Sprintf("Header:  %s: [REDACTED]\n", "[redacted-header]"))
			continue
		}
		// A header value gets the body's redaction too: a reset link in
		// X-Reset-Link is the same credential as the one in the body.
		sb.WriteString(fmt.Sprintf("Header:  %s: %s\n", scrubHeaderValue(k), RedactBody(scrubHeaderValue(email.Headers[k]))))
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
