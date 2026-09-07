package chat

import (
	"net"
	"net/http"
	"strings"

	"github.com/DonaldMurillo/gofastr/core/handler"
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
		// The write guard's own Host pin, the same one readGuard 25
		// lines below applies: DNS rebinding arrives same-origin (after
		// the rebind the attacker's page and the listener agree on the
		// attacker-named Host), so every Origin↔Host comparison passes
		// and only a Host pin refuses it — a browser cannot forge Host.
		// Origin-less callers (the agent transport, curl) pass
		// untouched, exactly as the read arm's contract states.
		if r.Header.Get("Origin") != "" && !isLoopbackAuthority(r.Host) {
			http.Error(w, "forbidden: unexpected Host (DNS-rebinding guard)", http.StatusForbidden)
			return
		}
		h.ServeHTTP(w, r)
	})
}

// readGuard wraps the world-disclosing GET surfaces (/kiln/world, the
// /kiln/status fields that carry the IR, and the /.kiln/events stream)
// so a cross-site or rebound browser subscriber is refused.
//
// sameOriginOnly covers the plain cross-site fetch, but DNS rebinding
// arrives same-origin: after the rebind the attacker's page and the
// listener agree on the attacker-named Host, so every Origin↔Host
// comparison passes. Only a Host pin refuses it, because a browser
// cannot forge Host. The pin accepts any loopback authority — kiln's
// default bind is 127.0.0.1:8765, so the operator's panel and any
// localhost spelling all match. Requests without an Origin header are
// not browsers (curl, the $KILN_URL agent transport, MCP/ACP clients)
// and pass untouched, matching the POST family's contract. The
// framework's own SSE half applies the same gate (core/mcp
// sseGetHandler); cmd/kiln's outer originGuard covers only its own
// process, and Mount is a library surface.
func readGuard(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if crossSite(r) {
			http.Error(w, "forbidden: cross-site request", http.StatusForbidden)
			return
		}
		if r.Header.Get("Origin") != "" && !isLoopbackAuthority(r.Host) {
			http.Error(w, "forbidden: unexpected Host (DNS-rebinding guard)", http.StatusForbidden)
			return
		}
		h.ServeHTTP(w, r)
	})
}

// isLoopbackAuthority reports whether authority ("host" or "host:port")
// names the loopback interface.
func isLoopbackAuthority(authority string) bool {
	host := authority
	if h, _, err := net.SplitHostPort(authority); err == nil {
		host = h
	}
	host = strings.Trim(host, "[]")
	if strings.EqualFold(host, "localhost") {
		return true
	}
	if ip := net.ParseIP(host); ip != nil {
		return ip.IsLoopback()
	}
	return false
}

// crossSite reports whether the request came from another site. The
// cross-site decision itself is the repo-wide predicate
// handler.IsCrossSiteRequest — kiln's copy used to trust
// Sec-Fetch-Site: same-site before any Origin compare, but the site
// computation drops the port, so a page on ANY sibling port of the
// kiln host is same-site while its Origin names a different origin;
// the shared predicate falls through to the Origin↔Host comparison for
// exactly that value, the convention battery/auth and battery/admin
// pin.
//
// One deliberate divergence, kept for kiln's pinned contract: an
// opaque Origin ("null") with no Fetch Metadata is refused here. The
// shared predicate allows it because a legitimate top-level same-origin
// FORM navigation sends it too, but kiln's surfaces are JSON RPC driven
// by fetch(), never form navigations, so a null Origin reaching this
// guard is the sandboxed-frame / cross-origin-redirect shape
// (TestReadRoutesRefuseCrossSiteSub's "Origin null" leg). Where the
// browser does send Sec-Fetch-Site, the header vouches and "null" is
// not second-guessed.
func crossSite(r *http.Request) bool {
	return handler.IsCrossSiteRequestStrict(r)
}
