// Package relay serves third-party services (analytics vendors, chat
// widgets, error trackers) first-party: a declarative, hardened
// same-origin reverse proxy mounted under one path on the host app.
//
// The point is the Content-Security-Policy. A host that links
// https://vendor.example/collect.js must punch script-src/connect-src
// holes into its strict default CSP and hand every visitor's browser a
// direct channel to the vendor's origin. A host that relays the vendor
// through /__gofastr/t/... keeps `default-src 'self'` untouched: the
// browser only ever talks to the app's own origin.
//
// This is a proxy, not a tunnel. Every route names ONE fixed upstream
// at construction; request data (path tail, query, headers, body)
// never selects scheme, host, or port. See the hardening contract in
// framework/docs/content/relay.md.
package relay

import (
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"strings"

	"github.com/DonaldMurillo/gofastr/core/netguard"
	"github.com/DonaldMurillo/gofastr/framework"
)

// DefaultPath is the mount prefix used when Config.Path is empty.
const DefaultPath = "/__gofastr/t"

// reservedGoFastrSegments are the path segments directly under
// /__gofastr that the framework and its batteries already serve
// (runtime.js, app.css, the SSE bus, compute workers, …). Mounting a
// relay route onto one of them collides at registration time, so New
// refuses the Config up front instead of letting the ServeMux panic
// without context during Init.
var reservedGoFastrSegments = map[string]bool{
	"sse": true, "session": true, "action": true, "widgets": true,
	"widget": true, "embed": true, "embed.js": true, "embed-runtime.js": true,
	"runtime.js": true, "runtime": true, "app.css": true, "comp": true,
	"color-scheme.js": true, "compute": true, "pwa": true, "icons": true,
}

// defaultMaxBodyBytes caps request bodies per route when
// Route.MaxBodyBytes is zero. 8 MiB is generous for analytics beacons
// and chat/widget uploads while keeping a single request from turning
// the relay into an egress firehose.
const defaultMaxBodyBytes int64 = 8 << 20

// Config constructs a Relay. The zero value is invalid: routes are the
// whole point and must be declared explicitly (empty Methods is a
// configuration error, not "allow everything").
type Config struct {
	// Path is the local mount prefix. Default "/__gofastr/t".
	//
	// Validated at New: absolute, no trailing slash, no traversal, no
	// percent-encoding, and no collision with a reserved /__gofastr
	// route. Any other absolute path works ("/firstparty", "/fp", …).
	Path string

	// Routes declare what gets proxied. Required, non-empty.
	Routes []Route

	// ClientIP resolves the client IP written into X-Forwarded-For.
	// Default: the host part of r.RemoteAddr.
	//
	// Inbound X-Forwarded-For is NEVER trusted, on any setting: the
	// relay sits on the public edge where that header is one
	// `curl -H` away from arbitrary values. Override this only with a
	// resolver backed by a trusted proxy layer (e.g. a PROXY-protocol
	// or unix-socket peer), never with a header-reading function.
	ClientIP func(*http.Request) string
}

