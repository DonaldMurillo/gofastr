package netguard

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"reflect"
	"slices"
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

// GuardPlainTransport returns a clone of tr carrying the guarded
// dial-time hook when tr dials directly: no proxy but the environment's
// and no dial hook but a net.Dialer's, which is what
// http.DefaultTransport.Clone() carries, so its TLS and pool settings
// survive. The clone drops the environment proxy, as GuardedTransport
// does. It returns false
// for any other transport (a proxy, a custom dialer), whose dialer
// cannot be swapped without breaking routing that reaches internal
// addresses by design; the caller wraps those in a resolve-and-check
// RoundTripper instead, the weaker posture. The deprecated Dial and
// DialTLS hooks still take effect when set, so they count as custom
// dialers too. A transport that would speak HTTP/2 on its own keeps
// speaking it.
func GuardPlainTransport(tr *http.Transport, prefix string) (*http.Transport, bool) {
	return withDialer(tr, GuardedTransport(false, prefix).DialContext)
}

// stockProxy reports whether p is no proxy or http.ProxyFromEnvironment,
// the one every http.DefaultTransport clone carries. Any other proxy
// func routes on purpose and keeps the transport out of the guard.
func stockProxy(p func(*http.Request) (*url.URL, error)) bool {
	return p == nil || reflect.ValueOf(p).Pointer() == reflect.ValueOf(http.ProxyFromEnvironment).Pointer()
}

// stockDial reports whether d is no dialer or a (*net.Dialer).DialContext
// method value, the one every http.DefaultTransport clone carries. A
// method value's code pointer is shared by every receiver, so this
// cannot see the dialer's own settings: its timeouts, LocalAddr and
// Control are replaced by the guard's. A dial func of any other shape
// keeps the transport out of the guard.
func stockDial(d func(ctx context.Context, network, addr string) (net.Conn, error)) bool {
	return d == nil || reflect.ValueOf(d).Pointer() == netDialerDialContext
}

var netDialerDialContext = reflect.ValueOf((&net.Dialer{}).DialContext).Pointer()

// withDialer is GuardPlainTransport with the dialer as a parameter, so a
// test can dial loopback through the same clone.
func withDialer(tr *http.Transport, dial func(ctx context.Context, network, addr string) (net.Conn, error)) (*http.Transport, bool) {
	if !stockProxy(tr.Proxy) || !stockDial(tr.DialContext) || tr.DialTLSContext != nil ||
		tr.Dial != nil || tr.DialTLS != nil {
		return nil, false
	}
	guarded := tr.Clone()
	guarded.DialContext = dial
	// The environment proxy goes, as it does in GuardedTransport: the
	// dial check never sees the connection a proxy makes.
	guarded.Proxy = nil
	// Clone() runs tr's HTTP/2 setup, which offers h2 in the TLS
	// handshake, but a transport with its own DialContext skips that
	// setup, so the clone would write HTTP/1.1 onto a connection the
	// server agreed would carry h2. An h2 offer means tr speaks HTTP/2;
	// keep it speaking it.
	if guarded.TLSClientConfig != nil && slices.Contains(guarded.TLSClientConfig.NextProtos, "h2") {
		guarded.ForceAttemptHTTP2 = true
	}
	return guarded, true
}
