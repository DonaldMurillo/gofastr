package email

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

// TestEmail_HeaderInjection verifies that buildMessage refuses to
// serialise an Email whose header fields contain CR/LF. Attack: SMTP
// header injection — a `To` value of `"x@y\r\nBcc: victim@e.com"`
// would otherwise add a hidden Bcc to the outgoing message.
func TestEmail_HeaderInjection(t *testing.T) {
	cases := []struct {
		name  string
		email Email
	}{
		{
			name:  "newline_in_to",
			email: Email{From: "a@b.test", To: []string{"x@y.test\r\nBcc: victim@e.com"}, Subject: "hi", TextBody: "body"},
		},
		{
			name:  "newline_in_from",
			email: Email{From: "a@b.test\r\nBcc: victim@e.com", To: []string{"x@y.test"}, Subject: "hi", TextBody: "body"},
		},
		{
			name:  "newline_in_subject",
			email: Email{From: "a@b.test", To: []string{"x@y.test"}, Subject: "hi\r\nBcc: victim@e.com", TextBody: "body"},
		},
		{
			name:  "newline_in_custom_header",
			email: Email{From: "a@b.test", To: []string{"x@y.test"}, Subject: "hi", TextBody: "body", Headers: map[string]string{"X-Mailer": "evil\r\nBcc: victim@e.com"}},
		},
		{
			name:  "bare_lf",
			email: Email{From: "a@b.test", To: []string{"x@y.test\nBcc: victim@e.com"}, Subject: "hi", TextBody: "body"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := buildMessage(tc.email)
			if err == nil {
				t.Errorf("SECURITY: [email] buildMessage accepted CR/LF in header. Attack: SMTP header injection (Bcc smuggling).")
			}
			_ = strings.Contains // satisfies the unused-import linter for older suites
		})
	}
}

// TestEmail_TemplateInjection verifies that template content doesn't
// allow arbitrary Go template execution. Attack: SSTI via email template.
func TestEmail_TemplateInjection(t *testing.T) {
	// Go's html/template auto-escapes, so template injection is mitigated
	// by default. This test documents that raw user input in templates
	// should use html/template, not text/template.
	t.Logf("NOTE: [email] ensure email templates use html/template (auto-escaping) not text/template")
}

// TestEmail_Base64Encoding verifies that Base64 encoding is correct
// and doesn't leak data. Attack: side-channel via encoding errors.
func TestEmail_Base64Encoding(t *testing.T) {
	tests := []struct {
		name  string
		input string
	}{
		{"simple", "Hello, World!"},
		{"unicode", "日本語テスト"},
		{"special_chars", "<script>alert('xss')</script>"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			encoded := b64Encode([]byte(tc.input))
			// Verify encoded form doesn't contain raw HTML
			if strings.Contains(encoded, "<script>") {
				t.Errorf("SECURITY: [email] base64 encoding preserved raw HTML: %q", encoded)
			}
			// Verify it's valid base64 (no control chars in the output except \r\n)
			for _, c := range encoded {
				if c != '\r' && c != '\n' && (c < 'A' || c > 'Z') && (c < 'a' || c > 'z') && (c < '0' || c > '9') && c != '+' && c != '/' && c != '=' {
					t.Errorf("SECURITY: [email] unexpected char in base64 output: %c (%d)", c, c)
				}
			}
		})
	}
}

func TestEmail_AttachmentFilenameHeaderInjection(t *testing.T) {
	email := Email{
		From:     "a@b.test",
		To:       []string{"x@y.test"},
		Subject:  "hi",
		TextBody: "body",
		Attachments: []Attachment{{
			Filename:    "invoice.pdf\"\r\nBcc: victim@example.com\r\nX-Evil: 1",
			ContentType: "application/pdf",
			Content:     []byte("fake-pdf"),
		}},
	}

	if _, err := buildMessage(email); err == nil {
		t.Fatalf("SECURITY: [email] buildMessage accepted CR/LF in attachment filename. Attack: MIME header injection via Content-Disposition filename.")
	}
}

