package relay

import (
	"bytes"
	"compress/gzip"
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/framework"
)

// captured records everything a fake upstream saw.
type captured struct {
	mu     sync.Mutex
	hits   int
	method string
	uri    string // r.URL.RequestURI (path + query)
	host   string // r.Host the request was addressed to
	body   []byte
	header http.Header
}

func (c *captured) record(w http.ResponseWriter, r *http.Request) {
	b, _ := io.ReadAll(r.Body)
	c.mu.Lock()
	defer c.mu.Unlock()
	c.hits++
	c.method = r.Method
	c.uri = r.URL.RequestURI()
	c.host = r.Host
	c.body = b
	c.header = r.Header.Clone()
	w.WriteHeader(http.StatusOK)
}

func (c *captured) count() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.hits
}

func (c *captured) snapshot() (method, uri, host string, body []byte, header http.Header) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.method, c.uri, c.host, c.body, c.header
}

// startRelay wires a Relay through a real framework App, serves the
// App's router, and returns the client-facing base URL. build receives
// the fake upstream's base URL (mounted under /cdn) so configs can
// point routes at it.
func startRelay(t *testing.T, build func(upstreamBase string) Config, upstream http.HandlerFunc) string {
	t.Helper()
	up := httptest.NewServer(upstream)
	t.Cleanup(up.Close)

	cfg := build(up.URL + "/cdn")
	app := framework.NewApp(framework.WithoutDefaultMiddleware())
	app.RegisterPlugin(New(cfg))
	if err := app.InitPlugins(); err != nil {
		t.Fatalf("InitPlugins: %v", err)
	}
	srv := httptest.NewServer(app.Router())
	t.Cleanup(srv.Close)
	return srv.URL
}

func startRelayCap(t *testing.T, build func(upstreamBase string) Config) (clientBase string, cap *captured) {
	t.Helper()
	cap = &captured{}
	base := startRelay(t, build, http.HandlerFunc(cap.record))
	return base, cap
}

func defaultCfg(upstreamBase string) Config {
	return Config{
		Routes: []Route{{
			Prefix:   "e/",
			Upstream: upstreamBase,
			Methods:  []string{http.MethodGet, http.MethodPost},
		}},
	}
}

func TestSubtreeForwardsPathAndQuery(t *testing.T) {
	base, cap := startRelayCap(t, defaultCfg)

	res, err := http.Get(base + "/__gofastr/t/e/lib/app.js?v=2")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", res.StatusCode)
	}
	method, uri, _, _, _ := cap.snapshot()
	if method != http.MethodGet {
		t.Fatalf("upstream method = %q", method)
	}
	if uri != "/cdn/lib/app.js?v=2" {
		t.Fatalf("upstream uri = %q, want /cdn/lib/app.js?v=2", uri)
	}
}

func TestExactRouteMatchesOnlyItsPath(t *testing.T) {
	base, cap := startRelayCap(t, func(up string) Config {
		return Config{Routes: []Route{{
			Prefix:   "ping",
			Upstream: up,
			Methods:  []string{http.MethodGet},
		}}}
	})

	res, err := http.Get(base + "/__gofastr/t/ping")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	res.Body.Close()
	if res.StatusCode != http.StatusOK || cap.count() != 1 {
		t.Fatalf("exact hit: status=%d hits=%d", res.StatusCode, cap.count())
	}

	res, err = http.Get(base + "/__gofastr/t/ping/extra")
	if err != nil {
		t.Fatalf("get extra: %v", err)
	}
	res.Body.Close()
	if res.StatusCode != http.StatusNotFound || cap.count() != 1 {
		t.Fatalf("subpath of exact route: status=%d hits=%d, want 404 and no extra upstream hit", res.StatusCode, cap.count())
	}
}

func TestUnknownTailReturns404(t *testing.T) {
	base, cap := startRelayCap(t, defaultCfg)

	res, err := http.Get(base + "/__gofastr/t/nosuch/deep.js")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	res.Body.Close()
	if res.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", res.StatusCode)
	}
	if cap.count() != 0 {
		t.Fatalf("upstream hit %d times for unknown tail", cap.count())
	}
}

