package notify

import (
	"context"

	"github.com/DonaldMurillo/gofastr/battery/email"
)

// EmailChannel adapts a battery/email Sender to the notify Channel
// interface. Renders Subject/TextBody/HTMLBody from the Templater into
// the email's matching fields.
type EmailChannel struct {
	sender   email.Sender
	from     string
	channel  string
}

// EmailChannelOption configures the email adapter.
type EmailChannelOption func(*EmailChannel)

// WithEmailChannelName overrides the registered name (default "email").
// Useful when you want multiple email adapters (e.g. transactional vs
// marketing) on the same Notifier.
func WithEmailChannelName(name string) EmailChannelOption {
	return func(c *EmailChannel) { c.channel = name }
}

// NewEmailChannel wraps a battery/email Sender with a from-address
// default. Pass any [EmailChannelOption]s to tweak the registration.
func NewEmailChannel(sender email.Sender, from string, opts ...EmailChannelOption) *EmailChannel {
	c := &EmailChannel{sender: sender, from: from, channel: "email"}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

// Name implements Channel.
func (c *EmailChannel) Name() string { return c.channel }

// Send implements Channel. Maps the Rendered payload onto an
// email.Email and dispatches via the wrapped Sender.
func (c *EmailChannel) Send(ctx context.Context, n Notification, r Rendered) error {
	if n.To.Email == "" {
		return nil // router shouldn't have selected us, but guard anyway
	}
	from := c.from
	if alt, ok := r.Extra["from"].(string); ok && alt != "" {
		from = alt
	}
	msg := email.Email{
		To:       []string{n.To.Email},
		From:     from,
		Subject:  r.Subject,
		TextBody: r.TextBody,
		HTMLBody: r.HTMLBody,
	}
	if hdr, ok := r.Extra["headers"].(map[string]string); ok {
		msg.Headers = hdr
	}
	return c.sender.Send(ctx, msg)
}