// Route is one fixed upstream mounted under Config.Path.
type Route struct {
	// Prefix is the local path segment under Path, e.g. "assets/" or
	// "e".
	//
	// A trailing slash declares a subtree: Path + "/" + Prefix +
	// "{rest...}" maps the sanitized remainder of the request path
	// onto the same remainder of Upstream. Without a trailing slash
	// the route matches exactly Path + "/" + Prefix and nothing
	// deeper.
	Prefix string

	// Upstream is the absolute https:// URL (scheme, host, optional
	// base path) the route proxies to. The subtree rest (sanitized) is
	// appended to the base path; the query string passes through.
	//
	// http:// is accepted ONLY for loopback hosts so tests and local
	// development work; any other http:// upstream panics at New.
	// Private/internal non-loopback hosts (RFC1918, link-local
	// including cloud metadata, CGNAT, IPv6 unique-local,
	// *.internal, metadata.google.internal) are refused at New
	// regardless of scheme: the relay has no per-customer SSRF story
	// yet, so it refuses to point at anything that smells like
	// internal infrastructure. localhost and *.localhost count as
	// loopback, so they are ACCEPTED (that's how tests and local dev
	// point at an httptest upstream).
	Upstream string

	// Methods is the allow-list of HTTP methods the route answers.
	// Empty is invalid — be explicit. Anything else gets 405 with an
	// Allow header.
	Methods []string

	// MaxBodyBytes caps the request body per route. Default 8 MiB
	// when zero; both declared Content-Length and chunked bodies are
	// enforced, with 413 on overflow.
	MaxBodyBytes int64

	// CacheOK selects the response caching posture.
	//
	// false (default): responses carry Cache-Control: no-store. The
	// relay is for beacons and dynamic endpoints; caching vendor
	// responses under the app's own origin is rarely what anyone
	// wants.
	//
	// true: upstream cache headers pass through unchanged, for
	// immutable versioned assets (/t/assets/sdk@1.2.3.js).
	CacheOK bool
}

// Relay is the first-party relay plugin. Construct with New, register
// with App.RegisterPlugin. Implements framework.Plugin.
type Relay struct {
	cfg       Config
	path      string
	clientIP  func(*http.Request) string
	transport *http.Transport
	logger    *slog.Logger
	routes    []*routeRuntime
}

// routeRuntime is a Route with everything pre-resolved at New so the
// request path does validation and no parsing.
type routeRuntime struct {
	prefix   string
	subtree  bool
	methods  []string
	maxBody  int64
	cacheOK  bool
	upstream *url.URL
	proxy    *reverseProxy
}

// New validates cfg and constructs the Relay. It panics on invalid
// configuration with a message prefixed "relay:": a bad relay Config is
// a construction-time programmer error (same posture as
// framework.NewApp's registration panics), not a runtime condition the
// process should limp along with.
func New(cfg Config) *Relay {
	if cfg.Path == "" {
		cfg.Path = DefaultPath
	}
	validatePath(cfg.Path)

	if len(cfg.Routes) == 0 {
		panic("relay: Config.Routes is empty: declare at least one fixed upstream")
	}

	seen := make(map[string]bool, len(cfg.Routes))
	routes := make([]*routeRuntime, 0, len(cfg.Routes))
	for i := range cfg.Routes {
		r := &cfg.Routes[i]
		validatePrefix(r.Prefix)
		if seen[r.Prefix] {
			panic(fmt.Sprintf("relay: route prefix %q declared twice: each prefix must map to exactly one upstream", r.Prefix))
		}
		seen[r.Prefix] = true
		if len(r.Methods) == 0 {
			panic(fmt.Sprintf("relay: route %q: Methods is empty: declare the allowed methods explicitly", r.Prefix))
		}
		for _, m := range r.Methods {
			if !validMethod(m) {
				panic(fmt.Sprintf("relay: route %q: method %q is not an uppercase HTTP token", r.Prefix, m))
			}
		}
		if r.MaxBodyBytes < 0 {
			panic(fmt.Sprintf("relay: route %q: negative MaxBodyBytes", r.Prefix))
		}
		if err := validateUpstreamURL(r.Upstream, net.LookupIP); err != nil {
			panic(fmt.Sprintf("relay: route %q: %v", r.Prefix, err))
		}
		u, err := url.Parse(r.Upstream)
		if err != nil {
			panic(fmt.Sprintf("relay: route %q: %v", r.Prefix, err))
		}
		maxBody := r.MaxBodyBytes
		if maxBody == 0 {
			maxBody = defaultMaxBodyBytes
		}
		routes = append(routes, &routeRuntime{
			prefix:   r.Prefix,
			subtree:  strings.HasSuffix(r.Prefix, "/"),
			methods:  r.Methods,
			maxBody:  maxBody,
			cacheOK:  r.CacheOK,
			upstream: u,
		})
	}

	clientIP := cfg.ClientIP
	if clientIP == nil {
		clientIP = remoteAddrHost
	}

	r := &Relay{
		cfg:       cfg,
		path:      cfg.Path,
		clientIP:  clientIP,
		transport: newTransport(),
		logger:    discardLogger(),
		routes:    routes,
	}
	for _, rt := range routes {
		rt.proxy = r.newProxy(rt)
	}
	return r
}

