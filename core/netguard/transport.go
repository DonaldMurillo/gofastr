package netguard

import (
	"fmt"
	"net"
	"net/http"
	"syscall"
	"time"
)

// GuardedTransport returns an *http.Transport for delivering to
// caller-supplied URLs. The net.Dialer.Control hook re-runs the
// internal-address predicate on the ACTUAL resolved (network, address)
// at connect time, which closes the DNS-rebinding / TOCTOU window a
// registration-time URL check cannot: a host that resolved public at
// validation and is later re-pointed at 127.0.0.1, 169.254.169.254, or
// an RFC1918 address never gets dialed. When allowPrivate is true the
// dial-time check is skipped, matching the registration-time opt-out
// posture (core/a2a PushOptions.AllowPrivate, battery/webhook
// Options.AllowPrivateNetworks). prefix names the calling surface in
// the error text ("a2a", "webhook", ...).
//
// A guarded transport ignores HTTP_PROXY, HTTPS_PROXY and ALL_PROXY: a
// proxy would make the connection the dial check never sees. An app that
// must egress through a proxy has to pass allowPrivate (the surface's own
// opt-out) and accept that the proxy, not this package, decides what is
// reachable.
//
// One constructor replaces the two byte-identical copies core/a2a's
// guardedTransport and battery/webhook's ssrfGuardedTransport; a new
// outbound-fetch surface calls this instead of growing a third.
func GuardedTransport(allowPrivate bool, prefix string) *http.Transport {
	dialer := &net.Dialer{
		Timeout:   10 * time.Second,
		KeepAlive: 30 * time.Second,
	}
	if !allowPrivate {
		dialer.Control = func(network, address string, _ syscall.RawConn) error {
			host, _, err := net.SplitHostPort(address)
			if err != nil {
				host = address
			}
			ip := net.ParseIP(host)
			if ip == nil {
				// Control sees the already-resolved numeric address; a
				// non-IP here is unexpected, refuse rather than dial
				// blind.
				return fmt.Errorf("%s: dial address %q is not a resolved IP", prefix, address)
			}
			if reason := Reason(ip); reason != "" {
				return fmt.Errorf("%s: %s %s not allowed", prefix, reason, ip)
			}
			return nil
		}
	}
	tr := http.DefaultTransport.(*http.Transport).Clone()
	tr.DialContext = dialer.DialContext
	if !allowPrivate {
		// Clone() carries ProxyFromEnvironment. With HTTP_PROXY or
		// HTTPS_PROXY set, the Control hook above only ever sees the
		// proxy's address, while the proxy resolves and connects to the
		// target, so an internal target sails through. The guard cannot
		// inspect a connection a proxy makes, so a guarded transport
		// never uses one (core/webbotauth made the same call).
		tr.Proxy = nil
	}
	return tr
}
