// check-csp:ignore-file
// The harness web client is a dev-only operator surface, not a
// production browser app, so the framework's strict-CSP contract
// does not apply. The renderTUIShell helper inlines a tiny
// bootstrap <script> + <style> bundle so the harness runs without
// a build step or an extra HTTP round-trip; that's the explicit
// trade-off, hence the directive.

package loopback

import (
	"net"
	"strings"
)

// loopbackGuards returns the Host authorities and browser Origins the
// sidecar accepts for a listener bound at addr.
//
// Host-pinning is the DNS-rebinding defence. Without it, an attacker
// who points their own domain at 127.0.0.1 makes the victim's browser
// treat the sidecar as same-origin, which lets their JS read response
// bodies, including the chat page's harness-token meta tag, and then
// drive /v1/sessions/<id>/input and .../permission as an authenticated
// client. Comparing Host against the bound authority breaks that: a
// rebound request still carries the attacker's own name.
//
// When the operator passes an explicit non-loopback -listen address we
// pin to exactly that authority rather than widening to loopback
// aliases, so an intentional LAN bind keeps working without also
// admitting every other name that resolves to it.
// isWildcardHost reports whether a bind address means "every interface".
func IsWildcardHost(host string) bool {
	switch host {
	case "", "0.0.0.0", "::", "[::]":
		return true
	}
	return false
}

func LoopbackGuards(addr string) (hosts, origins []string) {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return []string{addr}, []string{"http://" + addr}
	}
	names := []string{host}
	if IsLoopbackHost(host) || IsWildcardHost(host) {
		// Bound to loopback: accept the usual spellings of it, since
		// the operator may click either the IP or the name.
		//
		// A WILDCARD bind gets the same list. net.Listen(":8090") reports its
		// address as "[::]:8090", and "[::]" is a host no browser ever sends,
		// so pinning to it made every request 403, including the one from the
		// URL the tool prints. `--addr=:8090` is a documented invocation; it
		// has to work. The guard still does its job: DNS rebinding needs a
		// hostname that resolves to the bind address, and an attacker's
		// hostname is not "localhost".
		names = []string{"127.0.0.1", "localhost", "::1", "[::1]"}
	}
	seen := make(map[string]bool, len(names))
	for _, n := range names {
		authority := net.JoinHostPort(n, port)
		if strings.HasPrefix(n, "[") { // already bracketed
			authority = n + ":" + port
		}
		if seen[authority] {
			continue
		}
		seen[authority] = true
		hosts = append(hosts, authority)
		origins = append(origins, "http://"+authority)
	}
	return hosts, origins
}

// IsLoopbackHost reports whether host names the loopback interface.
func IsLoopbackHost(host string) bool {
	h := strings.Trim(host, "[]")
	if strings.EqualFold(h, "localhost") {
		return true
	}
	if ip := net.ParseIP(h); ip != nil {
		return ip.IsLoopback()
	}
	return false
}

// HostAllowed reports whether the request's Host matches one of the
// pinned authorities. An empty list means nothing was pinned, which
// only happens if the caller skipped loopbackGuards: fail closed.
func HostAllowed(host string, allowed []string) bool {
	for _, a := range allowed {
		if strings.EqualFold(host, a) {
			return true
		}
	}
	return false
}
