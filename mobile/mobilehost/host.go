// Package mobilehost runs a GoFastr app inside a phone app's process and
// serves the WebView's requests in-process: no listener socket, no port,
// no loopback origin. PROTOTYPE.
//
// The native side (a WKURLSchemeHandler on iOS, a WebViewClient's
// shouldInterceptRequest on Android) hands each WebView request to
// Host.Do (or Host.Open for a streamed body such as SSE) and writes the
// answer back to the WebView. Underneath, the request travels through the
// app's real net/http server over an in-memory net.Pipe
// (framework.App.Serve on a memListener), so every middleware, timeout,
// stream and shutdown path is the one a deployed server runs.
package mobilehost

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/DonaldMurillo/gofastr/framework"
)

// DefaultOrigin is the origin the WebView loads the app under. Android's
// WebViewAssetLoader convention reserves this https host for in-app
// content, so the page is a secure context and the host never resolves on
// the network. iOS cannot intercept https (WKURLSchemeHandler refuses
// http/https), so the iOS shell uses a custom scheme (see shells/ios) and
// passes its own Origin.
const DefaultOrigin = "https://appassets.androidplatform.net"

// maxBufferedBody caps Do's response body. A larger answer is a streamed
// one and belongs on Open.
const maxBufferedBody = 64 << 20

// Config configures a Host.
type Config struct {
	// DataDir is the absolute app sandbox directory holding the identity
	// file (and, through Options, the database and secret).
	DataDir string
	// Origin is the scheme://host the WebView loads; DefaultOrigin when
	// empty. Requests for any other origin are refused.
	Origin string
	// StartTimeout bounds Start; 30s when zero.
	StartTimeout time.Duration
}

// Host owns one in-process app.
type Host struct {
	cfg    Config
	origin *url.URL
	app    *framework.App
	ln     *memListener
	client *http.Client
	jar    http.CookieJar

	user             *localUser
	authOwnsIdentity bool

	mu      sync.Mutex
	started bool
	ready   chan struct{}
	done    chan error
}

// New prepares a Host for app and registers it as the app's "mobile"
// battery (its Init installs the local identity). Call it after the app's
// UI host is mounted and before Start.
func New(app *framework.App, cfg Config) (*Host, error) {
	if cfg.Origin == "" {
		cfg.Origin = DefaultOrigin
	}
	if cfg.StartTimeout == 0 {
		cfg.StartTimeout = 30 * time.Second
	}
	origin, err := url.Parse(cfg.Origin)
	if err != nil || origin.Scheme == "" || origin.Host == "" || (origin.Path != "" && origin.Path != "/") {
		return nil, fmt.Errorf("mobilehost: Origin %q must be scheme://host", cfg.Origin)
	}
	if err := ensurePrivateDir(cfg.DataDir); err != nil {
		return nil, err
	}
	user, err := loadIdentity(cfg.DataDir)
	if err != nil {
		return nil, err
	}
	jar, err := cookiejar.New(nil)
	if err != nil {
		return nil, err
	}
	ln := newMemListener()
	h := &Host{
		cfg:    cfg,
		origin: origin,
		app:    app,
		ln:     ln,
		jar:    jar,
		user:   user,
		ready:  make(chan struct{}),
		done:   make(chan error, 1),
		//gofastr:allow(clienttimeout) the same client carries held SSE streams (Open), which a client-wide Timeout would cut; Do is bounded by its caller's context and a Stream by Close
		client: &http.Client{
			Transport: &http.Transport{
				DialContext: ln.dial,
				// The WebView negotiates its own encodings; pass bytes
				// through untouched.
				DisableCompression:  true,
				MaxIdleConnsPerHost: 8,
				IdleConnTimeout:     90 * time.Second,
			},
			// The WebView, not the host, decides what a redirect means:
			// hand 3xx back as-is. (See README: neither platform's
			// interception API can return a redirect to the WebView, so the
			// native shim follows it there.)
			CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
		},
	}
	app.RegisterBattery(h)
	return h, nil
}

// Name implements framework.Battery.
func (h *Host) Name() string { return "mobile" }

// Init implements framework.Battery: the identity middleware and the
// owner extractor, so owner-scoped entities answer the phone's one user.
func (h *Host) Init(app *framework.App) error {
	h.authOwnsIdentity = installExtractor()
	app.Use(h.identityMiddleware)
	app.OnReady(func(string) { close(h.ready) })
	return nil
}

// Start runs the app on the in-memory listener and returns once it is
// serving. Migrations, seeds and start hooks run here, so the native side
// should call it off the UI thread and show its launch screen meanwhile.
func (h *Host) Start() error {
	h.mu.Lock()
	if h.started {
		h.mu.Unlock()
		return errors.New("mobilehost: Start called twice")
	}
	h.started = true
	h.mu.Unlock()

	// The OS owns a phone app's lifecycle; there is no SIGTERM to drain on.
	h.app.Config.DisableSignalHandling = true
	go func() { h.done <- h.app.Serve(h.ln) }()
	select {
	case <-h.ready:
		return nil
	case err := <-h.done:
		if err == nil {
			err = errors.New("app stopped before it was ready")
		}
		return fmt.Errorf("mobilehost: start: %w", err)
	case <-time.After(h.cfg.StartTimeout):
		_ = h.Stop(context.Background())
		return fmt.Errorf("mobilehost: app not ready within %s", h.cfg.StartTimeout)
	}
}

// Stop drains the app (OnStop hooks, batteries, open streams) and waits
// for Serve to return.
func (h *Host) Stop(ctx context.Context) error {
	err := h.app.Shutdown(ctx)
	select {
	case serveErr := <-h.done:
		if err == nil {
			err = serveErr
		}
	case <-ctx.Done():
	}
	return err
}

