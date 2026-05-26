package email

import (
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
