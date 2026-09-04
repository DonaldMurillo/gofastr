package email

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

// TestEmail_HeaderInjection verifies that buildMessage refuses to
// serialise an Email whose header fields contain CR/LF. Attack: SMTP
// header injection, a `To` value of `"x@y\r\nBcc: victim@e.com"`
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

// TestExecuteEscapesHTMLBodyVars pins the template split that makes
// caller-controlled template DATA safe: Execute renders HTMLBody through
// html/template (contextual escaping) and subject/text through text/template
// (literal plain text, guarded downstream by buildMessage's CRLF refusal).
// Supersedes the vacuous TestEmail_TemplateInjection above (a bare t.Logf
// that can never fail); that one is flagged for central deletion.
func TestExecuteEscapesHTMLBodyVars(t *testing.T) {
	const attack = "<script>alert(1)</script>"

	got, err := Execute(Template{
		Subject:  "Hi {{.name}}",
		TextBody: "Hello {{.name}}",
		HTMLBody: "<p>{{.name}}</p>",
	}, map[string]any{"name": attack})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if strings.Contains(got.HTMLBody, attack) {
		t.Errorf("SECURITY: [email] HTMLBody rendered template data unescaped (script into the mail body): %q", got.HTMLBody)
	}
	if !strings.Contains(got.HTMLBody, "&lt;script&gt;") {
		t.Errorf("expected html/template escaping in HTMLBody; got %q", got.HTMLBody)
	}
	// Subject and text stay literal: they are plain-text parts, safe only
	// because buildMessage refuses control bytes later.
	if got.Subject != "Hi "+attack {
		t.Errorf("subject should render the value literally; got %q", got.Subject)
	}
	if got.TextBody != "Hello "+attack {
		t.Errorf("text body should render the value literally; got %q", got.TextBody)
	}

	// A CR/LF smuggled in through template data renders literally into the
	// subject and must still be refused by buildMessage: the composed
	// contract that makes the text/template split safe.
	inj, err := Execute(Template{Subject: "Hi {{.name}}"}, map[string]any{"name": "x\r\nBcc: victim@e.com"})
	if err != nil {
		t.Fatalf("Execute(inj): %v", err)
	}
	if !strings.Contains(inj.Subject, "\r\n") {
		t.Fatalf("expected the CR/LF to survive text/template rendering; got %q", inj.Subject)
	}
	inj.From = "a@b.test"
	inj.To = []string{"x@y.test"}
	if _, err := buildMessage(inj); err == nil {
		t.Error("SECURITY: [email] buildMessage accepted a subject whose CR/LF arrived via template data (Bcc smuggling)")
	}
}

// assertLineNoCtrl fails when a header line carries any C0 control byte or
// DEL. Callers pass lines with the trailing CR/LF already trimmed, so any
// control byte found is message content, not framing.
func assertLineNoCtrl(t *testing.T, line string) {
	t.Helper()
	for i := range len(line) {
		if b := line[i]; b < 0x20 || b == 0x7f {
			t.Errorf("SECURITY: [email-ctrl] header line %q carries control byte 0x%02X at offset %d — C0/DEL outside the CRLF framing is smuggled into the outgoing SMTP message", line, b, i)
			return
		}
	}
}

// Property: header lines of the outgoing message never carry a C0 control
// byte or DEL. MUAs, spam filters and archive tooling render header bytes
// verbatim, so an ESC…BEL terminal-title sequence in a Subject or a DEL in
// a custom header is an injection exactly like CRLF; the test passes when
// buildMessage refuses the message OR emits clean lines (the assert refuses,
// scrubHeaderValue percent-encodes as the wire-side guarantee).
func TestHeaderLinesCarryNoControlBytes(t *testing.T) {
	msg, err := buildMessage(Email{
		From:     "from\x0b@example.com",
		To:       []string{"to\x0c@example.com"},
		Subject:  "a\x1b]0;pwn\x07b",
		Headers:  map[string]string{"X-Custom": "v\x7fal"},
		TextBody: "body",
	})
	if err != nil {
		return // refused outright: property satisfied by rejection
	}

	// Scan the top header block: everything before the first blank line.
	for _, ln := range strings.Split(string(msg), "\n") {
		ln = strings.TrimSuffix(ln, "\r")
		if ln == "" {
			break // end of headers
		}
		assertLineNoCtrl(t, ln)
	}
}

// Property: attachment filename parameters (Content-Type name= /
// Content-Disposition filename=) never carry C0/DEL; quoteParamValue strips
// the full range as the defensive wire-side guarantee behind
// assertNoHeaderInjection's refusal.
func TestMIMEParamsCarryNoControlBytes(t *testing.T) {
	msg, err := buildMessage(Email{
		From:     "f@example.com",
		To:       []string{"t@example.com"},
		Subject:  "s",
		TextBody: "b",
		Attachments: []Attachment{{
			Filename: "re\x01port\x1b[2K\x7f.csv",
			Content:  []byte("data"),
		}},
	})
	if err != nil {
		return // refused outright: property satisfied by rejection
	}

	// Part headers live after the top block, so scan every line that is one.
	for _, ln := range strings.Split(string(msg), "\n") {
		ln = strings.TrimSuffix(ln, "\r")
		if strings.HasPrefix(ln, "Content-Type:") || strings.HasPrefix(ln, "Content-Disposition:") {
			assertLineNoCtrl(t, ln)
		}
	}
}
