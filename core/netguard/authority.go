package netguard

import (
	"net"
	"strings"
)

// IsLoopbackAuthority reports whether authority ("host" or "host:port",
// brackets allowed) is a loopback IP literal or localhost in any case.
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
