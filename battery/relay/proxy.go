package relay

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
	"time"
)

// requestDeadline bounds one proxied request end to end. The effective
// deadline is min(inherited request context, 30s) so an app-level
// timeout or an impatient client can always cut a stuck upstream off.
const requestDeadline = 30 * time.Second

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
}

// stripOutboundHeaders never reach the client. Set-Cookie and the
// authenticate challenges are the vendor trying to establish identity
// the relay deliberately withholds; Access-Control-* would make the
// app origin advertise a third party's CORS policy.
var stripOutboundHeaders = []string{
	http.CanonicalHeaderKey("Set-Cookie"),
	http.CanonicalHeaderKey("WWW-Authenticate"),
	http.CanonicalHeaderKey("Proxy-Authenticate"),
}

// reverseProxy keeps the concrete type in one place; the handler layer
// speaks http.Handler.
type reverseProxy = httputil.ReverseProxy

// newTransport builds the shared outbound transport for all routes.
// One transport, one connection pool: per-route pools would multiply
// idle sockets by the number of vendors for no benefit.
func newTransport() *http.Transport {
	return &http.Transport{
		Proxy: http.ProxyFromEnvironment,
		DialContext: (&net.Dialer{
			Timeout:   5 * time.Second,
			KeepAlive: 30 * time.Second,
		}).DialContext,
		MaxIdleConns:          100,
		MaxIdleConnsPerHost:   16,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   5 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
		ResponseHeaderTimeout: 10 * time.Second,
		ForceAttemptHTTP2:     true,
	}
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
				http.Error(w, "bad request", http.StatusBadRequest)
				return
			}
		}
		// Declared-length bodies are refused before anything is read;
		// chunked bodies trip the reader mid-copy and surface through
		// the proxy's error handler, which maps them to 413 too.
		if req.ContentLength > rt.maxBody {
			http.Error(w, "request body too large", http.StatusRequestEntityTooLarge)
			return
		}
		req.Body = http.MaxBytesReader(w, req.Body, rt.maxBody)

		ctx, cancel := context.WithTimeout(req.Context(), requestDeadline)
		defer cancel()
		req = req.WithContext(ctx)

		rt.proxy.ServeHTTP(w, req)
	})
}

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
			// Forwarded metadata is derived from the connection, never
			// relayed from the client.
			out.Header.Set("X-Forwarded-For", r.clientIP(in))
			out.Header.Set("X-Forwarded-Proto", forwardedProto(in))
		},
		ErrorHandler:   r.proxyError,
		ModifyResponse: func(resp *http.Response) error { r.modifyResponse(rt, resp); return nil },
	}
}

// stripInbound removes credentials and spoofable forwarding metadata
// from the outbound header set. ReverseProxy itself removes hop-by-hop
// headers; this is the application-layer denylist on top.
func stripInbound(h http.Header) {
	for _, name := range stripInboundHeaders {
		h.Del(name)
	}
	for name := range h {
		if strings.HasPrefix(name, "X-Forwarded-") {
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
		http.Error(w, "request body too large", http.StatusRequestEntityTooLarge)
		return
	}
	if errors.Is(err, context.DeadlineExceeded) || isTimeoutError(err) {
		r.logger.Warn("relay: upstream timeout", "method", req.Method, "path", req.URL.EscapedPath(), "err", err.Error())
		http.Error(w, "upstream timeout", http.StatusGatewayTimeout)
		return
	}
	r.logger.Warn("relay: upstream unreachable", "method", req.Method, "path", req.URL.EscapedPath(), "err", err.Error())
	http.Error(w, "upstream unavailable", http.StatusBadGateway)
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
	resp.Header.Set("X-Content-Type-Options", "nosniff")
	if !rt.cacheOK {
		resp.Header.Set("Cache-Control", "no-store")
	}
}

// loggerBeforeInit is the pre-Init fallback so a Relay constructed but
// not yet Init'ed (unit tests driving handlers directly) never nil-d.
func discardLogger() *slog.Logger { return slog.New(slog.DiscardHandler) }
