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

func TestEmailChannel_RejectsCRLFHeaderKey(t *testing.T) {
	t.Parallel()
	sender := &captureSender{}
	ch := notify.NewEmailChannel(sender, "from@example.com")
	err := ch.Send(context.Background(), notify.Notification{
		To: notify.Recipient{Email: "alice@example.com"},
	}, notify.Rendered{
		Subject:  "hi",
		TextBody: "body",
		Extra: map[string]any{
			"headers": map[string]string{
				"X-Trace\r\nBcc: attacker@example.com": "1",
			},
		},
	})
	if err == nil {
		t.Fatalf("SECURITY: [notify-email] custom header name with CR/LF was accepted. Attack: Bcc smuggling through rendered Extra.headers.")
	}
	if len(sender.last.To) != 0 || sender.last.Subject != "" || len(sender.last.Headers) != 0 {
		t.Fatalf("SECURITY: [notify-email] sender invoked despite unsafe custom header name: %#v", sender.last)
	}
}

func TestEmailChannel_RejectsCRLFHeaderValue(t *testing.T) {
	t.Parallel()
	sender := &captureSender{}
	ch := notify.NewEmailChannel(sender, "from@example.com")
	err := ch.Send(context.Background(), notify.Notification{
		To: notify.Recipient{Email: "alice@example.com"},
	}, notify.Rendered{
		Subject:  "hi",
		TextBody: "body",
		Extra: map[string]any{
			"headers": map[string]string{
				"X-Trace": "ok\r\nBcc: attacker@example.com",
			},
		},
	})
	if err == nil {
		t.Fatalf("SECURITY: [notify-email] custom header value with CR/LF was accepted. Attack: forged SMTP headers through rendered Extra.headers.")
	}
	if len(sender.last.To) != 0 || sender.last.Subject != "" || len(sender.last.Headers) != 0 {
		t.Fatalf("SECURITY: [notify-email] sender invoked despite unsafe custom header value: %#v", sender.last)
	}
}

func TestEmailChannel_RejectsReservedBccHeaderOverride(t *testing.T) {
	t.Parallel()
	sender := &captureSender{}
	ch := notify.NewEmailChannel(sender, "from@example.com")
	err := ch.Send(context.Background(), notify.Notification{
		To: notify.Recipient{Email: "alice@example.com"},
	}, notify.Rendered{
		Subject:  "hi",
		TextBody: "body",
		Extra: map[string]any{
			"headers": map[string]string{
				"Bcc": "attacker@example.com",
			},
		},
	})
	if err == nil {
		t.Fatalf("SECURITY: [notify-email] reserved Bcc header override was accepted. Attack: hidden recipient smuggling from notification data.")
	}
	if len(sender.last.To) != 0 || sender.last.Subject != "" || len(sender.last.Headers) != 0 {
		t.Fatalf("SECURITY: [notify-email] sender invoked despite reserved Bcc header override: %#v", sender.last)
	}
}

func TestEmailChannel_RejectsReservedContentTypeHeaderOverride(t *testing.T) {
	t.Parallel()
	sender := &captureSender{}
	ch := notify.NewEmailChannel(sender, "from@example.com")
	err := ch.Send(context.Background(), notify.Notification{
		To: notify.Recipient{Email: "alice@example.com"},
	}, notify.Rendered{
		Subject:  "hi",
		TextBody: "body",
		Extra: map[string]any{
			"headers": map[string]string{
				"Content-Type": "multipart/alternative; boundary=evil",
			},
		},
	})
	if err == nil {
		t.Fatalf("SECURITY: [notify-email] reserved Content-Type override was accepted. Attack: caller-controlled MIME structure through rendered Extra.headers.")
	}
	if len(sender.last.To) != 0 || sender.last.Subject != "" || len(sender.last.Headers) != 0 {
		t.Fatalf("SECURITY: [notify-email] sender invoked despite reserved Content-Type override: %#v", sender.last)
	}
}

func TestEmailChannel_RejectsRenderedFromOverride(t *testing.T) {
	t.Parallel()
	sender := &captureSender{}
	ch := notify.NewEmailChannel(sender, "from@example.com")
	err := ch.Send(context.Background(), notify.Notification{
		To: notify.Recipient{Email: "alice@example.com"},
	}, notify.Rendered{
		Subject:  "hi",
		TextBody: "body",
		Extra: map[string]any{
			"from": "ceo@company.example",
		},
	})
	if err == nil {
		t.Fatalf("SECURITY: [notify-email] rendered from override was accepted. Attack: sender spoofing via templated notification data.")
	}
	if len(sender.last.To) != 0 || sender.last.Subject != "" || sender.last.From != "" {
		t.Fatalf("SECURITY: [notify-email] sender invoked despite rendered from override: %#v", sender.last)
	}
}

func TestEmailChannel_RejectsReservedFromHeaderOverride(t *testing.T) {
	t.Parallel()
	sender := &captureSender{}
	ch := notify.NewEmailChannel(sender, "from@example.com")
	err := ch.Send(context.Background(), notify.Notification{
		To: notify.Recipient{Email: "alice@example.com"},
	}, notify.Rendered{
		Subject:  "hi",
		TextBody: "body",
		Extra: map[string]any{
			"headers": map[string]string{
				"From": "ceo@company.example",
			},
		},
	})
	if err == nil {
		t.Fatalf("SECURITY: [notify-email] reserved From header override was accepted. Attack: duplicate From header spoofing via rendered Extra.headers.")
	}
	if len(sender.last.To) != 0 || sender.last.Subject != "" || len(sender.last.Headers) != 0 {
		t.Fatalf("SECURITY: [notify-email] sender invoked despite reserved From header override: %#v", sender.last)
	}
}

func TestEmailChannel_RejectsReservedReplyToHeaderOverride(t *testing.T) {
	t.Parallel()
	sender := &captureSender{}
	ch := notify.NewEmailChannel(sender, "from@example.com")
	err := ch.Send(context.Background(), notify.Notification{
		To: notify.Recipient{Email: "alice@example.com"},
	}, notify.Rendered{
		Subject:  "hi",
		TextBody: "body",
		Extra: map[string]any{
			"headers": map[string]string{
				"Reply-To": "attacker@example.com",
			},
		},
	})
	if err == nil {
		t.Fatalf("SECURITY: [notify-email] reserved Reply-To header override was accepted. Attack: reply-hijack / phishing through rendered Extra.headers.")
	}
	if len(sender.last.To) != 0 || sender.last.Subject != "" || len(sender.last.Headers) != 0 {
		t.Fatalf("SECURITY: [notify-email] sender invoked despite reserved Reply-To header override: %#v", sender.last)
	}
}
