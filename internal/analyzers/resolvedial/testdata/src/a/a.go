package a

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/url"
	"syscall"

	"dns"
)

func reject(ip net.IP) error {
	if ip.IsLoopback() || ip.IsPrivate() {
		return errors.New("internal")
	}
	return nil
}

// B64, reduced: battery/webhook ssrfGuardedRoundTripper.
type webhookRT struct{ inner http.RoundTripper }

func (g *webhookRT) RoundTrip(r *http.Request) (*http.Response, error) { // want RoundTrip:"resolvesHost"
	ips, err := net.DefaultResolver.LookupIPAddr(r.Context(), r.URL.Hostname())
	if err != nil {
		return nil, err
	}
	for _, ip := range ips {
		if err := reject(ip.IP); err != nil {
			return nil, err
		}
	}
	return g.inner.RoundTrip(r) // want `resolvedial: RoundTrip after a resolve-and-check`
}

// P54, reduced: core/a2a guardedRoundTripper. The IP-literal branch
// delegates before any lookup and stays quiet.
type a2aRT struct{ inner http.RoundTripper }

func (g *a2aRT) RoundTrip(r *http.Request) (*http.Response, error) { // want RoundTrip:"resolvesHost"
	host := r.URL.Hostname()
	if ip := net.ParseIP(host); ip != nil {
		if err := reject(ip); err != nil {
			return nil, err
		}
		return g.inner.RoundTrip(r)
	}
	ips, err := net.DefaultResolver.LookupIPAddr(r.Context(), host)
	if err != nil {
		return nil, err
	}
	for _, ip := range ips {
		if err := reject(ip.IP); err != nil {
			return nil, err
		}
	}
	return g.inner.RoundTrip(r) // want `resolvedial: RoundTrip after a resolve-and-check`
}

// The lookup sits in a same-package helper.
func checkHost(host string) error {
	ips, err := net.LookupIP(host)
	if err != nil {
		return err
	}
	for _, ip := range ips {
		if err := reject(ip); err != nil {
			return err
		}
	}
	return nil
}

type helperRT struct{ inner http.RoundTripper }

func (g helperRT) RoundTrip(r *http.Request) (*http.Response, error) { // want RoundTrip:"resolvesHost"
	if err := checkHost(r.URL.Hostname()); err != nil {
		return nil, err
	}
	return g.inner.RoundTrip(r) // want `resolvedial: RoundTrip after a resolve-and-check`
}

// The lookup sits in another package, two hops down (fact).
type crossRT struct{ inner http.RoundTripper }

func (g crossRT) RoundTrip(r *http.Request) (*http.Response, error) { // want RoundTrip:"resolvesHost"
	if err := dns.Wrapped(r.URL.Hostname()); err != nil {
		return nil, err
	}
	return g.inner.RoundTrip(r) // want `resolvedial: RoundTrip after a resolve-and-check`
}

// A concrete *http.Transport as the inner transport.
type concreteRT struct{ inner *http.Transport }

func (g concreteRT) RoundTrip(r *http.Request) (*http.Response, error) { // want RoundTrip:"resolvesHost"
	if _, err := net.LookupHost(r.URL.Hostname()); err != nil {
		return nil, err
	}
	return g.inner.RoundTrip(r) // want `resolvedial: RoundTrip after a resolve-and-check`
}

// H12, reduced: harness WebFetch preflights the host, then installs the
// dial-time guard only when the caller's client brought no transport.
type webFetch struct{ HTTPClient *http.Client }

func guarded() http.RoundTripper { return &http.Transport{} }

func (w *webFetch) Fetch(ctx context.Context, raw, host string) (*http.Response, error) { // want Fetch:"resolvesHost"
	if err := checkHost(host); err != nil {
		return nil, err
	}
	client := w.HTTPClient
	if client == nil {
		client = &http.Client{}
	}
	safe := *client
	if safe.Transport == nil { // want `resolvedial: the dial-time guard is installed only when Transport is nil`
		safe.Transport = guarded()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, raw, nil)
	if err != nil {
		return nil, err
	}
	return safe.Do(req)
}

// The same, through a cross-package preflight and a reversed nil test.
func fetchReversed(c *http.Client, host string) {
	_ = dns.CheckHost(host)
	if nil == c.Transport { // want `resolvedial: the dial-time guard is installed only when Transport is nil`
		c.Transport = guarded()
	}
}

// The nil-only install spelled as a tagless switch with no default.
func fetchSwitch(c *http.Client, host string) {
	_ = checkHost(host)
	switch {
	case c.Transport == nil: // want `resolvedial: the dial-time guard is installed only when Transport is nil`
		c.Transport = guarded()
	}
}

// A rewrite after the RoundTrip changes nothing the dial used.
type lateRewriteRT struct{ inner http.RoundTripper }

