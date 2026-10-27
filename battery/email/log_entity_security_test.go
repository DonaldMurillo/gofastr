package email

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

// html/template writes '&' inside an href as "&amp;", so a link like
// ?next=home&token=S rendered as "?next=home&amp;token=S". The redaction
// pattern accepted only '?' or '&' before the parameter name, missed the
// entity, and LogSender wrote the live token to the log.
func TestLogSenderRedactsEscapedAmpersandURL(t *testing.T) {
	const secret = "S3CRET-reset-token"
	for _, url := range []string{
		"https://app.example/reset?next=home&token=" + secret,
		"https://app.example/reset?next=home&lang=en&reset_token=" + secret,
	} {
		msg, err := Execute(Template{
			Subject:  "Reset",
			HTMLBody: `<p><a href="{{.URL}}">Reset your password</a></p>`,
		}, map[string]any{"URL": url})
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(msg.HTMLBody, "&amp;") {
			t.Fatalf("the template did not entity-escape the separator: %s", msg.HTMLBody)
		}
		var buf bytes.Buffer
		if err := NewLogSender(&buf).Send(context.Background(), msg); err != nil {
			t.Fatal(err)
		}
		if strings.Contains(buf.String(), secret) {
			t.Fatalf("the log holds the live token:\n%s", buf.String())
		}
	}
}

// The numeric spellings of '&' are separators too.
func TestRedactBodyNumericAmpersand(t *testing.T) {
	for _, sep := range []string{"&#38;", "&#x26;", "&#X26;", "&#038;"} {
		body := `<a href="https://app.example/reset?next=home` + sep + `token=S3CRET">x</a>`
		if got := RedactBody(body); strings.Contains(got, "S3CRET") {
			t.Errorf("separator %s: %s", sep, got)
		}
	}
	if got := RedactBody("https://app.example/docs?next=home&amp;page=2"); strings.Contains(got, "REDACTED") {
		t.Errorf("a URL with no secret parameter was redacted: %s", got)
	}
}
