package chat

import (
	"net/http"
	"net/url"
	"strings"
)

// sameOriginOnly wraps a state-changing kiln handler so a cross-site
// browser request cannot reach it.
//
// kiln's transport is deliberately unauthenticated and the loopback bind
// is the boundary. That boundary does not see the one caller class this
// guards: a page the operator visits can POST to /kiln/tool/approve_plan
// from their own machine, and the TCP peer is loopback either way. The
// plan gate's whole security leg is a human looking at a card and
// approving it, so a silent cross-site approve defeats exactly the
// control kiln is built around.
//
// An Origin allow-list leaves every non-browser caller untouched: the
// agent's $KILN_URL client and curl send no Origin at all, and the
// operator's own panel is same-origin. Sec-Fetch-Site is honoured first
// where the browser sends it, because a sandboxed or redirected page
// sends Origin: null, which parses to no host and would otherwise read
// as "not a browser".
func sameOriginOnly(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if crossSite(r) {
			http.Error(w, "forbidden: cross-site request", http.StatusForbidden)
			return
		}
		h.ServeHTTP(w, r)
	})
}

// crossSite reports whether the request came from another site.
func crossSite(r *http.Request) bool {
	switch r.Header.Get("Sec-Fetch-Site") {
	case "same-origin", "same-site", "none":
		// "none" is a user-initiated navigation (address bar, bookmark),
		// which cannot carry an attacker's body.
		return false
	case "cross-site":
		return true
	}
	origin := r.Header.Get("Origin")
	if origin == "" {
		return false // no Origin at all: not a browser request
	}
	u, err := url.Parse(origin)
	if err != nil || u.Host == "" {
		// Origin: null, or unparseable. A browser sends null from a
		// sandboxed iframe or after a cross-origin redirect; treating it
		// as "not a browser" is how this check gets walked around.
		return true
	}
	return !strings.EqualFold(u.Host, r.Host)
}
