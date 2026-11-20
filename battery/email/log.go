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

	"github.com/DonaldMurillo/gofastr/core/textsafe"
)

var sensitiveHeaderNames = map[string]struct{}{
	"authorization":       {},
	"x-api-key":           {},
	"cookie":              {},
	"set-cookie":          {},
	"proxy-authorization": {},
}

var urlWithSecretPattern = regexp.MustCompile(`(?i)[^\s"'<>]*(?:[?&]|&amp;|&#0*38;|&#x0*26;)[\w.-]*(?:token|code|key|secret|password)[\w.-]*=[^\s"'<>]*`)

var bearerPattern = regexp.MustCompile(`(?i)Bearer\s+\S+`)

// RedactBody scrubs anything resembling a live credential out of a
// rendered body before it is written to a development log: URLs whose
// query carries a token / code / key / secret / password parameter, and
// any Bearer token substring.
func RedactBody(s string) string {
	s = urlWithSecretPattern.ReplaceAllString(s, "[REDACTED-URL]")
	s = bearerPattern.ReplaceAllString(s, "Bearer [REDACTED]")
	return s
}

func logText(s string) string {
	return strings.Map(func(r rune) rune {
		if r == '\n' || r == '\t' {
			return r
		}
		if textsafe.IsUnsafe(r) {
			return -1
		}
		return r
	}, s)
}

// LogSender is a development email sender that logs email details to an
// io.Writer instead of actually sending them.
type LogSender struct {
	w io.Writer
}

// NewLogSender creates a new LogSender that writes to the provided
// writer.
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
	sb.WriteString(fmt.Sprintf("From:    %s\n", scrubHeaderValue(email.From)))
	sb.WriteString(fmt.Sprintf("To:      %s\n", scrubHeaderValue(strings.Join(email.To, ", "))))
	if len(email.CC) > 0 {
		sb.WriteString(fmt.Sprintf("CC:      %s\n", scrubHeaderValue(strings.Join(email.CC, ", "))))
	}
	sb.WriteString(fmt.Sprintf("Subject: %s\n", scrubHeaderValue(email.Subject)))

	if email.TextBody != "" {
		sb.WriteString("--- Text Body ---\n")
		sb.WriteString(logText(RedactBody(email.TextBody)))
		sb.WriteString("\n")
	}

	if email.HTMLBody != "" {
		sb.WriteString("--- HTML Body ---\n")
		sb.WriteString(logText(RedactBody(email.HTMLBody)))
		sb.WriteString("\n")
	}

	for _, k := range slices.Sorted(maps.Keys(email.Headers)) {
		if _, isSensitive := sensitiveHeaderNames[strings.ToLower(k)]; isSensitive {
			sb.WriteString(fmt.Sprintf("Header:  %s: [REDACTED]\n", "[redacted-header]"))
			continue
		}
		sb.WriteString(fmt.Sprintf("Header:  %s: %s\n", scrubHeaderValue(k), RedactBody(scrubHeaderValue(email.Headers[k]))))
	}

	if len(email.Attachments) > 0 {
		sb.WriteString(fmt.Sprintf("Attachments: %d\n", len(email.Attachments)))
		for _, att := range email.Attachments {
			ct := att.ContentType
			if ct == "" {
				ct = "application/octet-stream"
			}
			sb.WriteString(fmt.Sprintf("  - %s (%s, %d bytes)\n", scrubHeaderValue(att.Filename), scrubHeaderValue(ct), len(att.Content)))
		}
	}

	sb.WriteString("============================\n")

	_, err := fmt.Fprint(l.w, sb.String())
	if err != nil {
		return fmt.Errorf("%w: log write failed: %v", ErrSendFailed, err)
	}

	return nil
}
