package relay

import (
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/framework"
)

func TestValidateTailRejectsHostileForms(t *testing.T) {
	bad := []string{
		"../x",           // literal traversal (mux usually cleans it; sink still rejects)
		"a/../../x",      // mid-path traversal
		"e/..",           // trailing dot-dot
		"a\\b",           // backslash (alternate path separator on win32 upstreams)
		"a\x00b",         // NUL
		"a\x01b",         // control
		"a\x1fb",         // control
		"a\x7fb",         // DEL
		"a#b",            // fragment smuggled into the path
		"a//b",           // empty segment (raw-encoded form reaches the sink)
		"/abs",           // leading slash would double-join
		"a/b/../../../c", // deep traversal
	}
	for _, tail := range bad {
		if err := validateTail(tail, false); err == nil {
			t.Errorf("validateTail(%q) accepted a hostile form", tail)
		}
	}
	// Non-canonical percent-encoding anywhere in the URL (RawPath set)
	// is refused regardless of the decoded value: %2F smuggling is
	// invisible in the decoded tail.
	for _, tail := range []string{"a/b.js", "lib/app.js", "x"} {
		if err := validateTail(tail, true); err == nil {
			t.Errorf("validateTail(%q, rawEncoded=true) accepted an encoded-smuggle form", tail)
		}
	}
	good := []string{"", "a.js", "lib/app.js", "v1/x.y-z_1.js"}
	for _, tail := range good {
		if err := validateTail(tail, false); err != nil {
			t.Errorf("validateTail(%q) = %v, want accepted", tail, err)
		}
	}
}

func TestHostileTailsRejectedOrStayOnHost(t *testing.T) {
	base, cap := startRelayCap(t, defaultCfg)

	// One legit request first so the capture holds the real upstream
	// host for comparison.
	res, err := http.Get(base + "/__gofastr/t/e/x")
	if err != nil {
		t.Fatalf("legit get: %v", err)
	}
	res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("legit get status = %d", res.StatusCode)
	}
	_, _, upstreamHost, _, _ := cap.snapshot()
	if upstreamHost == "" {
		t.Fatal("no upstream host captured")
	}

	// Smuggled-encoding, traversal, and control forms must be refused
	// outright: no 200 from anywhere.
	mustRefuse := []string{
		"..%2F..%2Fkeys",
		"%2e%2e/admin",
		"a%2Fb",
		"%2f%2fevil.example",
		"a%5Cb",
		"a%00b",
		"a%01b",
		"a%1fb",
		"a%23b",
		"../../../etc/passwd",
	}
	for _, tail := range mustRefuse {
		req, _ := http.NewRequest(http.MethodGet, base+"/__gofastr/t/e/"+tail, nil)
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("tail %q: request error: %v", tail, err)
		}
		res.Body.Close()
		if res.StatusCode == http.StatusOK {
			t.Fatalf("tail %q: answered 200; smuggling must be refused, got uri from upstream", tail)
		}
	}

	// Origin-looking tails are either cleaned by the mux or forwarded
	// as ordinary path segments — either way they land on the FIXED
	// upstream, inside its base path. Request data can never select
	// scheme, host, or port.
	mustStayOnHost := []string{
		"https://evil.example",
		"http://evil.example",
		"//evil.example",
		"https:////evil.example",
		"@evil.example/x",
		"evil.example:8443/x",
	}
	for _, tail := range mustStayOnHost {
		req, _ := http.NewRequest(http.MethodGet, base+"/__gofastr/t/e/"+tail, nil)
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("tail %q: request error: %v", tail, err)
		}
		res.Body.Close()
		if res.StatusCode == http.StatusOK {
			_, uri, host, _, _ := cap.snapshot()
			if host != upstreamHost {
				t.Fatalf("tail %q: 200 from host %q (upstream host %q) — request data selected the origin", tail, host, upstreamHost)
			}
			if !strings.HasPrefix(uri, "/cdn/") {
				t.Fatalf("tail %q: escaped the upstream base path: %q", tail, uri)
			}
		}
	}
}

