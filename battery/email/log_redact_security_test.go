package email

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

// TestRedactBodyEveryParam pins RedactBody against credential query
// parameters whose name does not start with the keyword, and relative
// links (2026-10-27 bug bash E8): the pattern once required the keyword
// right after ? or &, and an http(s) scheme in front.
func TestRedactBodyEveryParam(t *testing.T) {
	const secret = "S3CR3TVALUE"
	for _, body := range []string{
		"https://app.test/reset?token=" + secret,
		"https://app.test/cb?api_key=" + secret,
		"https://app.test/cb?client_secret=" + secret,
		"https://app.test/cb?session_token=" + secret,
		"https://app.test/cb?resetToken=" + secret,
		"https://app.test/cb?next=home&amp;accessToken=" + secret,
		"/auth/reset-password?token=" + secret,
		`<a href="/auth/reset-password?token=` + secret + `">reset</a>`,
	} {
		if got := RedactBody(body); strings.Contains(got, secret) {
			t.Errorf("RedactBody(%q) = %q, still holds the secret", body, got)
		}
	}
	for _, body := range []string{"https://app.test/docs?page=2", "/search?q=gofastr"} {
		if got := RedactBody(body); got != body {
			t.Errorf("RedactBody(%q) = %q, want it unchanged", body, got)
		}
	}
}

// TestLogSenderScrubsHeaders pins LogSender's custom header lines and
// envelope lines (E11): a token in a header value was printed while the
// body's copy was redacted, and ESC reached the dev terminal.
func TestLogSenderScrubsHeaders(t *testing.T) {
	const secret = "S3CR3TVALUE"
	var buf bytes.Buffer
	err := NewLogSender(&buf).Send(context.Background(), Email{
		From:     "a\x1b]0;x\x07@app.test",
		To:       []string{"b@app.test"},
		Subject:  "reset\x1b[2J",
		TextBody: "https://app.test/reset?token=" + secret,
		Headers: map[string]string{
			"X-Reset-Link": "https://app.test/reset?token=" + secret,
			"X-Note":       "a\x1b[2Jb",
			"X-\x1bKey":    "v",
		},
	})
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "X-Note") {
		t.Fatalf("log carries no X-Note header line:\n%s", out)
	}
	if strings.Contains(out, secret) {
		t.Errorf("log holds the secret from a header value:\n%s", out)
	}
	if strings.ContainsRune(out, '\x1b') || strings.ContainsRune(out, '\x07') {
		t.Errorf("log holds a raw control byte:\n%q", out)
	}
}
