package service

import (
	"net/http/httptest"
	"testing"
)

// A localhost tab is sent to the address `open` signs the browser in
// at, which for an IPv6 bind is [::1], not 127.0.0.1.
func TestCanonicalRedirectFollowsBrowserAddr(t *testing.T) {
	for bind, want := range map[string]string{
		"127.0.0.1:47811": "http://127.0.0.1:47811/todos?a=1",
		"0.0.0.0:47811":   "http://127.0.0.1:47811/todos?a=1",
		"[::1]:47811":     "http://[::1]:47811/todos?a=1",
		"[::]:47811":      "http://[::1]:47811/todos?a=1",
	} {
		g := newTestGate(t, false)
		g.pinAt(browserAddr(bind), gatePort, nil)
		r := httptest.NewRequest("GET", "http://localhost:47811/todos?a=1", nil)
		if got := g.canonicalURL(r); got != want {
			t.Errorf("bind %s: localhost tab sent to %q; want %q", bind, got, want)
		}
	}
}

func TestScrubControlDropsC1(t *testing.T) {
	if got := scrubControl("GET /x\u009b31mred\u009dtitle\u009c\x1b[0m\x7f\tok\n"); got != "GET /x31mredtitle[0m\tok\n" {
		t.Fatalf("scrubControl = %q", got)
	}
}
