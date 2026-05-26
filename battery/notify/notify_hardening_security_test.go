package notify_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/battery/email"
	"github.com/DonaldMurillo/gofastr/battery/notify"
)

// captureSender records the last email handed to it.
type captureSender struct {
	last email.Email
	err  error
}

func (c *captureSender) Send(_ context.Context, e email.Email) error {
	c.last = e
	return c.err
}

// TestEmailChannel_RejectsCRLFRecipient verifies CR/LF/NUL in the
// recipient address is rejected before reaching the SMTP layer.
func TestEmailChannel_RejectsCRLFRecipient(t *testing.T) {
	t.Parallel()
	for _, addr := range []string{
		"alice\r\nBcc: attacker@evil.com",
		"alice@example.com\nX-Injected: 1",
		"alice@example.com\x00",
	} {
		sender := &captureSender{}
		ch := notify.NewEmailChannel(sender, "from@example.com")
		err := ch.Send(context.Background(), notify.Notification{To: notify.Recipient{Email: addr}}, notify.Rendered{})
		if !errors.Is(err, notify.ErrUnsafeRecipient) {
			t.Errorf("SECURITY: [notify-email] address %q: err = %v; want ErrUnsafeRecipient", addr, err)
		}
		if sender.last.Subject != "" || len(sender.last.To) != 0 {
			t.Errorf("SECURITY: [notify-email] sender invoked despite unsafe address %q", addr)
		}
	}
}

// TestEmailChannel_RejectsHTMLInAddress catches obvious XSS-shaped
// addresses early. An RFC 5321 address never contains `<` or `>`.
func TestEmailChannel_RejectsHTMLInAddress(t *testing.T) {
	t.Parallel()
	for _, addr := range []string{
		"<script>alert('xss')</script>@evil.com",
		"bob<img src=x>@example.com",
	} {
		sender := &captureSender{}
		ch := notify.NewEmailChannel(sender, "from@example.com")
		err := ch.Send(context.Background(), notify.Notification{To: notify.Recipient{Email: addr}}, notify.Rendered{})
		if !errors.Is(err, notify.ErrUnsafeRecipient) {
			t.Errorf("SECURITY: [notify-email] address %q: err = %v; want ErrUnsafeRecipient", addr, err)
		}
	}
}

// TestMapTemplater_StripCRLFFromSubject verifies that a CR/LF/NUL slipped
// in via a {{placeholder}} value is removed from the rendered Subject —
// downstream transports treat Subject as a header.
func TestMapTemplater_StripCRLFFromSubject(t *testing.T) {
	t.Parallel()
	tpl := notify.NewMapTemplater()
	tpl.Set("welcome", "email", notify.Template{
		Subject:  "Hello {{name}}",
		TextBody: "body",
	})
	r, err := tpl.Render(context.Background(), "welcome", "email", map[string]any{
		"name": "alice\r\nBcc: attacker@evil.com",
	})
	if err != nil {
		t.Fatal(err)
	}
	if strings.ContainsAny(r.Subject, "\r\n\x00") {
		t.Errorf("SECURITY: [notify-template] Subject still contains CR/LF/NUL: %q", r.Subject)
	}
}

// TestMapTemplater_CapsInterpolatedOutput verifies the per-render output
// cap prevents a giant {{placeholder}} from producing an unbounded string.
func TestMapTemplater_CapsInterpolatedOutput(t *testing.T) {
	t.Parallel()
	tpl := notify.NewMapTemplater()
	tpl.Set("x", "email", notify.Template{TextBody: "{{big}}"})
	r, err := tpl.Render(context.Background(), "x", "email", map[string]any{
		"big": strings.Repeat("a", 10*1024*1024), // 10 MiB
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(r.TextBody) > notify.MaxInterpolatedOutputBytes {
		t.Errorf("SECURITY: [notify-template] interpolated TextBody = %d bytes, want <= %d", len(r.TextBody), notify.MaxInterpolatedOutputBytes)
	}
}
