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

// LoopbackGuards returns the Host authorities and Origins a server bound at addr accepts.
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

// HostAllowed reports whether host matches an allowed authority, ignoring case.
func HostAllowed(host string, allowed []string) bool {
	for _, a := range allowed {
		if strings.EqualFold(host, a) {
			return true
		}
	}
	return false
}