func TestUndeclaredMethodGives405Allow(t *testing.T) {
	base, cap := startRelayCap(t, defaultCfg)

	req, _ := http.NewRequest(http.MethodDelete, base+"/__gofastr/t/e/x", nil)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("delete: %v", err)
	}
	res.Body.Close()
	if res.StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want 405", res.StatusCode)
	}
	allow := res.Header.Get("Allow")
	if !strings.Contains(allow, "GET") || !strings.Contains(allow, "POST") {
		t.Fatalf("Allow = %q, want GET and POST", allow)
	}
	if cap.count() != 0 {
		t.Fatalf("upstream hit %d times for undeclared method", cap.count())
	}
}

func TestInboundCredentialsStripped(t *testing.T) {
	base, cap := startRelayCap(t, defaultCfg)

	req, _ := http.NewRequest(http.MethodPost, base+"/__gofastr/t/e/ev", strings.NewReader(`{"a":1}`))
	req.Header.Set("Cookie", "session=secret")
	req.Header.Set("Authorization", "Bearer app-secret")
	req.Header.Set("Proxy-Authorization", "Basic zzz")
	req.Header.Set("X-CSRF-Token", "csrf-token")
	req.Header.Set("X-API-Key", "app-api-key")
	req.Header.Set("X-Forwarded-For", "1.2.3.4, 5.6.7.8")
	req.Header.Set("X-Forwarded-Host", "evil.example")
	req.Header.Set("X-Forwarded-Proto", "https")
	req.Header.Set("X-Forwarded-Custom", "spoof")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	res.Body.Close()

	_, _, _, _, h := cap.snapshot()
	for _, name := range []string{
		"Cookie", "Authorization", "Proxy-Authorization",
		"X-Csrf-Token", "X-Api-Key",
	} {
		if got := h.Get(name); got != "" {
			t.Errorf("%s reached upstream: %q", name, got)
		}
	}
	// Inbound-only X-Forwarded-* names are dropped outright; the two
	// derived ones are asserted by value below.
	for _, name := range []string{"X-Forwarded-Host", "X-Forwarded-Custom"} {
		if got := h.Get(name); got != "" {
			t.Errorf("inbound %s reached upstream: %q", name, got)
		}
	}
	// X-Forwarded-For must be the connection's actual peer, never the
	// spoofed inbound value.
	if got := h.Get("X-Forwarded-For"); got == "1.2.3.4, 5.6.7.8" || got == "" {
		t.Fatalf("X-Forwarded-For = %q, want the real peer address", got)
	}
	if got := h.Get("X-Forwarded-Proto"); got != "http" {
		t.Fatalf("X-Forwarded-Proto = %q, want http on a plain leg", got)
	}
}

func TestForwardedProtoHTTPSOnTLSLeg(t *testing.T) {
	cap := &captured{}
	up := httptest.NewServer(http.HandlerFunc(cap.record))
	t.Cleanup(up.Close)

	app := framework.NewApp(framework.WithoutDefaultMiddleware())
	app.RegisterPlugin(New(defaultCfg(up.URL + "/cdn")))
	if err := app.InitPlugins(); err != nil {
		t.Fatalf("InitPlugins: %v", err)
	}
	srv := httptest.NewTLSServer(app.Router())
	t.Cleanup(srv.Close)
	client := srv.Client()

	res, err := client.Get(srv.URL + "/__gofastr/t/e/x")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	res.Body.Close()
	_, _, _, _, h := cap.snapshot()
	if got := h.Get("X-Forwarded-Proto"); got != "https" {
		t.Fatalf("X-Forwarded-Proto = %q, want https behind TLS", got)
	}
}

func TestClientIPOverride(t *testing.T) {
	base, cap := startRelayCap(t, func(up string) Config {
		cfg := defaultCfg(up)
		cfg.ClientIP = func(r *http.Request) string { return "203.0.113.9" }
		return cfg
	})

	res, err := http.Get(base + "/__gofastr/t/e/x")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	res.Body.Close()
	_, _, _, _, h := cap.snapshot()
	if got := h.Get("X-Forwarded-For"); got != "203.0.113.9" {
		t.Fatalf("X-Forwarded-For = %q, want 203.0.113.9", got)
	}
}

