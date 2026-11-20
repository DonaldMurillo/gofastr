package notify

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/DonaldMurillo/gofastr/battery/email"
)

// ErrUnsafeRecipient reports a recipient or from address email.ValidateAddress refuses.
var ErrUnsafeRecipient = errors.New("notify: unsafe recipient address")

// ErrInvalidAttachments reports an attachments entry that is not []email.Attachment.
var ErrInvalidAttachments = errors.New("notify: attachments must be []email.Attachment")

// EmailChannel adapts a battery/email Sender to the Channel interface.
type EmailChannel struct {
	sender  email.Sender
	from    string
	channel string
}

// EmailChannelOption configures the email adapter.
type EmailChannelOption func(*EmailChannel)

// WithEmailChannelName overrides the registered name (default "email").
func WithEmailChannelName(name string) EmailChannelOption {
	return func(c *EmailChannel) { c.channel = name }
}

// NewEmailChannel wraps sender, sending from the from address.
func NewEmailChannel(sender email.Sender, from string, opts ...EmailChannelOption) *EmailChannel {
	c := &EmailChannel{sender: sender, from: from, channel: "email"}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

// Name implements Channel.
func (c *EmailChannel) Name() string { return c.channel }

// Send implements Channel.
func (c *EmailChannel) Send(ctx context.Context, n Notification, r Rendered) (err error) {
	defer func() {
		if v := recover(); v != nil {
			err = fmt.Errorf("notify: email channel: sender panicked: %v", v)
		}
	}()
	if n.To.Email == "" {
		return nil
	}
	if _, ok := r.Extra["from"]; ok {
		return fmt.Errorf("%w: rendered 'from' override not allowed", ErrUnsafeHeader)
	}
	from := c.from
	if err := assertSafeAddress("to", n.To.Email); err != nil {
		return err
	}
	if from != "" {
		if err := assertSafeAddress("from", from); err != nil {
			return err
		}
	}
	var headers map[string]string
	if raw, ok := r.Extra["headers"]; ok {
		hdr, ok := raw.(map[string]string)
		if !ok {
			return fmt.Errorf("%w: headers must be map[string]string", ErrUnsafeHeader)
		}
		if err := validateCustomHeaders(hdr); err != nil {
			return err
		}
		headers = hdr
	}
	var attachments []email.Attachment
	if raw, present := r.Extra["attachments"]; present {
		var ok bool
		attachments, ok = raw.([]email.Attachment)
		if !ok {
			return ErrInvalidAttachments
		}
	}
	msg := email.Email{
		To:          []string{n.To.Email},
		From:        from,
		Subject:     r.Subject,
		TextBody:    r.TextBody,
		HTMLBody:    r.HTMLBody,
		Headers:     headers,
		Attachments: attachments,
	}
	return c.sender.Send(ctx, msg)
}

// ErrUnsafeHeader reports a rendered from override or an unsafe or reserved custom header.
var ErrUnsafeHeader = errors.New("notify: unsafe header")

var reservedHeaderNames = map[string]struct{}{
	"to":                        {},
	"cc":                        {},
	"bcc":                       {},
	"from":                      {},
	"sender":                    {},
	"reply-to":                  {},
	"subject":                   {},
	"content-type":              {},
	"content-transfer-encoding": {},
	"mime-version":              {},
}

func validateCustomHeaders(h map[string]string) error {
	for k, v := range h {
		if strings.ContainsAny(k, "\r\n\x00:") {
			return fmt.Errorf("%w: header name contains CR/LF/NUL/colon: %q", ErrUnsafeHeader, k)
		}
		if strings.ContainsAny(v, "\r\n\x00") {
			return fmt.Errorf("%w: header value contains CR/LF/NUL: %q=%q", ErrUnsafeHeader, k, v)
		}
		if _, reserved := reservedHeaderNames[strings.ToLower(strings.TrimSpace(k))]; reserved {
			return fmt.Errorf("%w: reserved header %q cannot be overridden via Extra.headers", ErrUnsafeHeader, k)
		}
	}
	return nil
}

func assertSafeAddress(field, addr string) error {
	if err := email.ValidateAddress(addr); err != nil {
		return fmt.Errorf("%w: %s: %v", ErrUnsafeRecipient, field, err)
	}
	return nil
}
