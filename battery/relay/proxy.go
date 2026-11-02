package relay

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"maps"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/DonaldMurillo/gofastr/core/i18n"
	"github.com/DonaldMurillo/gofastr/core/netguard"
)

// requestDeadline bounds one proxied request end to end. The effective
// deadline is min(inherited request context, 30s) so an app-level
// timeout or an impatient client can always cut a stuck upstream off.
const requestDeadline = 30 * time.Second

// appHeaderPrefix names the app's own request headers (the embed grant,
// the runtime's navigation markers). stripInbound drops every one: the
// embed grant is a bearer credential boot-embed.js sends on each
// same-origin fetch from an embed frame. A test pins embed.GrantHeader
// inside it.
const appHeaderPrefix = "X-Gofastr-"

// stripInboundHeaders never reach the upstream. The relay is a public
// edge: the app's session cookie, bearer tokens, CSRF material, and
// API keys have no business at a third-party service, and inbound
// X-Forwarded-* is trivially spoofable by direct clients.
var stripInboundHeaders = []string{
	http.CanonicalHeaderKey("Cookie"),
	http.CanonicalHeaderKey("Authorization"),
	http.CanonicalHeaderKey("Proxy-Authorization"),
	http.CanonicalHeaderKey("X-CSRF-Token"),
	http.CanonicalHeaderKey("X-API-Key"),
	// Client-claimed forwarding identity. X-Forwarded-* is handled by the
	// prefix sweep in stripInbound, but these carry the same claim under
	// names that do not share it, and an upstream that trusts any of them
	// (they are the common CDN spellings) reads whatever the caller put
	// one curl -H away. The relay derives the client IP from the
	// connection and writes X-Forwarded-For itself.
	http.CanonicalHeaderKey("X-Real-IP"),
	http.CanonicalHeaderKey("X-Client-IP"),
	http.CanonicalHeaderKey("True-Client-IP"),
	http.CanonicalHeaderKey("CF-Connecting-IP"),
	http.CanonicalHeaderKey("Fastly-Client-IP"),
	http.CanonicalHeaderKey("Forwarded"),
}

// stripOutboundHeaders never reach the client. Set-Cookie and the
// authenticate challenges are the vendor trying to establish identity
// the relay deliberately withholds; Access-Control-* would make the
// app origin advertise a third party's CORS policy. Refresh is a
// header-driven navigation (a vendor 200 can steer the browser to any
// origin, the same leak the Location→502 refusal exists to close, and
// an open redirector on top); Clear-Site-Data is its destructive
// sibling, deleting the visitor's app-origin cookies and storage from
// the vendor's side.
var stripOutboundHeaders = []string{
	http.CanonicalHeaderKey("Set-Cookie"),
	http.CanonicalHeaderKey("WWW-Authenticate"),
	http.CanonicalHeaderKey("Proxy-Authenticate"),
	http.CanonicalHeaderKey("Refresh"),
	http.CanonicalHeaderKey("Clear-Site-Data"),
	// Service-Worker-Allowed widens a worker's scope past its script's
	// path: vendor script could register one over every app page.
	http.CanonicalHeaderKey("Service-Worker-Allowed"),
	// Origin-wide policy the browser keeps after this response. NEL with
	// Report-To / Reporting-Endpoints reports requests to every app URL
	// to a vendor endpoint for max_age, past the page's CSP; Alt-Svc
	// steers later app traffic; Accept-CH / Critical-CH make every app
	// request carry client hints (which the relay then forwards back);
	// Strict-Transport-Security pins the app's subdomains; Set-Login is
	// the origin's FedCM login status. The host sets these, never a
	// vendor (a value the host set before the relay ran stays).
	http.CanonicalHeaderKey("NEL"),
	http.CanonicalHeaderKey("Report-To"),
	http.CanonicalHeaderKey("Reporting-Endpoints"),
	http.CanonicalHeaderKey("Alt-Svc"),
	http.CanonicalHeaderKey("Accept-CH"),
	http.CanonicalHeaderKey("Critical-CH"),
	http.CanonicalHeaderKey("Strict-Transport-Security"),
	http.CanonicalHeaderKey("Set-Login"),
}

// reverseProxy keeps the concrete type in one place; the handler layer
// speaks http.Handler.
type reverseProxy = httputil.ReverseProxy

