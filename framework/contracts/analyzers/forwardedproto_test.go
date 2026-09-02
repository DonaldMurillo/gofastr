package analyzers_test

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/framework/contracts"
)

// GOFASTR1406 exists because UIHost.resolveBaseURL spliced the raw
// X-Forwarded-Proto value into discovery URLs (probe
// TestDiscoveryURLsIgnoreForwardedProto, pre-fix 7bd789e9, fixed
// a24928c1): one forged header painted an attacker-named origin into the
// agent card and the Link header. Fixtures reduce that site to its shape,
// carry the enum-check fix as the negative, and add two positives that
// never existed in this repo.

// The pre-fix resolveBaseURL, reduced: header to u, u to scheme, scheme
// into the returned origin, and no http/https comparison anywhere.
func TestForwardedProtoWithoutEnumIsReported(t *testing.T) {
	ds := fixture(t, map[string]string{
		"agentready.go": `package uihost

import (
	"net/http"
	"strings"
)

type UIHost struct{ baseURL string }

func (ds *UIHost) resolveBaseURL(req *http.Request) string {
	if ds.baseURL != "" {
		return strings.TrimRight(ds.baseURL, "/")
	}
	scheme := "http"
	if req.TLS != nil {
		scheme = "https"
	}
	if u := req.Header.Get("X-Forwarded-Proto"); u != "" {
		scheme = u
	}
	return scheme + "://" + req.Host
}
`,
	})
	d := assertHas(t, ds, contracts.RuleForwardedProtoEnum)
	if !strings.Contains(d.Message, "request-controlled") {
		t.Errorf("message does not say the header is request-controlled: %q", d.Message)
	}
}

// The fixed resolveBaseURL (a24928c1): the header is honored only as an
// exact http or https.
func TestForwardedProtoEnumFixIsQuiet(t *testing.T) {
	ds := fixture(t, map[string]string{
		"agentready.go": `package uihost

import (
	"net/http"
	"strings"
)

type UIHost struct{ baseURL string }

func (ds *UIHost) resolveBaseURL(req *http.Request) string {
	if ds.baseURL != "" {
		return strings.TrimRight(ds.baseURL, "/")
	}
	scheme := "http"
	if req.TLS != nil {
		scheme = "https"
	}
	if u := req.Header.Get("X-Forwarded-Proto"); u == "http" || u == "https" {
		scheme = u
	}
	return scheme + "://" + req.Host
}
`,
	})
	assertNot(t, ds, contracts.RuleForwardedProtoEnum,
		`u == "http" || u == "https" is the documented enum`)
}

// Two positives with no counterpart in this repo: the value concatenated
// directly, and the value assigned to a proto-named variable and set as a
// url.URL Scheme.
func TestForwardedProtoFiresOnUnrelatedSites(t *testing.T) {
	ds := fixture(t, map[string]string{
		"feed/feed.go": `package feed

import "net/http"

func absolute(r *http.Request) string {
	return r.Header.Get("X-Forwarded-Proto") + "://" + r.Host + "/feed.json"
}
`,
		"synd/origin.go": `package synd

import (
	"net/http"
	"net/url"
)

func origin(r *http.Request) *url.URL {
	proto := r.Header.Get("X-Forwarded-Proto")
	return &url.URL{Scheme: proto, Host: r.Host}
}
`,
	})
	assertHas(t, ds, contracts.RuleForwardedProtoEnum)
	if got := rules(ds)[contracts.RuleForwardedProtoEnum]; got != 2 {
		t.Errorf("expected both synthetic sites to fire, got %d findings", got)
	}
}

// The documented silences: an EqualFold comparison (battery/setup's
// spelling), a switch enum (framework/pluginhost/assets.go's spelling),
// and writing the header outbound (battery/relay's Set), which is not a
// reflection at all.
func TestForwardedProtoEnumCheckIsQuiet(t *testing.T) {
	ds := fixture(t, map[string]string{
		"quiet.go": `package quiet

import (
	"net/http"
	"strings"
)

func secureCookie(r *http.Request) bool {
	return r.TLS != nil || strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https")
}

func origin(r *http.Request) string {
	scheme := "http"
	if xf := r.Header.Get("X-Forwarded-Proto"); xf != "" {
		switch xf {
		case "http", "https":
			scheme = xf
		default:
			return "http://" + r.Host
		}
	}
	return scheme + "://" + r.Host
}

func forward(out *http.Request, tlsTerminated bool) {
	if tlsTerminated {
		out.Header.Set("X-Forwarded-Proto", "https")
	}
}
`,
	})
	assertNot(t, ds, contracts.RuleForwardedProtoEnum,
		"EqualFold, a switch enum, and Header.Set are all documented silences")
}