// Request is one WebView request.
type Request struct {
	Method string
	// URL is absolute, under the Host's origin.
	URL    string
	Header http.Header
	Body   []byte
}

// Response is a buffered answer.
type Response struct {
	Status int
	// Header carries no Set-Cookie: cookies stay in the host's jar (see
	// roundTrip).
	Header http.Header
	Body   []byte
}

// Do serves req and buffers the whole answer.
func (h *Host) Do(ctx context.Context, req *Request) (*Response, error) {
	resp, err := h.roundTrip(ctx, req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBufferedBody+1))
	if err != nil {
		return nil, fmt.Errorf("mobilehost: read response: %w", err)
	}
	if len(body) > maxBufferedBody {
		return nil, fmt.Errorf("mobilehost: response over %d bytes; use Open", maxBufferedBody)
	}
	return &Response{Status: resp.StatusCode, Header: resp.Header, Body: body}, nil
}

// Stream is an answer whose body the native side pulls as it arrives:
// an SSE stream, a download, anything not worth buffering.
type Stream struct {
	Status int
	Header http.Header
	body   io.ReadCloser
}

// Read returns up to max bytes of the body, blocking until some arrive.
// It returns io.EOF at the end.
func (s *Stream) Read(max int) ([]byte, error) {
	if max <= 0 {
		max = 32 << 10
	}
	buf := make([]byte, max)
	n, err := s.body.Read(buf)
	return buf[:n], err
}

// Close ends the stream; the server sees the request context cancelled.
func (s *Stream) Close() error { return s.body.Close() }

// Open serves req and returns as soon as the response headers are in.
func (h *Host) Open(ctx context.Context, req *Request) (*Stream, error) {
	resp, err := h.roundTrip(ctx, req)
	if err != nil {
		return nil, err
	}
	return &Stream{Status: resp.StatusCode, Header: resp.Header, body: resp.Body}, nil
}

// roundTrip forwards req over the in-memory transport.
//
// Cookies: neither platform lets an intercepted response set cookies in
// the WebView (WKURLSchemeHandler responses never reach WKHTTPCookieStore;
// Android ignores Set-Cookie on a WebResourceResponse), and neither
// reliably hands the WebView's cookies to the interceptor. So the host
// keeps the cookie jar itself, keyed by the WebView-visible URL: it adds
// the jar's cookies to every request and moves every Set-Cookie into the
// jar. The page's JavaScript therefore never sees these cookies (see
// README: the runtime's serverAction reads one from document.cookie).
func (h *Host) roundTrip(ctx context.Context, req *Request) (*http.Response, error) {
	if req == nil {
		return nil, errors.New("mobilehost: nil request")
	}
	ext, err := url.Parse(req.URL)
	if err != nil {
		return nil, fmt.Errorf("mobilehost: bad URL: %w", err)
	}
	if !strings.EqualFold(ext.Scheme, h.origin.Scheme) || !strings.EqualFold(ext.Host, h.origin.Host) {
		return nil, fmt.Errorf("mobilehost: %s is not under the app origin %s", ext.Redacted(), h.origin)
	}
	method := req.Method
	if method == "" {
		method = http.MethodGet
	}
	// The pipe speaks plain HTTP/1.1; the URL only names the path. Host
	// stays the WebView-visible host so absolute URLs the app builds and
	// same-origin checks agree with what the page sees.
	inner := &url.URL{Scheme: "http", Host: h.origin.Host, Path: ext.Path, RawPath: ext.RawPath, RawQuery: ext.RawQuery}
	var body io.Reader
	if len(req.Body) > 0 {
		body = bytes.NewReader(req.Body)
	}
	hr, err := http.NewRequestWithContext(ctx, method, inner.String(), body)
	if err != nil {
		return nil, fmt.Errorf("mobilehost: build request: %w", err)
	}
	for k, vs := range req.Header {
		if hopByHop(k) {
			continue
		}
		for _, v := range vs {
			hr.Header.Add(k, v)
		}
	}
	// The page is a secure context (https on Android; a custom scheme the
	// shell serves on iOS), but the pipe carries no TLS, so the app would
	// read every request as plaintext and warn that its Secure session
	// cookie will be dropped. Only this transport can reach the server,
	// so the forwarded scheme is trustworthy here; the WebView's own
	// value, if any, is replaced.
	hr.Header.Set("X-Forwarded-Proto", "https")
	jarURL := h.jarURL(ext)
	for _, c := range h.jar.Cookies(jarURL) {
		hr.AddCookie(c)
	}
	resp, err := h.client.Do(hr)
	if err != nil {
		return nil, fmt.Errorf("mobilehost: %w", err)
	}
	if cs := resp.Cookies(); len(cs) > 0 {
		h.jar.SetCookies(jarURL, cs)
	}
	resp.Header.Del("Set-Cookie")
	return resp, nil
}

// jarURL keys the cookie jar. It is always https: net/http/cookiejar
// sends a Secure cookie only to an https URL, and the iOS shell's custom
// scheme is as private as https (nothing outside the app can load it).
func (h *Host) jarURL(ext *url.URL) *url.URL {
	return &url.URL{Scheme: "https", Host: h.origin.Host, Path: ext.Path}
}

func hopByHop(k string) bool {
	switch http.CanonicalHeaderKey(k) {
	case "Connection", "Keep-Alive", "Proxy-Connection", "Transfer-Encoding", "Upgrade", "Te", "Trailer", "Host", "Content-Length":
		return true
	}
	return false
}
