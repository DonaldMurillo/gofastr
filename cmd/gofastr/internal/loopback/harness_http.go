package loopback

import (
	"net"
	"strings"
)

// IsWildcardHost reports whether a bind address means "every interface".
func IsWildcardHost(host string) bool {
	switch host {
	case "", "0.0.0.0", "::", "[::]":
		return true
	}
	return false
}

// LoopbackGuards returns the Host authorities and browser Origins the
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
// Loopback and wildcard binds admit the standard loopback spellings.
// Loopback binds also admit the bound authority. Other binds admit only their own authority.
func LoopbackGuards(addr string) (hosts, origins []string) {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return []string{addr}, []string{"http://" + addr}
	}
	names := []string{host}
	loopback := IsLoopbackHost(host)
	if loopback || IsWildcardHost(host) {
		names = []string{"127.0.0.1", "localhost", "::1"}
		if loopback {
			names = append(names, host)
		}
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
