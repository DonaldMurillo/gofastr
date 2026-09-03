//go:build red

// RED TEST — open finding, 2026-09-03 adversarial pass round 4 (tests-only;
// no fix applied).
//
// Property: every browser-facing response kiln serves carries
// X-Content-Type-Options: nosniff — the repo's own always-on discipline
// (docs/content/security.md pins "nosniff (always, not configurable)" for
// apps; the default middleware chain delivers it, pinned by
// app_middleware_test.go; the harness web client sets it by hand).
//
// Surfaces: Live.serveApp fallback write path (kiln/live/live.go:302-306) —
// the full HTML document served on ANY unclaimed URL when the request
// prefers HTML. cmd/kiln wires the fallback to chat.HostHTMLForLive, so
// every URL that isn't a built page renders the host page (chat widget +
// world lead). It sets only Content-Type and Cache-Control: no nosniff, no
// X-Frame-Options, no CSP — the one HTML document kiln itself writes that
// bypasses the framework default chain.
//
// Finding: this is the page the operator's browser lands on when they open
// the kiln server root before any page exists, and it embeds world-derived
// lead text; serving a full HTML document with zero security headers breaks
// parity with every other HTML surface in the stack (app pages: nosniff +
// XFO DENY + CSP; relay: nosniff; uploads: nosniff).
//
// Fix direction: set the header set the default chain sets (at minimum
// X-Content-Type-Options: nosniff) on the fallback write at live.go:302 —
// or route the fallback through the same security-header helper the
// framework chain uses.
//
// Severity: low, labeled honestly — loopback dev tool, originGuard already
// pins the DNS-rebinding and CSRF classes, and Content-Type is set
// explicitly to text/html. This is a defense-in-depth parity gap (MIME
// sniffing confusion needs a second bug to matter), pinned because the
// repo's contract is "nosniff always", not "nosniff where it's load-
// bearing".

package live_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/framework"
	"github.com/DonaldMurillo/gofastr/kiln/journal"
	"github.com/DonaldMurillo/gofastr/kiln/live"
)

func TestLiveRedFallbackNoSniff(t *testing.T) {
	l, err := live.New(journal.NewMemory(), func() *framework.App { return framework.NewApp() })
	if err != nil {
		t.Fatalf("live.New: %v", err)
	}
	l.SetFallbackHTML("<!DOCTYPE html><html><body>red host page</body></html>")

	// Unclaimed URL + browser Accept → the fallback document.
	req := httptest.NewRequest(http.MethodGet, "/nothing-here", nil)
	req.Header.Set("Accept", "text/html")
	rec := httptest.NewRecorder()
	l.ServeHTTP(rec, req)
	res := rec.Result()

	if res.StatusCode != http.StatusOK || !strings.Contains(rec.Body.String(), "red host page") {
		t.Fatalf("fallback page not served (code=%d body=%.120s); surface moved, revisit this pin",
			res.StatusCode, rec.Body.String())
	}
	if ct := res.Header.Get("Content-Type"); ct != "text/html; charset=utf-8" {
		t.Fatalf("unexpected Content-Type %q; wrong surface", ct)
	}
	if got := res.Header.Get("X-Content-Type-Options"); got != "nosniff" {
		t.Errorf("SECURITY: fallback HTML document served without X-Content-Type-Options (got %q). This is the full "+
			"page every unclaimed URL returns — the only HTML kiln writes that skips the framework's always-on "+
			"security headers (live.go:302-306 sets Content-Type + Cache-Control only). Fix: set nosniff (and the "+
			"chain's XFO/CSP) on the fallback write.",
			got)
	}
}
