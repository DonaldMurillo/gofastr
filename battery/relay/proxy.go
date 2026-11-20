package relay

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"maps"
	"mime"
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

const requestDeadline = 30 * time.Second

const appHeaderPrefix = "X-Gofastr-"

var stripInboundHeaders = []string{
	http.CanonicalHeaderKey("Cookie"),
	http.CanonicalHeaderKey("Authorization"),
	http.CanonicalHeaderKey("Proxy-Authorization"),
	http.CanonicalHeaderKey("X-CSRF-Token"),
	http.CanonicalHeaderKey("X-API-Key"),
	http.CanonicalHeaderKey("X-Real-IP"),
	http.CanonicalHeaderKey("X-Client-IP"),
	http.CanonicalHeaderKey("True-Client-IP"),
	http.CanonicalHeaderKey("CF-Connecting-IP"),
	http.CanonicalHeaderKey("Fastly-Client-IP"),
	http.CanonicalHeaderKey("Forwarded"),
}

var stripOutboundHeaders = []string{
	http.CanonicalHeaderKey("Set-Cookie"),
	http.CanonicalHeaderKey("WWW-Authenticate"),
	http.CanonicalHeaderKey("Proxy-Authenticate"),
	http.CanonicalHeaderKey("Refresh"),
	http.CanonicalHeaderKey("Clear-Site-Data"),
	http.CanonicalHeaderKey("Service-Worker-Allowed"),
	http.CanonicalHeaderKey("NEL"),
	http.CanonicalHeaderKey("Report-To"),
	http.CanonicalHeaderKey("Reporting-Endpoints"),
	http.CanonicalHeaderKey("Alt-Svc"),
	http.CanonicalHeaderKey("Accept-CH"),
	http.CanonicalHeaderKey("Critical-CH"),
	http.CanonicalHeaderKey("Strict-Transport-Security"),
	http.CanonicalHeaderKey("Set-Login"),
}

type reverseProxy = httputil.ReverseProxy

func newTransport() *http.Transport {
	dialer := &net.Dialer{
		Timeout:   5 * time.Second,
		KeepAlive: 30 * time.Second,
	}
	return &http.Transport{
		DialContext:           guardedDialContext(net.DefaultResolver.LookupIPAddr, dialer.DialContext),
		MaxIdleConns:          100,
		MaxIdleConnsPerHost:   16,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   5 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
		ResponseHeaderTimeout: 10 * time.Second,
		ForceAttemptHTTP2:     true,
		DisableCompression:    true,
	}
}

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

// Unwrap lets http.ResponseController reach the client writer's Flush and
// deadlines.
func (w *hostHeaderWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }

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
			out.Header.Del("Connection")
			out.Header.Del("Upgrade")
			if ref := refererOrigin(in.Header.Get("Referer")); ref != "" {
				out.Header.Set("Referer", ref)
			} else {
				out.Header.Del("Referer")
			}
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

var errSwitchRefused = errors.New("relay: upstream protocol switch refused")

func (r *Relay) refuseSwitch(rt *routeRuntime, resp *http.Response) error {
	r.logger.Warn("relay: refused upstream protocol switch",
		"upgrade", url.PathEscape(resp.Header.Get("Upgrade")),
		"upstream", rt.upstream.Host)
	return errSwitchRefused
}

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

func forwardedProto(r *http.Request) string {
	if r.TLS != nil {
		return "https"
	}
	return "http"
}

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

func (r *Relay) modifyResponse(rt *routeRuntime, resp *http.Response) {
	if resp.StatusCode >= 300 && resp.StatusCode < 400 && resp.Header.Get("Location") != "" {
		r.logger.Warn("relay: refused upstream redirect",
			"status", resp.StatusCode,
			"location", resp.Header.Get("Location"),
			"upstream", rt.upstream.Host)
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4<<10))
		_ = resp.Body.Close()
		body := "upstream redirect refused\n"
		resp.StatusCode = http.StatusBadGateway
		resp.Status = http.StatusText(http.StatusBadGateway)
		resp.Body = io.NopCloser(strings.NewReader(body))
		resp.ContentLength = int64(len(body))
		resp.Header = http.Header{"Content-Type": {"text/plain; charset=utf-8"}}
	}
	r.restrictContentType(rt, resp)

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

	resp.Body = &cappedBody{ReadCloser: resp.Body, remaining: rt.maxBody}
}

func (r *Relay) restrictContentType(rt *routeRuntime, resp *http.Response) {
	ct := resp.Header.Get("Content-Type")
	if ct == "" {
		if resp.StatusCode != http.StatusNoContent && resp.StatusCode != http.StatusNotModified && resp.ContentLength != 0 {
			resp.Header.Set("Content-Type", "application/octet-stream")
		}
		return
	}
	mt, _, err := mime.ParseMediaType(ct)
	if err == nil && relayableType(rt, mt) {
		if mt == svgType && !rt.extraCT[svgType] {
			resp.Header.Add("Content-Security-Policy", "sandbox")
		}
		return
	}
	if bodiless(resp) {
		return
	}
	r.logger.Warn("relay: refused upstream content type",
		"status", resp.StatusCode,
		"content_type", url.PathEscape(ct),
		"upstream", rt.upstream.Host)
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4<<10))
	_ = resp.Body.Close()
	body := "upstream content type refused\n"
	resp.StatusCode = http.StatusBadGateway
	resp.Status = http.StatusText(http.StatusBadGateway)
	resp.Body = io.NopCloser(strings.NewReader(body))
	resp.ContentLength = int64(len(body))
	resp.Header = http.Header{"Content-Type": {"text/plain; charset=utf-8"}}
}

func bodiless(resp *http.Response) bool {
	if resp.StatusCode == http.StatusNoContent || resp.StatusCode == http.StatusNotModified ||
		resp.ContentLength == 0 || resp.Body == nil || resp.Body == http.NoBody ||
		(resp.Request != nil && resp.Request.Method == http.MethodHead) {
		return true
	}
	if resp.ContentLength > 0 {
		return false
	}
	var one [1]byte
	n, err := io.ReadFull(resp.Body, one[:])
	if n == 0 && err == io.EOF {
		return true
	}
	resp.Body = struct {
		io.Reader
		io.Closer
	}{io.MultiReader(bytes.NewReader(one[:n]), resp.Body), resp.Body}
	return false
}

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
	if end := strings.IndexAny(t, "/?#"); end >= 0 {
		t = t[:end]
	}
	return !strings.Contains(t, ":")
}

var errUpstreamTooLarge = errors.New("relay: upstream response exceeds MaxBodyBytes")

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

func discardLogger() *slog.Logger { return slog.New(slog.DiscardHandler) }