// Name implements framework.Plugin.
func (r *Relay) Name() string { return "relay" }

// Base returns the mount path (Config.Path, or the default). Use it to
// point server-side SDKs and page templates at the relay without
// hard-coding the prefix:
//
//	plausibleBase := relay.Base() + "/e"
func (r *Relay) Base() string { return r.path }

// Init registers the routes on the App's router and wires transport
// cleanup into shutdown. One ServeMux pattern per (route, method):
// exact routes register Path + "/" + Prefix, subtree routes register
// Path + "/" + Prefix + "{rest...}".
func (r *Relay) Init(app *framework.App) error {
	// Route the guard logs through the App's logger so a logging
	// plugin's sinks see refused-tail / redirect events too.
	r.logger = app.Logger()
	for _, rt := range r.routes {
		pattern := r.path + "/" + rt.prefix
		if rt.subtree {
			pattern += "{rest...}"
		}
		h := r.handler(rt)
		for _, m := range rt.methods {
			app.Router().Handle(m, pattern, h)
		}
	}
	// The shared transport holds keep-alive connections to every
	// upstream; close them when the app drains so a graceful shutdown
	// doesn't block on idle sockets.
	app.OnStop(func() error {
		r.transport.CloseIdleConnections()
		return nil
	})
	return nil
}

// validatePath enforces the mount contract: absolute, clean, no
// percent-encoding, and clear of the framework's reserved
// /__gofastr namespace.
func validatePath(p string) {
	if !strings.HasPrefix(p, "/") {
		panic(fmt.Sprintf("relay: Config.Path %q must be absolute (start with /)", p))
	}
	if p == "/" {
		panic(`relay: Config.Path "/" would capture the whole app`)
	}
	if strings.HasSuffix(p, "/") {
		panic(fmt.Sprintf("relay: Config.Path %q must not end with a slash", p))
	}
	if err := validateSegments("Config.Path", p); err != nil {
		panic(err.Error())
	}
	if p == "/__gofastr" {
		panic(`relay: Config.Path "/__gofastr" is the framework's reserved namespace root; mount under a subpath like "/__gofastr/t" or a custom path`)
	}
	if rest, ok := strings.CutPrefix(p, "/__gofastr/"); ok && rest != "" {
		seg := rest
		if i := strings.IndexByte(rest, '/'); i >= 0 {
			seg = rest[:i]
		}
		if reservedGoFastrSegments[seg] {
			panic(fmt.Sprintf("relay: Config.Path %q collides with the reserved framework route /__gofastr/%s", p, seg))
		}
	}
}

// validateSegments rejects traversal, empty segments, percent-encoding,
// and control bytes in an absolute path.
func validateSegments(what, p string) error {
	for i := range len(p) {
		c := p[i]
		if c <= 0x20 || c == 0x7f {
			return fmt.Errorf("relay: %s %q contains a control character or space", what, p)
		}
		if c == '%' || c == '#' || c == '?' || c == '\\' {
			return fmt.Errorf("relay: %s %q contains %q; percent-encoding and fragments are not valid here", what, p, string(c))
		}
	}
	for _, seg := range strings.Split(p[1:], "/") {
		if seg == "" {
			return fmt.Errorf("relay: %s %q contains an empty path segment", what, p)
		}
		if seg == "." || seg == ".." {
			return fmt.Errorf("relay: %s %q contains a traversal segment", what, p)
		}
	}
	return nil
}