// newTransport builds the shared outbound transport for all routes.
// One transport, one connection pool: per-route pools would multiply
// idle sockets by the number of vendors for no benefit.
func newTransport() *http.Transport {
	dialer := &net.Dialer{
		Timeout:   5 * time.Second,
		KeepAlive: 30 * time.Second,
	}
	return &http.Transport{
		// An environment proxy resolves the destination outside this process,
		// bypassing the relay's dial-time internal-address check. The relay
		// connects directly and validates every address returned for the fixed
		// upstream before opening a socket.
		DialContext:           guardedDialContext(net.DefaultResolver.LookupIPAddr, dialer.DialContext),
		MaxIdleConns:          100,
		MaxIdleConnsPerHost:   16,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   5 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
		ResponseHeaderTimeout: 10 * time.Second,
		ForceAttemptHTTP2:     true,
	}
}

// guardedDialContext validates request-time DNS results before dialing.
// Construction-time checks alone do not stop DNS rebinding (including a name
// that was temporarily unresolvable when the Relay was created).
func guardedDialContext(
	lookup func(context.Context, string) ([]net.IPAddr, error),
	dial func(context.Context, string, string) (net.Conn, error),
) func(context.Context, string, string) (net.Conn, error) {
	return func(ctx context.Context, network, addr string) (net.Conn, error) {
		host, port, err := net.SplitHostPort(addr)
		if err != nil {
			return nil, err
		}
		ips := []net.IPAddr{}
		if ip := net.ParseIP(host); ip != nil {
			ips = append(ips, net.IPAddr{IP: ip})
		} else {
			ips, err = lookup(ctx, host)
			if err != nil {
				return nil, err
			}
		}
		if len(ips) == 0 {
			return nil, fmt.Errorf("relay: upstream host resolved to no addresses")
		}

		loopbackName := isLoopbackHost(host)
		for _, candidate := range ips {
			if candidate.IP == nil ||
				(loopbackName && !candidate.IP.IsLoopback()) ||
				(!loopbackName && netguard.IsInternal(candidate.IP)) {
				return nil, fmt.Errorf("relay: upstream host resolved to an internal address")
			}
		}

		var lastErr error
		for _, candidate := range ips {
			ip := candidate.IP.String()
			if candidate.Zone != "" {
				ip += "%" + candidate.Zone
			}
			conn, err := dial(ctx, network, net.JoinHostPort(ip, port))
			if err == nil {
				return conn, nil
			}
			lastErr = err
		}
		return nil, lastErr
	}
}

func isLoopbackHost(host string) bool {
	lower := strings.ToLower(host)
	if lower == "localhost" || strings.HasSuffix(lower, ".localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// handler wraps one route's proxy with the request-side guards:
// tail validation, body cap, and the per-request deadline. Method
// allow-listing (405 + Allow) and unknown-tail 404s come from the
// ServeMux patterns registered in Init.
func (r *Relay) handler(rt *routeRuntime) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if rt.subtree {
			tail := req.PathValue("rest")
			if err := validateTail(tail, req.URL.RawPath != ""); err != nil {
				r.logger.Warn("relay: refused hostile tail", "err", err.Error(), "path", req.URL.EscapedPath())
				i18n.HTTPError(w, req, http.StatusBadRequest, i18n.StatusPhrase(http.StatusBadRequest))
				return
			}
		}
		// Declared-length bodies are refused before anything is read;
		// chunked bodies trip the reader mid-copy and surface through
		// the proxy's error handler, which maps them to 413 too.
		if req.ContentLength > rt.maxBody {
			i18n.HTTPError(w, req, http.StatusRequestEntityTooLarge, i18n.StatusPhrase(http.StatusRequestEntityTooLarge))
			return
		}
		req.Body = http.MaxBytesReader(w, req.Body, rt.maxBody)

		ctx, cancel := context.WithTimeout(req.Context(), requestDeadline)
		defer cancel()
		req = req.WithContext(ctx)

		rt.proxy.ServeHTTP(&hostHeaderWriter{ResponseWriter: w, host: w.Header().Clone()}, req)
	})
}

// hostHeaderWriter keeps an upstream's informational responses from the
// browser. ReverseProxy forwards a 1xx (103 Early Hints) with the 1xx's
// own headers, which never pass modifyResponse, so an absolute Link
// preload would reach the browser; it then clears the header map,
// dropping the security headers the host set before dispatch. The
// writer swallows every 1xx and, on the final status, puts back each
// host header value the clear removed, ahead of the upstream's, which
// is the header set a response with no 1xx has.
type hostHeaderWriter struct {
	http.ResponseWriter
	host http.Header
}

