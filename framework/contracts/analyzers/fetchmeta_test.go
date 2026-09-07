package analyzers_test

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/framework/contracts"
)

// GOFASTR1411 exists because the repo's ONE cross-site predicate
// (core/handler.IsCrossSiteRequest, Sec-Fetch-Site first with the
// Origin-host compare as the fallback) grew nine private copies, and
// the 2026-09-06/07 adversarial round (TestSetupRedSameSiteOriginRefused,
// TestCSRFRedSameSiteFallsThrough) showed the copies diverge in
// exactly the way an attacker needs: two of them early-allow
// "same-site", a value a sibling-subdomain attack honestly carries.
// Fixtures reduce the direct readers (battery/setup token.go,
// battery/auth core.go, battery/admin csrf.go, kiln/chat csrf.go,
// examples/rtc-call main.go, examples/webmcp-remote-assist session.go)
// and pin the quiet postures: core/handler itself, the helper call,
// and prose that merely mentions the header.

// The setup copy, reduced: Header.Get on the request's own header map.
func TestFetchMetadataCopyIsReported(t *testing.T) {
	ds := fixture(t, map[string]string{
		"token.go": `package setup

import "net/http"

func rejectCrossSiteForm(w http.ResponseWriter, r *http.Request) bool {
	if sfs := r.Header.Get("Sec-Fetch-Site"); sfs != "" {
		if sfs == "cross-site" {
			http.Error(w, "forbidden: cross-site request", http.StatusForbidden)
			return true
		}
	}
	return false
}
`,
	})
	d := assertHas(t, ds, contracts.RuleFetchMetadata)
	if !strings.Contains(d.Message, "IsCrossSiteRequest") {
		t.Errorf("message must name the one predicate: %q", d.Message)
	}
}

// The canonical-key map form is the same read; the header map handed
// to a helper (bare-identifier receiver) is too.
func TestFetchMetadataMapAndHelperFormsAreReported(t *testing.T) {
	ds := fixture(t, map[string]string{
		"csrf.go": `package csrf

import "net/http"

func crossSite(h http.Header) bool {
	return h["Sec-Fetch-Site"][0] == "cross-site"
}

func sameSite(h http.Header) bool {
	return h.Get("Sec-Fetch-Site") == "same-site"
}
`,
	})
	if found := countRule(t, ds, contracts.RuleFetchMetadata); len(found) != 2 {
		t.Fatalf("want 2 findings (map form + helper Get), got %d: %v", len(found), found)
	}
}

// core/handler owns the predicate and stays exempt; a caller of the
// shared helper reads nothing itself.
func TestFetchMetadataHandlerAndCallersAreQuiet(t *testing.T) {
	ds := fixture(t, map[string]string{
		"core/handler/crosssite.go": `package handler

import "net/http"

func IsCrossSiteRequest(r *http.Request) bool {
	if s := r.Header.Get("Sec-Fetch-Site"); s != "" {
		return s == "cross-site"
	}
	return false
}
`,
		"route.go": `package app

import (
	"net/http"

	"example.com/app/core/handler"
)

func post(w http.ResponseWriter, r *http.Request) {
	if !handler.IsCrossSiteRequest(r) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
}
`,
	})
	assertNot(t, ds, contracts.RuleFetchMetadata,
		"core/handler owns the predicate and a caller of it reads no header")
}

// Prose that mentions the header (examples/site's ticket body) is a
// string literal in another position, not a read; so is any other
// header's Get.
func TestFetchMetadataProseAndOtherHeadersAreQuiet(t *testing.T) {
	ds := fixture(t, map[string]string{
		"screen.go": `package site

import "net/http"

type ticket struct{ Body string }

var tickets = []ticket{{
	Body: "Suspecting the Sec-Fetch-Site cookie guard.",
}}

func agent(r *http.Request) string {
	return r.Header.Get("User-Agent")
}
`,
	})
	assertNot(t, ds, contracts.RuleFetchMetadata,
		"a display string and another header's Get are not the predicate")
}