// validatePrefix enforces the route prefix contract: relative to Path,
// clean, no encoding tricks.
func validatePrefix(prefix string) {
	if prefix == "" {
		panic("relay: route prefix must not be empty")
	}
	if strings.HasPrefix(prefix, "/") {
		panic(fmt.Sprintf("relay: route prefix %q must be relative to Config.Path, not absolute", prefix))
	}
	// A trailing slash is the subtree marker, not an empty segment;
	// validate the name with it stripped.
	name := strings.TrimSuffix(prefix, "/")
	if name == "" {
		panic(`relay: route prefix "/" would shadow the whole mount path`)
	}
	if err := validateSegments("route prefix", "/"+name); err != nil {
		panic(err.Error())
	}
}

// validMethod: uppercase HTTP token, at least one character. The
// canonical verbs plus extension methods (REPORT, PROPFIND, …) all
// fit; lowercase or spaces are configuration typos.
func validMethod(m string) bool {
	if m == "" {
		return false
	}
	for i := range len(m) {
		c := m[i]
		if c < 'A' || c > 'Z' {
			return false
		}
	}
	return true
}

// validateUpstreamURL refuses anything the relay must not proxy to.
// lookup is injectable for tests. DNS failures defer to request time
// (mirrors battery/webhook's posture): construction shouldn't require
// the vendor to be resolvable right now.
func validateUpstreamURL(raw string, lookup func(string) ([]net.IP, error)) error {
	if raw == "" {
		return fmt.Errorf("upstream URL required")
	}
	u, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("parse URL: %w", err)
	}
	scheme := strings.ToLower(u.Scheme)
	if scheme != "http" && scheme != "https" {
		return fmt.Errorf("scheme %q not allowed (need http or https)", u.Scheme)
	}
	if u.Host == "" {
		return fmt.Errorf("URL missing host")
	}
	if u.User != nil {
		return fmt.Errorf("URL with embedded userinfo not allowed (credential leakage)")
	}
	if u.RawQuery != "" || u.ForceQuery || u.Fragment != "" {
		return fmt.Errorf("upstream must be scheme+host+optional base path, no query or fragment")
	}

	host := u.Hostname()
	lower := strings.ToLower(host)

	// Loopback: the one internal class that is allowed, so tests and
	// local dev can point at httptest servers and localhost
	// sidecars. http:// is restricted to exactly this class.
	isLoopback := lower == "localhost" || strings.HasSuffix(lower, ".localhost")
	if ip := net.ParseIP(host); ip != nil && ip.IsLoopback() {
		isLoopback = true
	}
	if scheme == "http" && !isLoopback {
		return fmt.Errorf("http:// upstream %q refused: plaintext is only allowed for loopback hosts; use https", u.Host)
	}
	if isLoopback {
		return nil
	}

	// Internal-hostname denylist, cheaper than DNS and catches the
	// cloud-metadata names regardless of how they resolve.
	if strings.HasSuffix(lower, ".internal") || lower == "metadata.google.internal" {
		return fmt.Errorf("host %q targets internal infrastructure", host)
	}
	// Literal IPs: no DNS needed.
	if ip := net.ParseIP(host); ip != nil {
		return refuseInternal(ip)
	}
	// Hostname: resolve and check every address.
	addrs, err := lookup(host)
	if err != nil {
		// Unresolvable now is not refused-now; request time will
		// surface the failure as a 502.
		return nil
	}
	for _, ip := range addrs {
		if err := refuseInternal(ip); err != nil {
			return err
		}
	}
	return nil
}

// refuseInternal rejects non-loopback internal addresses via the
// single repo-wide predicate.
func refuseInternal(ip net.IP) error {
	if netguard.IsInternal(ip) && !ip.IsLoopback() {
		return fmt.Errorf("host targets internal infrastructure (%s)", netguard.Reason(ip))
	}
	return nil
}

// remoteAddrHost is the default ClientIP: the host part of the peer
// address on the wire.
func remoteAddrHost(r *http.Request) string {
	if host, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
		return host
	}
	return r.RemoteAddr
}