func (w *hostHeaderWriter) WriteHeader(code int) {
	if code < 200 {
		return
	}
	h := w.ResponseWriter.Header()
	for _, name := range slices.Sorted(maps.Keys(w.host)) {
		var missing []string
		for _, v := range w.host[name] {
			if !slices.Contains(h[name], v) {
				missing = append(missing, v)
			}
		}
		if len(missing) > 0 {
			h[name] = append(missing, h[name]...)
		}
	}
	w.ResponseWriter.WriteHeader(code)
}

// Unwrap lets http.ResponseController reach the client writer's Flush
// and deadlines.
func (w *hostHeaderWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }

// newProxy builds the ReverseProxy for one route. The Rewrite hook (not
// the deprecated Director) means the outbound request is built from
// scratch: scheme, host, and port come ONLY from the route's parsed
// upstream; request data can influence at most the path tail and the
// query string.
func (r *Relay) newProxy(rt *routeRuntime) *reverseProxy {
	return &httputil.ReverseProxy{
		Transport: r.transport,
		Rewrite: func(pr *httputil.ProxyRequest) {
			in, out := pr.In, pr.Out
			out.URL = &url.URL{
				Scheme:   rt.upstream.Scheme,
				Host:     rt.upstream.Host,
				Path:     joinUpstreamPath(rt.upstream.Path, in.PathValue("rest")),
				RawQuery: in.URL.RawQuery,
			}
			out.Host = rt.upstream.Host

			stripInbound(out.Header)
			// ReverseProxy puts Connection/Upgrade back after its hop-by-hop
			// strip so websockets can pass; the relay refuses protocol
			// switches (see refuseSwitch), so the vendor never gets asked.
			out.Header.Del("Connection")
			out.Header.Del("Upgrade")
			// A relayed request is same-origin, so the browser sent the
			// full page URL as Referer, query string (a reset or
			// magic-link token) included. Sent to the vendor directly,
			// its default policy would have sent the origin alone; the
			// vendor gets no more than that here.
			if ref := refererOrigin(in.Header.Get("Referer")); ref != "" {
				out.Header.Set("Referer", ref)
			} else {
				out.Header.Del("Referer")
			}
			// Forwarded metadata is derived from the connection, never
			// relayed from the client.
			out.Header.Set("X-Forwarded-For", r.clientIP(in))
			out.Header.Set("X-Forwarded-Proto", forwardedProto(in))
		},
		ErrorHandler: r.proxyError,
		ModifyResponse: func(resp *http.Response) error {
			if resp.StatusCode == http.StatusSwitchingProtocols {
				return r.refuseSwitch(rt, resp)
			}
			r.modifyResponse(rt, resp)
			return nil
		},
	}
}

// errSwitchRefused is the ModifyResponse error for an upstream 101.
var errSwitchRefused = errors.New("relay: upstream protocol switch refused")

// refuseSwitch turns down an upstream 101 Switching Protocols. The relay
// is a bounded request/response proxy (body caps, the request deadline,
// no-store); a switched protocol would hand the vendor an open socket
// past all of them, so it is refused rather than spliced. Returning an
// error makes ReverseProxy close the upstream body, which owns the
// switched connection, before the error handler answers 502. Without it
// ReverseProxy met cappedBody (not an io.ReadWriteCloser), answered 502
// and left that connection open.
func (r *Relay) refuseSwitch(rt *routeRuntime, resp *http.Response) error {
	r.logger.Warn("relay: refused upstream protocol switch",
		"upgrade", url.PathEscape(resp.Header.Get("Upgrade")),
		"upstream", rt.upstream.Host)
	return errSwitchRefused
}

// stripInbound removes credentials and spoofable forwarding metadata
// from the outbound header set. ReverseProxy itself removes hop-by-hop
// headers; this is the application-layer denylist on top.
func stripInbound(h http.Header) {
	for _, name := range stripInboundHeaders {
		h.Del(name)
	}
	for name := range h {
		if strings.HasPrefix(name, "X-Forwarded-") || strings.HasPrefix(name, appHeaderPrefix) {
			h.Del(name)
		}
	}
}