func (g lateRewriteRT) RoundTrip(r *http.Request) (*http.Response, error) { // want RoundTrip:"resolvesHost"
	if err := checkHost(r.URL.Hostname()); err != nil {
		return nil, err
	}
	resp, err := g.inner.RoundTrip(r) // want `resolvedial: RoundTrip after a resolve-and-check`
	r.URL.Host = "logged.invalid"
	return resp, err
}

// --- quiet: the fix spellings -------------------------------------------

// Rewriting the request to the checked IP: the dial targets what was
// checked.
type pinnedRT struct{ inner http.RoundTripper }

func (g pinnedRT) RoundTrip(r *http.Request) (*http.Response, error) { // want RoundTrip:"resolvesHost"
	ips, err := net.LookupIP(r.URL.Hostname())
	if err != nil || len(ips) == 0 {
		return nil, errors.New("resolve")
	}
	if err := reject(ips[0]); err != nil {
		return nil, err
	}
	r2 := r.Clone(r.Context())
	r2.URL.Host = ips[0].String()
	return g.inner.RoundTrip(r2)
}

// Replacing the URL wholesale counts as a rewrite too.
type urlRT struct{ inner http.RoundTripper }

func (g urlRT) RoundTrip(r *http.Request) (*http.Response, error) { // want RoundTrip:"resolvesHost"
	if err := checkHost(r.URL.Hostname()); err != nil {
		return nil, err
	}
	r2 := r.Clone(r.Context())
	r2.URL = pinned(r2)
	return g.inner.RoundTrip(r2)
}

func pinned(r *http.Request) *url.URL { u := *r.URL; return &u }

// A dialer that resolves, checks, and dials the IP it checked: the
// check is at dial time. The literal has no RoundTrip.
func dialChecked() *http.Transport {
	d := &net.Dialer{}
	return &http.Transport{
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			host, port, err := net.SplitHostPort(addr)
			if err != nil {
				return nil, err
			}
			ips, err := net.DefaultResolver.LookupIPAddr(ctx, host)
			if err != nil || len(ips) == 0 {
				return nil, errors.New("resolve")
			}
			if err := reject(ips[0].IP); err != nil {
				return nil, err
			}
			return d.DialContext(ctx, network, net.JoinHostPort(ips[0].IP.String(), port))
		},
	}
}

// A Control hook checks the numeric address the dialer connects to.
func controlChecked() *http.Transport {
	d := &net.Dialer{Control: func(network, address string, _ syscall.RawConn) error {
		host, _, _ := net.SplitHostPort(address)
		return reject(net.ParseIP(host))
	}}
	return &http.Transport{DialContext: d.DialContext}
}

// --- quiet: silent postures ---------------------------------------------

// Registration-time validation resolves and returns; nothing is sent.
func validateURL(host string) error { return checkHost(host) }

// A RoundTripper with no lookup at all, and one that only checks a
// literal (no DNS: nothing to rebind).
type plainRT struct{ inner http.RoundTripper }

func (g plainRT) RoundTrip(r *http.Request) (*http.Response, error) {
	if err := dns.Literal(r.URL.Hostname()); err != nil {
		return nil, err
	}
	return g.inner.RoundTrip(r)
}

// A preflight in front of Do with no conditional install: the rule
// cannot see the client's transport, and in front of a dial-guarded one
// the preflight is defence in depth.
func preflightThenDo(c *http.Client, req *http.Request) (*http.Response, error) {
	if err := checkHost(req.URL.Hostname()); err != nil {
		return nil, err
	}
	return c.Do(req)
}

// A nil-only install with no lookup in the function: a plain
// "default the transport" constructor.
func defaultTransport(c *http.Client) *http.Client {
	cc := *c
	if cc.Transport == nil {
		cc.Transport = guarded()
	}
	return &cc
}

// Every branch assigns the transport (ssrfGuardClient's chain).
func everyBranch(c *http.Client, host string) {
	_ = checkHost(host)
	if c.Transport == nil {
		c.Transport = guarded()
	} else {
		c.Transport = plainRT{inner: c.Transport}
	}
}

// A lookup inside a goroutine literal is that literal's own body: the
// RoundTrip in the enclosing function does not follow it.
func lookupInLiteral(rt http.RoundTripper, r *http.Request) (*http.Response, error) {
	go func() { _, _ = net.LookupIP("example.com") }()
	return rt.RoundTrip(r)
}

// A reverse lookup does not resolve the dial target.
func reverseThenTrip(rt http.RoundTripper, r *http.Request) (*http.Response, error) {
	_, _ = net.LookupAddr("192.0.2.1")
	return rt.RoundTrip(r)
}

// A nil test that does not install a transport is not this shape.
func nilRefused(c *http.Client, host string) error {
	_ = checkHost(host)
	if c.Transport == nil {
		return errors.New("a transport is required")
	}
	return nil
}

// A switch whose default branch wraps the caller's transport.
func switchWithDefault(c *http.Client, host string) {
	_ = checkHost(host)
	switch {
	case c.Transport == nil:
		c.Transport = guarded()
	default:
		c.Transport = plainRT{inner: c.Transport}
	}
}