// TestRawFragmentTailRefused: a literal "#" in the request-target
// (only sendable by a raw client — Go's http.Client strips fragments
// and escapes any "#" it finds in a URL) reaches the handler decoded
// into the tail. The sink validator must refuse it: forwarded verbatim
// it would be re-escaped upstream, but a relay that accepts fragment
// markers into tails is one refactor away from accepting them
// anywhere.
func TestRawFragmentTailRefused(t *testing.T) {
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(up.Close)

	app := framework.NewApp(framework.WithoutDefaultMiddleware())
	app.RegisterPlugin(New(defaultCfg(up.URL + "/cdn")))
	if err := app.InitPlugins(); err != nil {
		t.Fatalf("InitPlugins: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/__gofastr/t/e/lib.js#frag", nil)
	rec := httptest.NewRecorder()
	app.Router().ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 for a fragment in the tail", rec.Code)
	}
}

func TestNewPanicsOnBadUpstream(t *testing.T) {
	bad := []string{
		"",
		"notaurl",
		"ftp://example.com/x",
		"https://user:pw@example.com", // embedded credentials
		"https://example.com?q=1",     // query in upstream base
		"https://example.com#frag",    // fragment in upstream base
		"http://example.com",          // http to a non-loopback host
		"http://192.168.1.4",          // http + private
		"https://10.0.0.5",            // RFC1918 literal
		"https://192.168.1.4",         // RFC1918 literal
		"https://172.16.3.3",          // RFC1918 literal
		"https://169.254.169.254",     // cloud metadata
		"https://100.64.0.1",          // CGNAT
		"https://host.internal",       // internal-suffixed name
		"https://metadata.google.internal",
		"https://[fd00::1]", // IPv6 unique-local
	}
	for _, up := range bad {
		cfg := Config{Routes: []Route{{Prefix: "e/", Upstream: up, Methods: []string{"GET"}}}}
		got := func() (msg string) {
			defer func() { msg, _ = recover().(string) }()
			New(cfg)
			return ""
		}()
		if got == "" {
			t.Errorf("New(upstream=%q) did not panic", up)
			continue
		}
		if !strings.Contains(got, "relay:") {
			t.Errorf("New(upstream=%q) panic = %q, want a relay: message", up, got)
		}
	}
}

func TestNewAcceptsLoopbackAndHTTPS(t *testing.T) {
	good := []string{
		"http://127.0.0.1:9999",   // loopback literal, http allowed
		"http://localhost:9999",   // loopback name
		"http://[::1]:9999",       // IPv6 loopback
		"https://example.com",     // plain https
		"https://example.com/cdn", // base path
		"https://203.0.113.7",     // public literal
	}
	for _, up := range good {
		cfg := Config{Routes: []Route{{Prefix: "e/", Upstream: up, Methods: []string{"GET"}}}}
		r := New(cfg)
		if r == nil || r.Name() != "relay" {
			t.Errorf("New(upstream=%q) returned %v", up, r)
		}
	}
}

func TestResolvedPrivateUpstreamRefused(t *testing.T) {
	lookupPrivate := func(host string) ([]net.IP, error) {
		return []net.IP{net.ParseIP("203.0.113.7"), net.ParseIP("10.0.0.7")}, nil
	}
	if err := validateUpstreamURL("https://acme.dev", lookupPrivate); err == nil {
		t.Fatal("hostname resolving onto an RFC1918 IP was accepted")
	}
	lookupPublic := func(host string) ([]net.IP, error) {
		return []net.IP{net.ParseIP("203.0.113.7")}, nil
	}
	if err := validateUpstreamURL("https://acme.dev", lookupPublic); err != nil {
		t.Fatalf("public resolution refused: %v", err)
	}
	lookupFail := func(host string) ([]net.IP, error) {
		return nil, net.UnknownNetworkError("dns")
	}
	if err := validateUpstreamURL("https://acme.dev", lookupFail); err != nil {
		t.Fatalf("DNS failure at construction should defer to request time, got %v", err)
	}
}

