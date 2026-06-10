package notify

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/DonaldMurillo/gofastr/battery/email"
)

// ErrUnsafeRecipient is returned by EmailChannel.Send when the
// recipient address contains characters that would let a caller smuggle
// extra headers (CR / LF / NUL) or that fails a sanity-check on shape
// (must contain "@" and no embedded HTML / control bytes).
var ErrUnsafeRecipient = errors.New("notify: unsafe recipient address")

// EmailChannel adapts a battery/email Sender to the notify Channel
// interface. Renders Subject/TextBody/HTMLBody from the Templater into
// the email's matching fields.
type EmailChannel struct {
	sender  email.Sender
	from    string
	channel string
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
//
// Recipient and from addresses are checked for the three shapes that
// turn an address into a header-injection vector: CR, LF, NUL. They are
// also screened for an "@" and rejected if they contain HTML tag
// characters — a literal "<script>alert(1)</script>@evil.com" is never
// a legitimate address. The downstream SMTP sender re-checks at the
// transport boundary, but rejecting early surfaces the bug nearer the
// data source.
func (c *EmailChannel) Send(ctx context.Context, n Notification, r Rendered) error {
	if n.To.Email == "" {
		return nil // router shouldn't have selected us, but guard anyway
	}
	// Rendered.Extra["from"] is rejected: a templated from override is a
	// sender-spoofing vector — the channel's configured from-address is
	// the only legitimate source.
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
	msg := email.Email{
		To:       []string{n.To.Email},
		From:     from,
		Subject:  r.Subject,
		TextBody: r.TextBody,
		HTMLBody: r.HTMLBody,
		Headers:  headers,
	}
	return c.sender.Send(ctx, msg)
}

// ErrUnsafeHeader is returned when rendered Extra contains a header
// name/value that would let a caller smuggle SMTP headers (CR/LF), or
// when it tries to override a reserved header that the channel owns.
var ErrUnsafeHeader = errors.New("notify: unsafe header")

// reservedHeaderNames are header keys that callers must NOT override
// through Rendered.Extra["headers"]. The channel computes them itself
// (To/From/Subject/Content-Type) or routing-critical fields a caller
// could weaponize for recipient smuggling (Bcc, Cc) or reply hijacking
// (Reply-To). Compared case-insensitively.
var reservedHeaderNames = map[string]struct{}{
	"to":           {},
	"cc":           {},
	"bcc":          {},
	"from":         {},
	"sender":       {},
	"reply-to":     {},
	"subject":      {},
	"content-type": {},
	"mime-version": {},
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

// assertSafeAddress applies a fast shape check that catches the
// failure modes the downstream SMTP layer also enforces (CR / LF / NUL)
// plus a few obviously-not-an-email shapes (HTML, missing @). It is
// intentionally permissive otherwise — RFC 5321 addresses are richer
// than any short regex can capture.
func assertSafeAddress(field, addr string) error {
	if strings.ContainsAny(addr, "\r\n\x00") {
		return fmt.Errorf("%w: %s contains CR/LF/NUL", ErrUnsafeRecipient, field)
	}
	if strings.ContainsAny(addr, "<>\"'") {
		return fmt.Errorf("%w: %s contains HTML tag characters: %q", ErrUnsafeRecipient, field, addr)
	}
	if !strings.Contains(addr, "@") {
		return fmt.Errorf("%w: %s missing '@': %q", ErrUnsafeRecipient, field, addr)
	}
	return nil
}
