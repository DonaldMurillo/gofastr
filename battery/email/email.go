// Package email provides a pluggable email system with SMTP sending,
// development logging, and template rendering capabilities.
package email

import (
	"context"
	"errors"
)

// ErrSendFailed is returned when an email fails to send.
var ErrSendFailed = errors.New("email: send failed")

// Attachment represents an email attachment with its content.
type Attachment struct {
	Filename    string
	Content     []byte
	ContentType string
}

// Email represents an email message.
type Email struct {
	To          []string
	CC          []string
	BCC         []string
	From        string
	Subject     string
	TextBody    string
	HTMLBody    string
	Attachments []Attachment
	Headers     map[string]string
}

// Sender is the interface for sending emails.
type Sender interface {
	Send(ctx context.Context, email Email) error
}