func TestNewPanicsOnBadRouteConfig(t *testing.T) {
	cases := []struct {
		name string
		r    Route
	}{
		{"empty prefix", Route{Upstream: "https://example.com", Methods: []string{"GET"}}},
		{"traversal prefix", Route{Prefix: "../x", Upstream: "https://example.com", Methods: []string{"GET"}}},
		{"dot-dot segment", Route{Prefix: "e/..", Upstream: "https://example.com", Methods: []string{"GET"}}},
		{"empty segment", Route{Prefix: "a//b", Upstream: "https://example.com", Methods: []string{"GET"}}},
		{"absolute prefix", Route{Prefix: "/abs", Upstream: "https://example.com", Methods: []string{"GET"}}},
		{"encoded prefix", Route{Prefix: "a%2fb", Upstream: "https://example.com", Methods: []string{"GET"}}},
		{"control char prefix", Route{Prefix: "a\x01b", Upstream: "https://example.com", Methods: []string{"GET"}}},
		{"backslash prefix", Route{Prefix: `a\b`, Upstream: "https://example.com", Methods: []string{"GET"}}},
		{"empty methods", Route{Prefix: "e/", Upstream: "https://example.com"}},
		{"lowercase method", Route{Prefix: "e/", Upstream: "https://example.com", Methods: []string{"get"}}},
		{"method with space", Route{Prefix: "e/", Upstream: "https://example.com", Methods: []string{"POS T"}}},
		{"negative body cap", Route{Prefix: "e/", Upstream: "https://example.com", Methods: []string{"GET"}, MaxBodyBytes: -1}},
	}
	for _, tc := range cases {
		cfg := Config{Routes: []Route{tc.r}}
		got := func() (msg string) {
			defer func() { msg, _ = recover().(string) }()
			New(cfg)
			return ""
		}()
		if got == "" {
			t.Errorf("New(%s) did not panic", tc.name)
			continue
		}
		if !strings.Contains(got, "relay:") {
			t.Errorf("New(%s) panic = %q, want a relay: message", tc.name, got)
		}
	}

	// Duplicate prefixes across routes.
	dup := func() (msg string) {
		defer func() { msg, _ = recover().(string) }()
		New(Config{Routes: []Route{
			{Prefix: "e/", Upstream: "https://example.com", Methods: []string{"GET"}},
			{Prefix: "e/", Upstream: "https://other.example.com", Methods: []string{"POST"}},
		}})
		return ""
	}()
	if dup == "" {
		t.Error("duplicate route prefix did not panic")
	}
}

func TestNewPanicsOnBadPath(t *testing.T) {
	bad := []string{
		"", // handled by default, not panic — see below; keep list explicit
		"/__gofastr",
		"/__gofastr/",
		"/__gofastr/sse",
		"/__gofastr/runtime.js/x",
		"t/e",
		"/t/",
		"/a//b",
		"/a/../b",
		"/a%2Fb",
		"/a b",
	}
	for _, p := range bad {
		if p == "" {
			continue
		}
		cfg := Config{Path: p, Routes: []Route{{Prefix: "e/", Upstream: "https://example.com", Methods: []string{"GET"}}}}
		got := func() (msg string) {
			defer func() { msg, _ = recover().(string) }()
			New(cfg)
			return ""
		}()
		if got == "" {
			t.Errorf("New(path=%q) did not panic", p)
			continue
		}
		if !strings.Contains(got, "relay:") {
			t.Errorf("New(path=%q) panic = %q, want a relay: message", p, got)
		}
	}
}

func TestNoRoutesPanics(t *testing.T) {
	got := func() (msg string) {
		defer func() { msg, _ = recover().(string) }()
		New(Config{Path: "/t"})
		return ""
	}()
	if got == "" {
		t.Fatal("New with zero routes did not panic")
	}
}
