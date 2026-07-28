package embed

import (
	"net/http"
	"path"
	"strings"
)

// The embed HTTP surface. These are exported because more than one package has
// to agree on them: uihost mounts them, and the CSRF middleware has to know
// which ones are structurally exempt.
//
// The two API endpoints sit OUTSIDE the /__gofastr/embed/{surface} space on
// purpose — a surface named "exchange" would otherwise shadow the exchange
// endpoint, and "which pattern wins" is not a question a security boundary
// should depend on.
const (
	LoaderPath   = "/__gofastr/embed.js"
	RuntimePath  = "/__gofastr/embed-runtime.js"
	ExchangePath = "/__gofastr/embed-exchange"
	RefreshPath  = "/__gofastr/embed-refresh"
	// SurfacePrefix is the shell + content space: /__gofastr/embed/{surface}
	// and /__gofastr/embed/{surface}/content.
	SurfacePrefix = "/__gofastr/embed/"

	// GrantHeader carries the frame's grant on every credentialed request. A
	// header rather than a cookie, because a cookie would be ambient — and
	// ambient is exactly what an embedded surface must not have.
	GrantHeader = "X-Gofastr-Embed"
)

// CSRFExempt reports whether r targets an embed endpoint that cannot be a CSRF
// target, and so must not be gated on a CSRF token.
//
// This is not a convenience exemption. Double-submit CSRF works by pairing a
// cookie with a header, and no cookie is ever sent from inside an embed frame —
// SameSite is computed against the top-level browsing context, which is the
// customer's site. An app that installs CSRF middleware would therefore 403
// every exchange with "missing cookie" and the feature would be dead, in
// exactly the configuration the framework recommends.
//
// What makes the exemption safe is that these endpoints have no ambient
// credential to abuse. The exchange consumes a single-use nonce the caller must
// already possess; the refresh consumes a grant the caller must already
// possess. A cross-site page that could forge the request still has neither, so
// there is no confused deputy to exploit — which is the same reasoning behind
// middleware.SkipBearerAuth.
func CSRFExempt(r *http.Request) bool {
	if r == nil || r.URL == nil {
		return false
	}
	// A request carrying an embed grant is authenticated by that grant and by
	// nothing ambient, so the double-submit check has nothing to protect. This
	// is what lets an embedded surface's island RPCs reach ORDINARY app routes,
	// which are not under the embed path space and would otherwise 403 with
	// "missing cookie" — no cookie is ever sent from inside a frame.
	//
	// Setting a custom header is not something a cross-site form or an <img>
	// can do; a cross-origin fetch that tries is preflighted, and the app
	// answers no preflight. Same reasoning as middleware.SkipBearerAuth, which
	// exempts Authorization-bearing requests for the same reason.
	//
	// The exemption removes only the CSRF gate. The grant still has to verify
	// before it authenticates anything (see Host.Middleware), so a forged
	// header buys a 401, not a session.
	if r.Header.Get(GrantHeader) != "" {
		return true
	}
	// Match the CLEANED path. r.URL.Path is percent-decoded, so
	// "/__gofastr/embed/%2e%2e/%2e%2e/admin/delete" arrives here as a traversal
	// that has the embed prefix but does not name an embed route. The router
	// redirects such paths today, so nothing is exploitable — but an exemption
	// whose safety depends on another package's redirect behaviour is one
	// refactor away from being wrong.
	clean := path.Clean(r.URL.Path)
	switch clean {
	case ExchangePath, RefreshPath:
		return true
	}
	return strings.HasPrefix(clean, SurfacePrefix)
}