// forwardedProto reports the scheme of the CLIENT->app leg from the
// actual TLS state, not from any header.
func forwardedProto(r *http.Request) string {
	if r.TLS != nil {
		return "https"
	}
	return "http"
}

// proxyError maps outbound failures to plain client answers. Upstream
// error detail and relay configuration never enter the response; they
// go to the log.
func (r *Relay) proxyError(w http.ResponseWriter, req *http.Request, err error) {
	var maxErr *http.MaxBytesError
	if errors.As(err, &maxErr) {
		i18n.HTTPError(w, req, http.StatusRequestEntityTooLarge, i18n.StatusPhrase(http.StatusRequestEntityTooLarge))
		return
	}
	if errors.Is(err, errSwitchRefused) {
		i18n.HTTPError(w, req, http.StatusBadGateway, reasonSwitchRefused)
		return
	}
	if errors.Is(err, context.DeadlineExceeded) || isTimeoutError(err) {
		// The method is request-borne: percent-encode control bytes the
		// way every other log sink scrubs them, so a forged method
		// cannot paint a forged line into the operator's tail.
		r.logger.Warn("relay: upstream timeout", "method", url.PathEscape(req.Method), "path", req.URL.EscapedPath(), "err", err.Error())
		i18n.HTTPError(w, req, http.StatusGatewayTimeout, reasonUpstreamTimeout)
		return
	}
	r.logger.Warn("relay: upstream unreachable", "method", url.PathEscape(req.Method), "path", req.URL.EscapedPath(), "err", err.Error())
	i18n.HTTPError(w, req, http.StatusBadGateway, reasonUpstreamUnavailable)
}

func isTimeoutError(err error) bool {
	var ne net.Error
	return errors.As(err, &ne) && ne.Timeout()
}

// modifyResponse applies the response-side contract: refuse redirects
// that would leak the upstream origin, strip vendor identity/CORS
// headers, pin nosniff, and force no-store unless the route opted into
// upstream cache headers.
func (r *Relay) modifyResponse(rt *routeRuntime, resp *http.Response) {
	// Redirect-following is off by design (ReverseProxy does not
	// follow), and a Location header must never LEAK either: an
	// absolute or protocol-relative Location hands the client the
	// vendor origin, defeating the first-party posture, and a relative
	// one resolves against the mount path into a guaranteed 404. A 3xx
	// carrying Location is replaced wholesale with a plain 502;
	// Location-less 3xx (304 Not Modified) passes through untouched.
	if resp.StatusCode >= 300 && resp.StatusCode < 400 && resp.Header.Get("Location") != "" {
		r.logger.Warn("relay: refused upstream redirect",
			"status", resp.StatusCode,
			"location", resp.Header.Get("Location"),
			"upstream", rt.upstream.Host)
		// Drain-and-close the discarded upstream body: ReverseProxy only
		// closes the body present when ModifyResponse returns, so leaving
		// the original open would leak one connection per refused
		// redirect. The bounded drain lets the transport reuse the conn.
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4<<10))
		_ = resp.Body.Close()
		body := "upstream redirect refused\n"
		resp.StatusCode = http.StatusBadGateway
		resp.Status = http.StatusText(http.StatusBadGateway)
		resp.Body = io.NopCloser(strings.NewReader(body))
		resp.ContentLength = int64(len(body))
		resp.Header = http.Header{}
	}

	for _, name := range stripOutboundHeaders {
		resp.Header.Del(name)
	}
	for name := range resp.Header {
		if strings.HasPrefix(name, "Access-Control-") {
			resp.Header.Del(name)
		}
	}
	stripAbsoluteLinkTargets(resp.Header)
	resp.Header.Set("X-Content-Type-Options", "nosniff")
	if !rt.cacheOK {
		resp.Header.Set("Cache-Control", "no-store")
	}

	// Bound the response direction with the same brake the request
	// direction carries. MaxBodyBytes capped req.Body only, and
	// ReverseProxy streams the upstream's body straight through, so a
	// vendor endpoint that never ends its response held a goroutine, a
	// socket pair, and bandwidth for the full request deadline -- per
	// open client request, at no cost to the vendor. "Egress is your cost
	// now" is the relay's own warning about this direction, and nothing
	// enforced it.
	resp.Body = &cappedBody{ReadCloser: resp.Body, remaining: rt.maxBody}
}

