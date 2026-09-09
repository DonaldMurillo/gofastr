package netguard

import (
	"net"
	"strings"
)

// IsLoopbackAuthority reports whether authority ("host" or "host:port")
// names the loopback interface: a loopback IP literal or any casing of
// "localhost" (with or without IPv6 brackets). It is the Host-pin half
// of the DNS-rebinding guard on unauthenticated loopback-bound HTTP
// surfaces: a rebinding attack delivers a page whose Origin matches the
// attacker-named Host, but a browser cannot forge Host, so pinning the
// authority to loopback refuses it.
//
// One definition replaces the three byte-identical copies cmd/kiln's,
// kiln/chat's, and core/mcp's isLoopbackAuthority.
func IsLoopbackAuthority(authority string) bool {
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
