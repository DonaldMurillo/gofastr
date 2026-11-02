package log

import (
	"io"
	"net"
	"os"
	"strings"
	"testing"
	"time"
)

// TestWebhookGiveUpWarningHidesURLCredential pins that the stderr line a
// webhook sink writes when it gives up on a batch does not carry the
// webhook URL's path or query. An incoming-webhook URL is its own
// credential (Slack's /services/T…/B…/<secret>, a collector's
// ?token=…), and net/http's *url.Error renders the whole URL into the
// message, so the "gave up" warning wrote the secret to stderr —
// journald, the container log, every collector behind it.
func TestWebhookGiveUpWarningHidesURLCredential(t *testing.T) {
	// A port nothing listens on: every attempt is a transport error.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	_ = ln.Close()

	const pathSecret = "XXpathSecretXX"
	const querySecret = "XXquerySecretXX"
	url := "http://" + addr + "/services/T000/B000/" + pathSecret + "?token=" + querySecret

	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	orig := os.Stderr
	os.Stderr = w
	t.Cleanup(func() { os.Stderr = orig })

	s := WebhookSink(url, WebhookOpts{
		BatchSize:     1,
		BatchInterval: 10 * time.Millisecond,
		MaxRetries:    1,
		Timeout:       time.Second,
	})
	if err := s.Write([]byte(`{"msg":"x"}`)); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for s.(*webhookSink).GaveUp() == 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	_ = s.Close()
	os.Stderr = orig
	_ = w.Close()
	out, _ := io.ReadAll(r)
	got := string(out)

	if s.(*webhookSink).GaveUp() == 0 {
		t.Fatalf("sink never gave up; stderr: %q", got)
	}
	if !strings.Contains(got, "gave up") {
		t.Fatalf("expected a give-up warning on stderr, got %q", got)
	}
	for _, secret := range []string{pathSecret, querySecret} {
		if strings.Contains(got, secret) {
			t.Errorf("give-up warning leaked the webhook URL credential %q: %q", secret, got)
		}
	}
	if !strings.Contains(got, addr) {
		t.Errorf("warning should still name the receiver host %q: %q", addr, got)
	}
}

// TestWebhookRequestBuildErrorHidesURL covers the other way the URL
// reaches an error: a URL that fails to parse comes back from
// http.NewRequest as a *url.Error quoting the raw string.
func TestWebhookRequestBuildErrorHidesURL(t *testing.T) {
	const secret = "XXsecretXX"
	s := &webhookSink{url: "http://example.invalid/" + secret + "\x7f", opts: WebhookOpts{Method: "POST", Timeout: time.Second}}
	err := s.postOnce([]byte(`{}`))
	if err == nil {
		t.Fatal("expected a request-build error")
	}
	if msg := err.Error(); strings.Contains(msg, secret) {
		t.Errorf("request-build error leaked the URL: %q", msg)
	}
}