// refererOrigin reduces a Referer to its origin with a trailing slash,
// the form a browser's strict-origin-when-cross-origin policy sends to
// another origin. Anything that is not an absolute http(s) URL with a
// host comes back "" and is not forwarded.
func refererOrigin(ref string) string {
	if ref == "" {
		return ""
	}
	u, err := url.Parse(ref)
	if err != nil || u.Host == "" {
		return ""
	}
	scheme := strings.ToLower(u.Scheme)
	if scheme != "http" && scheme != "https" {
		return ""
	}
	return scheme + "://" + u.Host + "/"
}

// stripAbsoluteLinkTargets drops Link header values that name another
// origin. A relative Link target resolves against the app origin (the
// mount), so pagination hints like `</page/2>; rel="next"` stay
// forwarded; one on another origin — typically a vendor `rel=preload`
// or `rel=preconnect` — hands the browser a direct connection to a
// third party, re-opening the exact channel the first-party posture
// exists to close. A value holding any such target is dropped whole:
// conservative beats leaking.
func stripAbsoluteLinkTargets(h http.Header) {
	values := h.Values("Link")
	if len(values) == 0 {
		return
	}
	kept := make([]string, 0, len(values))
	for _, v := range values {
		if linkValueSameOrigin(v) {
			kept = append(kept, v)
		}
	}
	h.Del("Link")
	for _, v := range kept {
		h.Add("Link", v)
	}
}

// linkValueSameOrigin reports whether every <target> in a Link field
// value resolves on the app's own origin. A value with an unclosed "<"
// is refused.
func linkValueSameOrigin(v string) bool {
	for {
		i := strings.IndexByte(v, '<')
		if i < 0 {
			return true
		}
		j := strings.IndexByte(v[i+1:], '>')
		if j < 0 {
			return false
		}
		if !linkTargetSameOrigin(v[i+1 : i+1+j]) {
			return false
		}
		v = v[i+1+j+1:]
	}
}

// linkTargetSameOrigin reads a Link target the way a browser's URL
// parser does, not as written: it drops every tab and newline, trims
// leading and trailing C0 controls and spaces, and reads "\" as "/" (an
// http(s) base makes both separators), so <\\vendor.example/x> and
// </<TAB>/vendor.example/x> are the scheme-relative //vendor.example/x
// they parse as. A target with a scheme or an authority is another
// origin as far as the relay is concerned.
func linkTargetSameOrigin(target string) bool {
	t := strings.Map(func(r rune) rune {
		switch r {
		case '\t', '\n', '\r':
			return -1
		case '\\':
			return '/'
		}
		return r
	}, target)
	t = strings.TrimFunc(t, func(r rune) bool { return r <= 0x20 })
	if strings.HasPrefix(t, "//") {
		return false
	}
	// A scheme ends at the first ":" ahead of any "/", "?" or "#".
	if end := strings.IndexAny(t, "/?#"); end >= 0 {
		t = t[:end]
	}
	return !strings.Contains(t, ":")
}

// errUpstreamTooLarge ends a response whose upstream body ran past the
// route's budget.
var errUpstreamTooLarge = errors.New("relay: upstream response exceeds MaxBodyBytes")

// cappedBody stops a response body at a byte budget. The client already
// has the status and headers by then, so the cut cannot be a status; it
// is a read error instead, which makes ReverseProxy abort the stream.
// An io.EOF here ended a chunked response cleanly, and the client (or a
// cache, under CacheOK) took the cut-off body for the whole one. A body
// that ends exactly at the budget is whole and ends with io.EOF.
type cappedBody struct {
	io.ReadCloser
	remaining int64
}

func (c *cappedBody) Read(p []byte) (int, error) {
	if c.remaining <= 0 {
		var probe [1]byte
		for {
			n, err := c.ReadCloser.Read(probe[:])
			switch {
			case n > 0:
				return 0, errUpstreamTooLarge
			case err != nil:
				return 0, err
			}
		}
	}
	if int64(len(p)) > c.remaining {
		p = p[:c.remaining]
	}
	n, err := c.ReadCloser.Read(p)
	c.remaining -= int64(n)
	return n, err
}

// loggerBeforeInit is the pre-Init fallback so a Relay constructed but
// not yet Init'ed (unit tests driving handlers directly) never nil-d.
func discardLogger() *slog.Logger { return slog.New(slog.DiscardHandler) }