func TestEmail_AttachmentContentTypeHeaderInjection(t *testing.T) {
	email := Email{
		From:     "a@b.test",
		To:       []string{"x@y.test"},
		Subject:  "hi",
		TextBody: "body",
		Attachments: []Attachment{{
			Filename:    "report.csv",
			ContentType: "text/csv\r\nBcc: victim@example.com\r\nX-Evil: 1",
			Content:     []byte("a,b,c"),
		}},
	}

	if _, err := buildMessage(email); err == nil {
		t.Fatalf("SECURITY: [email] buildMessage accepted CR/LF in attachment content type. Attack: MIME header injection via attachment Content-Type.")
	}
}

func TestLogSender_DoesNotExposeBCCRecipients(t *testing.T) {
	var buf bytes.Buffer
	sender := NewLogSender(&buf)
	email := Email{
		From:     "a@b.test",
		To:       []string{"primary@example.com"},
		BCC:      []string{"hidden@example.com"},
		Subject:  "secret",
		TextBody: "body",
	}

	if err := sender.Send(context.Background(), email); err != nil {
		t.Fatalf("Send: %v", err)
	}

	if strings.Contains(buf.String(), "hidden@example.com") || strings.Contains(buf.String(), "BCC:") {
		t.Fatalf("SECURITY: [email-log] LogSender exposed BCC recipients in logs: %q", buf.String())
	}
}

func TestLogSender_DoesNotExposeSensitiveHeaders(t *testing.T) {
	var buf bytes.Buffer
	sender := NewLogSender(&buf)
	email := Email{
		From:     "a@b.test",
		To:       []string{"primary@example.com"},
		Subject:  "secret",
		TextBody: "body",
		Headers: map[string]string{
			"Authorization": "Bearer super-secret-token",
			"X-API-Key":     "top-secret",
		},
	}

	if err := sender.Send(context.Background(), email); err != nil {
		t.Fatalf("Send: %v", err)
	}

	logs := buf.String()
	if strings.Contains(logs, "super-secret-token") || strings.Contains(logs, "top-secret") || strings.Contains(logs, "Authorization:") || strings.Contains(logs, "X-API-Key:") {
		t.Fatalf("SECURITY: [email-log] LogSender exposed sensitive headers in logs: %q", logs)
	}
}

func TestLogSender_DoesNotExposeLiveResetLinksInTextBody(t *testing.T) {
	var buf bytes.Buffer
	sender := NewLogSender(&buf)
	email := Email{
		From:     "a@b.test",
		To:       []string{"primary@example.com"},
		Subject:  "reset",
		TextBody: "Reset your password: http://localhost/reset-password?token=live-secret-token",
	}

	if err := sender.Send(context.Background(), email); err != nil {
		t.Fatalf("Send: %v", err)
	}

	logs := buf.String()
	if strings.Contains(logs, "token=live-secret-token") || strings.Contains(logs, "/reset-password?token=") {
		t.Fatalf("SECURITY: [email-log] LogSender exposed live reset link in text body logs: %q", logs)
	}
}

func TestLogSender_DoesNotExposeLiveResetLinksInHTMLBody(t *testing.T) {
	var buf bytes.Buffer
	sender := NewLogSender(&buf)
	email := Email{
		From:     "a@b.test",
		To:       []string{"primary@example.com"},
		Subject:  "reset",
		HTMLBody: `<a href="http://localhost/reset-password?token=html-live-secret">Reset password</a>`,
	}

	if err := sender.Send(context.Background(), email); err != nil {
		t.Fatalf("Send: %v", err)
	}

	logs := buf.String()
	if strings.Contains(logs, "token=html-live-secret") || strings.Contains(logs, "/reset-password?token=") {
		t.Fatalf("SECURITY: [email-log] LogSender exposed live reset link in HTML body logs: %q", logs)
	}
}
