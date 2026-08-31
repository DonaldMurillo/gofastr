package web

import (
	"net"
	"net/http"
	"net/url"
	"strings"
)

// guardLoopback refuses a request that a page on another site could have
// made, and reports whether it answered the request itself.
//
// This sidecar binds 127.0.0.1 on a random port and serves an SSE stream
// plus /input, which POSTs SendInput into the agent session at model
// authority. The bind address is not a boundary against a browser: any
// page the operator visits can fetch these endpoints from their own
// machine. /input decodes JSON regardless of Content-Type, so a
// text/plain fetch() -- no preflight, nothing to block -- injects a
// prompt into the live session.
//
// Two checks, matching the CLI sidecar's loopbackGuards:
//
//   - Host must be a loopback authority. This is the DNS-rebinding
//     defence: an attacker pointing their own domain at 127.0.0.1 makes
//     the browser treat this server as same-origin, and Origin alone
//     cannot see that.
//   - Origin, WHEN PRESENT, must match the Host. A browser always sends
//     it on a cross-origin request; curl and dev tooling send none and
//     keep working, which is the whole reason this is an allow-list on
//     Origin rather than a requirement for it.
func guardLoopback(w http.ResponseWriter, r *http.Request) bool {
	if !loopbackHost(r.Host) {
		http.Error(w, "forbidden: non-loopback Host", http.StatusForbidden)
		return true
	}
	if origin := r.Header.Get("Origin"); origin != "" && !originMatchesHost(origin, r.Host) {
		http.Error(w, "forbidden: cross-origin request", http.StatusForbidden)
		return true
	}
	return false
}

// loopbackHost reports whether a Host authority names this machine.
// A bare hostname that merely RESOLVES to 127.0.0.1 is not accepted:
// that resolution is exactly what a rebinding attacker controls.
func loopbackHost(host string) bool {
	h := host
	if parsed, _, err := net.SplitHostPort(host); err == nil {
		h = parsed
	}
	h = strings.Trim(h, "[]")
	if h == "localhost" {
		return true
	}
	ip := net.ParseIP(h)
	return ip != nil && ip.IsLoopback()
}

// originMatchesHost reports whether an Origin header names the same
// authority the request was addressed to.
func originMatchesHost(origin, host string) bool {
	u, err := url.Parse(origin)
	if err != nil || u.Host == "" {
		// Origin: null (sandboxed iframe, cross-origin redirect) parses
		// to no host. It is a browser request from somewhere that lost
		// its origin, which is not this server.
		return false
	}
	return strings.EqualFold(u.Host, host)
}
