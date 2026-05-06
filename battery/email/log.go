package email

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"
)

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
	if len(email.BCC) > 0 {
		sb.WriteString(fmt.Sprintf("BCC:     %s\n", strings.Join(email.BCC, ", ")))
	}
	sb.WriteString(fmt.Sprintf("Subject: %s\n", email.Subject))

	if email.TextBody != "" {
		sb.WriteString("--- Text Body ---\n")
		sb.WriteString(email.TextBody)
		sb.WriteString("\n")
	}

	if email.HTMLBody != "" {
		sb.WriteString("--- HTML Body ---\n")
		sb.WriteString(email.HTMLBody)
		sb.WriteString("\n")
	}

	for k, v := range email.Headers {
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
