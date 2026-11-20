// Package relay serves third-party services (analytics vendors, chat
// widgets, error trackers) first-party: a declarative, hardened
// same-origin reverse proxy mounted under one path on the host app.
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

var reservedGoFastrSegments = map[string]bool{
	"sse": true, "session": true, "action": true, "widgets": true,
	"widget": true, "embed": true, "embed.js": true, "embed-runtime.js": true,
	"runtime.js": true, "runtime": true, "app.css": true, "comp": true,
	"color-scheme.js": true, "compute": true, "pwa": true, "icons": true,
}

const defaultMaxBodyBytes int64 = 8 << 20

// Config constructs a Relay.
type Config struct {
	// Path is the local mount prefix.
	Path string

	// Routes declare what gets proxied.
	Routes []Route

	// ClientIP resolves the client IP written into X-Forwarded-For.
	ClientIP func(*http.Request) string
}

// Route is one fixed upstream mounted under Config.Path.
type Route struct {
	// Prefix is the local path segment under Path, e.g. "assets/" or "e".
	Prefix string

	// Upstream is the absolute https:// URL (scheme, host, optional base
	// path) the route proxies to.
	Upstream string

	// Methods is the allow-list of HTTP methods the route answers.
	Methods []string

	// MaxBodyBytes caps the request body per route.
	MaxBodyBytes int64

	// CacheOK selects the response caching posture.
	CacheOK bool

	// ContentTypes names media types this route may relay on top of the
	// default set: script, style, image, font, JSON and text/plain.
	ContentTypes []string
}

// Relay is the first-party relay plugin.
type Relay struct {
	cfg       Config
	path      string
	clientIP  func(*http.Request) string
	transport *http.Transport
	logger    *slog.Logger
	routes    []*routeRuntime
}

type routeRuntime struct {
	prefix   string
	subtree  bool
	methods  []string
	maxBody  int64
	cacheOK  bool
	extraCT  map[string]bool
	upstream *url.URL
	proxy    *reverseProxy
}

// New validates cfg and constructs the Relay.
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
		extraCT := make(map[string]bool, len(r.ContentTypes))
		for _, ct := range r.ContentTypes {
			if !validMediaType(ct) {
				panic(fmt.Sprintf("relay: route %q: ContentTypes entry %q is not a lowercase type/subtype", r.Prefix, ct))
			}
			extraCT[ct] = true
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
			extraCT:  extraCT,
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

// Base returns the mount path (Config.Path, or the default).
func (r *Relay) Base() string { return r.path }

// Init registers the routes on the App's router and wires transport
// cleanup into shutdown.
func (r *Relay) Init(app *framework.App) error {
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
	app.OnStop(func() error {
		r.transport.CloseIdleConnections()
		return nil
	})
	return nil
}

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

func validateSegments(what, p string) error {
	for i := range len(p) {
		c := p[i]
		if c <= 0x20 || c == 0x7f {
			return fmt.Errorf("relay: %s %q contains a control character or space", what, p)
		}
		if c == '{' || c == '}' || c == '$' {
			return fmt.Errorf("relay: %s %q must be a literal path without ServeMux metacharacters", what, p)
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

func validatePrefix(prefix string) {
	if prefix == "" {
		panic("relay: route prefix must not be empty")
	}
	if strings.HasPrefix(prefix, "/") {
		panic(fmt.Sprintf("relay: route prefix %q must be relative to Config.Path, not absolute", prefix))
	}
	name := strings.TrimSuffix(prefix, "/")
	if name == "" {
		panic(`relay: route prefix "/" would shadow the whole mount path`)
	}
	if err := validateSegments("route prefix", "/"+name); err != nil {
		panic(err.Error())
	}
}

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

	if strings.HasSuffix(lower, ".internal") || lower == "metadata.google.internal" {
		return fmt.Errorf("host %q targets internal infrastructure", host)
	}
	if ip := net.ParseIP(host); ip != nil {
		return refuseInternal(ip)
	}
	addrs, err := lookup(host)
	if err != nil {
		return nil
	}
	for _, ip := range addrs {
		if err := refuseInternal(ip); err != nil {
			return err
		}
	}
	return nil
}

func refuseInternal(ip net.IP) error {
	if netguard.IsInternal(ip) && !ip.IsLoopback() {
		return fmt.Errorf("host targets internal infrastructure (%s)", netguard.Reason(ip))
	}
	return nil
}

func remoteAddrHost(r *http.Request) string {
	if host, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
		return host
	}
	return r.RemoteAddr
}
