package notify_test

import (
	"context"
	"errors"
	"html/template"
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
// in via a {{placeholder}} value is removed from the rendered Subject,
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

// Pins HTML-body escaping at the interpolation sink, found by the
// 2026-09-04 red-probe round (html_body_red_test.go); fixed by rendering
// MapTemplater's HTMLBody through html/template's escaper (the
// battery/email.Execute posture), with html/template.HTML as the
// explicit per-value trust marker.
//
// Property: a user-controlled value interpolated into an HTML email
// body is escaped before the body reaches the sender — no layer in the
// pipeline may hand raw attacker bytes to the HTML body sink.
// Surfaces: notify.go::MapTemplater.Render (interpolate on HTMLBody)
// feeding notify.go::EmailChannel.Send (Rendered.HTMLBody →
// email.Email.HTMLBody). The package's own doc example interpolates
// {{name}}, and Data values are routinely user-controlled.
func TestMapTemplaterEscapesHTMLBody(t *testing.T) {
	tmpl := notify.NewMapTemplater()
	tmpl.Set("welcome", "email", notify.Template{
		Subject:  "Welcome",
		TextBody: "Hi {{name}}",
		HTMLBody: "<p>Welcome, {{name}}!</p>",
	})

	r, err := tmpl.Render(context.Background(), "welcome", "email",
		map[string]any{"name": `<script>alert("pwned")</script>`})
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	if strings.Contains(r.HTMLBody, "<script>") {
		t.Errorf("SECURITY: [notify-html] MapTemplater interpolated a user-controlled value into "+
			"HTMLBody unescaped (rendered %q): EmailChannel hands this straight to the SMTP HTML part, "+
			"and battery/email.Execute autoescapes the same shape — the escaping obligation fell "+
			"between the two batteries.", r.HTMLBody)
	}
	// The escaped body must still CARRY the value (escaped, not dropped).
	if !strings.Contains(r.HTMLBody, "alert(") {
		t.Errorf("escaped HTMLBody lost the value entirely: %q", r.HTMLBody)
	}
	// TextBody is plain text: unchanged, raw bytes are fine there.
	if r.TextBody != "Hi <script>alert(\"pwned\")</script>" {
		t.Errorf("TextBody should stay payload bytes, got %q", r.TextBody)
	}

	// End to end through the channel: the email that reaches the SMTP
	// layer must not contain live markup from the value.
	sender := &captureSender{}
	ch := notify.NewEmailChannel(sender, "no-reply@example.com")
	if err := ch.Send(context.Background(),
		notify.Notification{To: notify.Recipient{Email: "alice@example.com"}}, r); err != nil {
		t.Fatalf("Send: %v", err)
	}
	if strings.Contains(sender.last.HTMLBody, "<script>") {
		t.Errorf("SECURITY: [notify-html] live <script> markup reached the SMTP HTML body: %q",
			sender.last.HTMLBody)
	}
}

// TestMapTemplaterTrustedHTMLMarker pins the escape hatch: a value the
// HOST generated (markup, not user input) is passed through verbatim
// when typed html/template.HTML — the documented opt-out.
func TestMapTemplaterTrustedHTMLMarker(t *testing.T) {
	tmpl := notify.NewMapTemplater()
	tmpl.Set("order", "email", notify.Template{
		HTMLBody: `<p>{{badge}}</p>`,
	})

	r, err := tmpl.Render(context.Background(), "order", "email",
		map[string]any{"badge": template.HTML(`<strong>shipped</strong>`)})
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	if !strings.Contains(r.HTMLBody, "<strong>shipped</strong>") {
		t.Errorf("template.HTML marker did not opt out of escaping: %q", r.HTMLBody)
	}

	// The same markup as a plain string IS escaped — only the explicit
	// type is trusted.
	r2, err := tmpl.Render(context.Background(), "order", "email",
		map[string]any{"badge": `<strong>shipped</strong>`})
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	if strings.Contains(r2.HTMLBody, "<strong>") {
		t.Errorf("plain-string markup was not escaped: %q", r2.HTMLBody)
	}
}
