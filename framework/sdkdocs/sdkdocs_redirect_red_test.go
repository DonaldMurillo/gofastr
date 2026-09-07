//go:build red

package sdkdocs

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core/router"
)

// RED TEST — open finding, 2026-09-06/07 adversarial round 5, phase 2
// (family enumeration; tier T2).
// Property: every DecisionRedirect URL is scrubbed of C0/DEL bytes before
// it reaches the Location header. uihost pins the contract at all four of
// its emit sites (scrubCtl at uihost.go:1328 and :3263 for the literal
// targets, :1374 and :2273 for the identical app.Decision value —
// TestRedirect308LocationScrubbed / TestTrailingSlashLocationScrubbed).
// Surfaces: sdkdocs.go:318-325 — artifactHandler's DecisionRedirect arm
// emits http.Redirect(w, r, d.URL, 303) with the raw Policy URL. This is
// the tree's only unscrubbed consumer of the same Decision value: the
// contract's analyzer (controlbytes family) cannot trace the Policy seam,
// so the divergence survived family enumeration of .go sinks.
// Finding: a Policy URL of "/onboard\r\nSet-Cookie: pwn=1\x7f" lands
// byte-for-byte in Location (probe-verified): raw CR/LF forge headers and
// log lines downstream, DEL breaks HTTP/2 writes. On a live wire the
// transport mangles or dies on these bytes, so the emit-site contract is
// asserted at the ResponseRecorder — the same observation point the
// uihost sibling pins.
// Fix direction: scrub d.URL at the emit site the way uihost does
// (export/reuse scrubCtl, or the shared header-sanitize helper), keeping
// the visible path intact; the control leg pins that the scrub must not
// mangle clean URLs.

// TestPolicyRedirectRedScrubbed: a Policy redirect carrying control bytes
// still 303s, scrubbed, with the visible destination intact.
func TestPolicyRedirectRedScrubbed(t *testing.T) {
	reg := testRegistry()
	cfg := Config{
		Registry:  reg,
		Artifacts: testArtifacts(t, reg, true),
		Policy: app.PolicyFunc(func(ctx context.Context) app.Decision {
			return app.Decision{Kind: app.DecisionRedirect, URL: "/onboard\r\nSet-Cookie: pwn=1\x7f"}
		}),
	}
	coreApp := app.NewApp("red")
	r := router.New()
	if err := Mount(coreApp, r, cfg); err != nil {
		t.Fatalf("setup broken: mount sdkdocs: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/docs/api/sdk/go.zip", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	loc := rec.Header().Get("Location")
	if rec.Code != http.StatusSeeOther || loc == "" {
		t.Fatalf("setup broken: expected 303 + Location from the policy redirect, got %d %q (body %.200s)",
			rec.Code, loc, rec.Body.String())
	}
	if !strings.Contains(loc, "/onboard") {
		t.Errorf("SECURITY: [sdkdocs-policy-redirect-scrub] scrub mangled the destination: Location %q no longer contains \"/onboard\" — the emit site must strip control bytes, not the visible path", loc)
	}
	for i := range len(loc) {
		c := loc[i]
		if (c < 0x20 && c != '\t') || c == 0x7f {
			t.Errorf("SECURITY: [sdkdocs-policy-redirect-scrub] 303 Location %q carries raw control byte 0x%02x at %d — "+
				"the Policy Decision's URL flows into http.Redirect unscrubbed; uihost scrubs the identical "+
				"value at all four of its emit sites (uihost.go:1328/:1374/:2273/:3263), artifactHandler is the "+
				"one divergent consumer", loc, c, i)
			break
		}
	}
}

// TestPolicyRedirectRedCleanRoundTrip: the control leg — a clean Policy
// URL survives the scrub verbatim.
func TestPolicyRedirectRedCleanRoundTrip(t *testing.T) {
	reg := testRegistry()
	cfg := Config{
		Registry:  reg,
		Artifacts: testArtifacts(t, reg, true),
		Policy: app.PolicyFunc(func(ctx context.Context) app.Decision {
			return app.Decision{Kind: app.DecisionRedirect, URL: "/onboarding"}
		}),
	}
	coreApp := app.NewApp("red")
	r := router.New()
	if err := Mount(coreApp, r, cfg); err != nil {
		t.Fatalf("setup broken: mount sdkdocs: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/docs/api/sdk/go.zip", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	loc := rec.Header().Get("Location")
	if rec.Code != http.StatusSeeOther || loc != "/onboarding" {
		t.Fatalf("setup broken: clean policy URL must round-trip verbatim, got %d Location %q (body %.200s)",
			rec.Code, loc, rec.Body.String())
	}
}