func TestPreservedHeadersReachUpstream(t *testing.T) {
	base, cap := startRelayCap(t, defaultCfg)

	req, _ := http.NewRequest(http.MethodPost, base+"/__gofastr/t/e/ev", strings.NewReader(`{}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Content-Encoding", "gzip")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Accept-Encoding", "gzip")
	req.Header.Set("User-Agent", "VendorSDK/1.2")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	res.Body.Close()

	_, _, _, _, h := cap.snapshot()
	for name, want := range map[string]string{
		"Content-Type":     "application/json",
		"Content-Encoding": "gzip",
		"Accept":           "application/json",
		"Accept-Encoding":  "gzip",
		"User-Agent":       "VendorSDK/1.2",
	} {
		if got := h.Get(name); got != want {
			t.Errorf("%s = %q, want %q", name, got, want)
		}
	}
}

func TestOutboundAuthHeadersStripped(t *testing.T) {
	base := startRelay(t, defaultCfg, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Set-Cookie", "vendor=1; Path=/")
		w.Header().Set("WWW-Authenticate", `Basic realm="vendor"`)
		w.Header().Set("Proxy-Authenticate", `Basic realm="vendor"`)
		w.Header().Set("Access-Control-Allow-Origin", "https://app.example.com")
		w.Header().Set("Access-Control-Allow-Credentials", "true")
		w.Header().Set("X-Vendor-Extra", "keep-me")
		w.WriteHeader(http.StatusOK)
	}))

	res, err := http.Get(base + "/__gofastr/t/e/x")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	res.Body.Close()

	for _, name := range []string{
		"Set-Cookie", "Www-Authenticate", "Proxy-Authenticate",
		"Access-Control-Allow-Origin", "Access-Control-Allow-Credentials",
	} {
		if got := res.Header.Get(name); got != "" {
			t.Errorf("%s leaked to client: %q", name, got)
		}
	}
	if got := res.Header.Get("X-Content-Type-Options"); got != "nosniff" {
		t.Errorf("X-Content-Type-Options = %q, want nosniff", got)
	}
	if got := res.Header.Get("X-Vendor-Extra"); got != "keep-me" {
		t.Errorf("X-Vendor-Extra = %q, want passthrough", got)
	}
}

func TestNoStoreForcedWhenCacheOKFalse(t *testing.T) {
	base := startRelay(t, defaultCfg, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "public, max-age=99999")
		w.WriteHeader(http.StatusOK)
	}))

	res, err := http.Get(base + "/__gofastr/t/e/x")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	res.Body.Close()
	if got := res.Header.Get("Cache-Control"); got != "no-store" {
		t.Fatalf("Cache-Control = %q, want no-store", got)
	}
}

func TestCacheOKTruePassesCacheHeaders(t *testing.T) {
	base := startRelay(t, func(up string) Config {
		cfg := defaultCfg(up)
		cfg.Routes[0].CacheOK = true
		return cfg
	}, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		w.WriteHeader(http.StatusOK)
	}))

	res, err := http.Get(base + "/__gofastr/t/e/x")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	res.Body.Close()
	if got := res.Header.Get("Cache-Control"); got != "public, max-age=31536000, immutable" {
		t.Fatalf("Cache-Control = %q, want upstream value", got)
	}
}

func TestBodyCapDeclaredLength413(t *testing.T) {
	base, cap := startRelayCap(t, func(up string) Config {
		cfg := defaultCfg(up)
		cfg.Routes[0].MaxBodyBytes = 32
		return cfg
	})

	body := bytes.Repeat([]byte("x"), 64)
	res, err := http.Post(base+"/__gofastr/t/e/ev", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	res.Body.Close()
	if res.StatusCode != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want 413", res.StatusCode)
	}
	if cap.count() != 0 {
		t.Fatalf("oversized declared body reached upstream (%d hits)", cap.count())
	}
}

func TestBodyCapChunked413(t *testing.T) {
	base, cap := startRelayCap(t, func(up string) Config {
		cfg := defaultCfg(up)
		cfg.Routes[0].MaxBodyBytes = 32
		return cfg
	})

	// unknownLenReader hides the length so the client sends chunked.
	req, _ := http.NewRequest(http.MethodPost, base+"/__gofastr/t/e/ev",
		unknownLenReader{r: bytes.NewReader(bytes.Repeat([]byte("x"), 64))})
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	res.Body.Close()
	if res.StatusCode != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want 413", res.StatusCode)
	}
	_, _, _, body, _ := cap.snapshot()
	if len(body) >= 64 {
		t.Fatalf("full chunked body reached upstream (%d bytes)", len(body))
	}
}

type unknownLenReader struct{ r io.Reader }

func (u unknownLenReader) Read(p []byte) (int, error) { return u.r.Read(p) }

func TestUpstreamRedirectRefused502(t *testing.T) {
	base := startRelay(t, defaultCfg, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Location", "https://vendor.example/elsewhere")
		w.WriteHeader(http.StatusFound)
	}))

	res, err := http.Get(base + "/__gofastr/t/e/x")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	res.Body.Close()
	if res.StatusCode != http.StatusBadGateway {
		t.Fatalf("status = %d, want 502", res.StatusCode)
	}
	if got := res.Header.Get("Location"); got != "" {
		t.Fatalf("Location leaked: %q", got)
	}
}

func TestNotModifiedPassesThrough(t *testing.T) {
	base := startRelay(t, defaultCfg, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotModified)
	}))

	res, err := http.Get(base + "/__gofastr/t/e/x")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	res.Body.Close()
	if res.StatusCode != http.StatusNotModified {
		t.Fatalf("status = %d, want 304", res.StatusCode)
	}
}

func TestGzipPassthroughBytesIdentical(t *testing.T) {
	var gz bytes.Buffer
	zw := gzip.NewWriter(&gz)
	zw.Write([]byte("compress me untouched"))
	zw.Close()
	want := gz.Bytes()

	base := startRelay(t, defaultCfg, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Encoding", "gzip")
		w.Header().Set("Content-Type", "text/plain")
		w.Write(want)
	}))

	req, _ := http.NewRequest(http.MethodGet, base+"/__gofastr/t/e/blob", nil)
	req.Header.Set("Accept-Encoding", "gzip")
	res, err := http.DefaultTransport.RoundTrip(req) // no transparent gunzip
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer res.Body.Close()
	got, _ := io.ReadAll(res.Body)
	if res.Header.Get("Content-Encoding") != "gzip" {
		t.Fatalf("Content-Encoding = %q, want gzip", res.Header.Get("Content-Encoding"))
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("gzip bytes not identical: got %d bytes, want %d", len(got), len(want))
	}
}

// TestDeadlineExceededGives504 drives the router directly with a
// recorder so the relay's own 504 is observable: the client's transport
// isn't racing to abort first. The relay's per-request deadline is
// min(inherited ctx, 30s); here the inherited ctx wins.
func TestDeadlineExceededGives504(t *testing.T) {
	release := make(chan struct{})
	defer close(release)
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-release
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(up.Close)

	app := framework.NewApp(framework.WithoutDefaultMiddleware())
	app.RegisterPlugin(New(defaultCfg(up.URL + "/cdn")))
	if err := app.InitPlugins(); err != nil {
		t.Fatalf("InitPlugins: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()
	req := httptest.NewRequest(http.MethodGet, "/__gofastr/t/e/x", nil).WithContext(ctx)
	rec := httptest.NewRecorder()
	app.Router().ServeHTTP(rec, req)

	if rec.Code != http.StatusGatewayTimeout {
		t.Fatalf("status = %d, want 504", rec.Code)
	}
}

func TestTransportFailureGives502(t *testing.T) {
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	cfg := defaultCfg(up.URL + "/cdn")
	up.Close() // connection refused from here on

	app := framework.NewApp(framework.WithoutDefaultMiddleware())
	app.RegisterPlugin(New(cfg))
	if err := app.InitPlugins(); err != nil {
		t.Fatalf("InitPlugins: %v", err)
	}
	srv := httptest.NewServer(app.Router())
	t.Cleanup(srv.Close)

	res, err := http.Get(srv.URL + "/__gofastr/t/e/x")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	res.Body.Close()
	if res.StatusCode != http.StatusBadGateway {
		t.Fatalf("status = %d, want 502", res.StatusCode)
	}
}

func TestBaseReturnsMountPath(t *testing.T) {
	if got := New(Config{Routes: []Route{{Prefix: "e/", Upstream: "https://example.com", Methods: []string{"GET"}}}}).Base(); got != "/__gofastr/t" {
		t.Fatalf("default Base = %q, want /__gofastr/t", got)
	}
	if got := New(Config{Path: "/firstparty", Routes: []Route{{Prefix: "e/", Upstream: "https://example.com", Methods: []string{"GET"}}}}).Base(); got != "/firstparty" {
		t.Fatalf("custom Base = %q, want /firstparty", got)
	}
}

func TestRoutesRegisteredOnInit(t *testing.T) {
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	t.Cleanup(up.Close)

	app := framework.NewApp(framework.WithoutDefaultMiddleware())
	app.RegisterPlugin(New(Config{
		Routes: []Route{
			{Prefix: "e/", Upstream: up.URL, Methods: []string{http.MethodGet}},
			{Prefix: "ping", Upstream: up.URL, Methods: []string{http.MethodPost}},
		},
	}))
	if err := app.InitPlugins(); err != nil {
		t.Fatalf("InitPlugins: %v", err)
	}

	want := map[string]bool{
		"GET /__gofastr/t/e/{rest...}": false,
		"POST /__gofastr/t/ping":       false,
	}
	for _, rt := range app.Router().Routes() {
		key := rt.Method + " " + rt.Pattern
		if _, ok := want[key]; ok {
			want[key] = true
		}
	}
	for key, seen := range want {
		if !seen {
			t.Errorf("route %q not registered", key)
		}
	}
}

func TestShutdownClosesIdleTransportConns(t *testing.T) {
	var openConns atomic.Int64
	up := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	up.Config.ConnState = func(c net.Conn, cs http.ConnState) {
		switch cs {
		case http.StateNew:
			openConns.Add(1)
		case http.StateClosed, http.StateHijacked:
			openConns.Add(-1)
		}
	}
	up.Start()
	t.Cleanup(up.Close)

	app := framework.NewApp(framework.WithoutDefaultMiddleware())
	app.RegisterPlugin(New(defaultCfg(up.URL + "/cdn")))
	if err := app.InitPlugins(); err != nil {
		t.Fatalf("InitPlugins: %v", err)
	}
	srv := httptest.NewServer(app.Router())
	t.Cleanup(srv.Close)

	res, err := http.Get(srv.URL + "/__gofastr/t/e/x")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	res.Body.Close()

	// One keep-alive conn to the upstream should now be idle on the
	// relay's shared transport.
	deadline := time.Now().Add(2 * time.Second)
	for openConns.Load() != 1 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if got := openConns.Load(); got != 1 {
		t.Fatalf("idle upstream conns = %d, want 1 before shutdown", got)
	}

	if err := app.Shutdown(context.Background()); err != nil {
		t.Fatalf("Shutdown: %v", err)
	}
	deadline = time.Now().Add(2 * time.Second)
	for openConns.Load() != 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if got := openConns.Load(); got != 0 {
		t.Fatalf("open upstream conns after Shutdown = %d, want 0 (CloseIdleConnections via OnStop)", got)
	}
}

// closeRecorder records whether the wrapped body was closed.
type closeRecorder struct {
	io.Reader
	closed bool
}

func (c *closeRecorder) Close() error { c.closed = true; return nil }

func TestRefusedRedirectClosesUpstreamBody(t *testing.T) {
	r := New(defaultCfg("http://127.0.0.1:1/base"))
	rt := r.routes[0]
	body := &closeRecorder{Reader: strings.NewReader("moved")}
	resp := &http.Response{
		StatusCode: http.StatusFound,
		Header:     http.Header{"Location": []string{"https://vendor.example/next"}},
		Body:       body,
	}
	r.modifyResponse(rt, resp)
	if !body.closed {
		t.Fatal("refused redirect left the upstream body open")
	}
	if resp.StatusCode != http.StatusBadGateway {
		t.Fatalf("status = %d, want 502", resp.StatusCode)
	}
}
