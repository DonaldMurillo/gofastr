package pluginhost

import (
	"io/fs"
	"net/http"
	"strconv"
	"strings"

	"github.com/DonaldMurillo/gofastr/core/router"
	"github.com/DonaldMurillo/gofastr/core/static"
)

// framedCSP builds the Content-Security-Policy for the sandboxed plugin frame,
// keyed to the EXPLICIT request origin (scheme://host) rather than the 'self'
// keyword.
//
// Why not 'self': the frame runs sandbox="allow-scripts" WITHOUT
// allow-same-origin, so its document is an OPAQUE origin ("null"). Per CSP spec,
// 'self' matches the protected resource's origin, which for the frame is the
// opaque null origin, NOT the host origin that actually served editor.js/css. So
// `script-src 'self'` / `style-src 'self'` REFUSE the frame's own same-URL
// sub-resources. Chrome resolves 'self' leniently to the document's URL origin
// and loads them anyway; Safari follows the spec and BLOCKS them, leaving an
// empty, un-typeable editor. Using the concrete origin makes it unambiguous
// across browsers.
//
// style-src allows 'unsafe-inline' (ProseMirror inline style attrs + the injected
// token <style>). connect-src 'none': the editor has no network need, every host
// interaction is a postMessage, so we forbid fetch/XHR/WebSocket outright, which
// is the real exfiltration guard the sandbox + this line provide together.
//
// csp carries the per-plugin opt-in keywords from [Manifest.CSP] (threaded via
// [AssetServer.WithCSP]). They widen script-src ONLY: the sole allowlisted
// member, 'wasm-unsafe-eval', permits WebAssembly compilation inside the frame
// without enabling string eval ('unsafe-eval' is still forbidden) and without
// touching network or storage — the frame keeps its opaque origin, the
// sandbox allow-scripts directive, and connect-src 'none', so a wasm engine
// still cannot fetch, cannot reach host cookies or DOM, and cannot exfiltrate;
// data arrives over the postMessage bridge and leaves the same way. The tokens
// are re-filtered through [allowedCSPKeywords] here (Validate already checked
// them at registration; this is the authoritative assembly point, mirroring
// how SandboxString sanitises regardless of Validate), so a slice that skipped
// validation cannot smuggle a keyword. nil/empty produces the byte-identical
// policy a plugin without the tier gets.
func framedCSP(origin string, csp []string) string {
	// sandbox allow-scripts: forces the document into an opaque-origin sandbox
	// EVEN ON A TOP-LEVEL LOAD. The iframe `sandbox` attribute only sandboxes
	// the framed case; without this directive an attacker could navigate a
	// victim directly to editor.html (served text/html) and run the untrusted
	// plugin code as a first-class same-origin document. This makes the
	// sandbox intrinsic to the asset, not just the embedding.
	scriptSrc := origin
	seen := map[string]bool{}
	for _, kw := range csp {
		if !allowedCSPKeywords[kw] || seen[kw] {
			continue
		}
		seen[kw] = true
		scriptSrc += " " + kw
	}
	return "sandbox allow-scripts" +
		"; default-src " + origin +
		"; script-src " + scriptSrc +
		"; style-src " + origin + " 'unsafe-inline'" +
		"; img-src " + origin + " data:" +
		"; font-src " + origin + " data:" +
		"; connect-src 'none'" +
		"; frame-ancestors " + origin +
		"; base-uri " + origin
}

// requestOrigin reconstructs the scheme://host origin the request came in on,
// honouring a reverse-proxy X-Forwarded-Proto when present. Both the scheme
// and host are request-controlled and get interpolated into the CSP header, so
// both are strictly validated: a value carrying a space or ';' could inject an
// arbitrary CSP directive (e.g. re-enabling connect-src, the exfil guard). An
// invalid origin returns ok=false and the framed asset is refused (400) rather
// than served with a poisoned policy.
func requestOrigin(r *http.Request) (string, bool) {
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	// Only an exact http/https X-Forwarded-Proto is honoured, never a raw
	// header value spliced into the policy.
	if xf := r.Header.Get("X-Forwarded-Proto"); xf != "" {
		switch xf {
		case "http", "https":
			scheme = xf
		default:
			return "", false
		}
	}
	host := r.Host
	if host == "" {
		host = "localhost"
	}
	if !validHost(host) {
		return "", false
	}
	return scheme + "://" + host, true
}

// validHost accepts only hostname[:port] / IPv4 / bracketed-IPv6 characters,
// enough to serve any real origin, but nothing that can break out of a CSP
// directive (no space, ';', quote, comma, control char).
func validHost(host string) bool {
	if host == "" || len(host) > 253 {
		return false
	}
	for i := 0; i < len(host); i++ {
		c := host[i]
		ok := (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') ||
			(c >= '0' && c <= '9') ||
			c == '.' || c == '-' || c == ':' || c == '[' || c == ']'
		if !ok {
			return false
		}
	}
	return true
}

// AssetSpec describes one asset served from a filesystem by [AssetServer].
type AssetSpec struct {
	// Name is the filename within the AssetServer's fs.FS (e.g. "editor.html").
	// The route is registered at prefix + "/" + Name.
	Name string

	// ContentType is the exact Content-Type header (e.g.
	// "text/html; charset=utf-8"). Optional: when empty it is derived from
	// Name's extension by [static.DetectFromName]. Set it only to override
	// that default, e.g. to serve a ".js" file as "application/json".
	ContentType string

	// Framed marks the assets that make up the sandboxed plugin frame (the
	// frame document and its sub-resources). Framed assets get the
	// framing/CORP/CSP relaxation GoFastr's global security middleware
	// otherwise blocks (DECISIONS.md "Phase 0 — DONE" gotcha #1); non-framed
	// host-page scripts (the broker / adapter) are served plain.
	Framed bool
}

// AssetServer serves a plugin's embedded client assets with the correct
// Content-Types and the platform framing/CORP/CSP policy on framed assets. It
// is the client-side isolation contract, factored out of the wysiwyg plugin so
// every heavy-JS plugin reuses it instead of hand-rolling the header relaxation.
type AssetServer struct {
	prefix string
	specs  []AssetSpec
	fsys   fs.FS
	extra  []loadedAsset // byte-backed assets added via AddBytes (e.g. host scripts)
	csp    []string      // per-plugin script-src keywords from [Manifest.CSP], set via [AssetServer.WithCSP]
}

// loadedAsset is a byte-backed asset (host-page script served outside the FS).
type loadedAsset struct {
	path        string
	contentType string
	framed      bool
	bytes       []byte
}

// NewAssetServer builds an AssetServer that reads the named specs lazily from
// fsys (an embed.FS sub or any fs.FS) and serves them under prefix. Files
// missing from fsys at request time yield a 404; for go:embed'd bundles that
// never happens. Call [AssetServer.AddBytes] for host-page scripts that live in
// a different embed root, then [AssetServer.Register].
func NewAssetServer(fsys fs.FS, prefix string, specs []AssetSpec) *AssetServer {
	return &AssetServer{prefix: prefix, specs: specs, fsys: fsys}
}

// WithCSP sets the per-plugin CSP keyword extensions (from [Manifest.CSP])
// appended to framed assets' script-src, returning s for chaining:
//
//	srv := pluginhost.NewAssetServer(fsys, prefix, specs).WithCSP(mod.Manifest.CSP)
//
// Tokens are re-filtered through [allowedCSPKeywords] when the header is
// assembled, so a slice that skipped [Manifest.Validate] cannot smuggle a
// keyword. nil (the default, i.e. no call) produces the same header as a
// plugin without the tier.
func (s *AssetServer) WithCSP(tokens []string) *AssetServer {
	s.csp = tokens
	return s
}

// AddBytes registers an asset from pre-loaded bytes at an explicit full route
// path. Use it for host-page scripts (the broker adapter) that are not part of
// the framed FS. framed should be false for host scripts. An empty contentType
// is derived from route's extension, as for [AssetSpec.ContentType].
func (s *AssetServer) AddBytes(route, contentType string, framed bool, b []byte) {
	s.extra = append(s.extra, loadedAsset{
		path:        route,
		contentType: resolveContentType(route, contentType),
		framed:      framed,
		bytes:       b,
	})
}

// Register mounts every asset on the router. It is safe to register multiple
// AssetServers on the same router as long as their paths do not collide (the
// router panics on duplicate patterns otherwise).
//
// Specs with no filesystem to read them from are rejected here, at boot.
// Without this they registered fine and took down whichever request first
// asked for one, because fs.ReadFile on a nil fs.FS dereferences the nil
// interface. Boot is also the right place rather than the quieter repair of
// serving 404 per request. [ClientModule.Assets] is
// documented as optional — a plugin may serve its own assets — but then it
// does not pass specs to an AssetServer either, so a nil FS carrying specs is
// always a wiring mistake and never a runtime condition: the specs are right
// there in the same call, and a 404 on the frame document would make it one
// more construction that validates, registers, serves, and yields a frame that
// cannot work — the failure class [AssetSpec.ContentType] and [Manifest.CSP]
// already cost a debugging cycle each. A nil FS with no specs is the
// legitimate byte-backed server ([AssetServer.AddBytes] only) and is left
// alone.
func (s *AssetServer) Register(rt *router.Router) {
	if s.fsys == nil && len(s.specs) > 0 {
		panic("pluginhost: AssetServer has " + strconv.Itoa(len(s.specs)) +
			" spec(s) but a nil fs.FS to read them from — pass the plugin's " +
			"embedded assets (ClientModule.Assets) to NewAssetServer, or use " +
			"ClientModule.AssetServer which supplies them")
	}
	for _, spec := range s.specs {
		path := joinPath(s.prefix, spec.Name)
		rt.Get(path, s.serveFS(spec))
	}
	for _, a := range s.extra {
		rt.Get(a.path, s.serveBytes(a))
	}
}

// joinPath concatenates a prefix and a filename with exactly one slash.
func joinPath(prefix, name string) string {
	if prefix == "" {
		return "/" + strings.TrimPrefix(name, "/")
	}
	if strings.HasSuffix(prefix, "/") {
		return prefix + strings.TrimPrefix(name, "/")
	}
	return prefix + "/" + strings.TrimPrefix(name, "/")
}

func (s *AssetServer) serveFS(spec AssetSpec) http.HandlerFunc {
	contentType := resolveContentType(spec.Name, spec.ContentType)
	return func(w http.ResponseWriter, r *http.Request) {
		b, err := fs.ReadFile(s.fsys, spec.Name)
		if err != nil {
			http.NotFound(w, r)
			return
		}
		writeAsset(w, r, b, contentType, spec.Framed, s.csp)
	}
}

// resolveContentType returns the header to send for name, preferring the
// declared value and otherwise deriving one from the extension via
// [static.DetectFromName] — the repo's canonical detector, whose own table
// wins over mime.TypeByExtension so a plugin's .html/.js/.css/.wasm assets
// get the same type on every host, with the stdlib covering only the long
// tail and "application/octet-stream" as the floor.
//
// The empty string is not a usable header. [writeAsset] sets Content-Type
// unconditionally and then sets nosniff on the next line, so an omitted
// ContentType serves a 200 with the right bytes, an empty type, and a browser
// forbidden from recovering by sniffing: the document is never parsed, and
// neither the server log nor the console nor a page error says why (#303).
// nosniff is correct and stays; what changes is that nothing can reach it
// without the header it makes load-bearing.
func resolveContentType(name, declared string) string {
	if declared != "" {
		return declared
	}
	return static.DetectFromName(name)
}

func (s *AssetServer) serveBytes(a loadedAsset) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		writeAsset(w, r, a.bytes, a.contentType, a.framed, s.csp)
	}
}

// writeAsset emits the bytes with a fixed Content-Type and a dev Cache-Control,
// then, for framed assets only, applies the framing/CORP/CSP relaxation.
//
// GoFastr's global security middleware sends anti-embedding headers on EVERY
// response: X-Frame-Options: DENY, CSP frame-ancestors 'none', and
// Cross-Origin-Resource-Policy: same-origin. Those are correct app defaults,
// but they also block the host page from framing our OWN plugin document and
// block the opaque-origin frame (a "null" origin requester) from loading its
// JS/CSS. So for exactly the framed first-party assets we relax embedding:
//
//   - drop X-Frame-Options (no "same-origin-ancestor" mode works for an opaque
//     frame; frame-ancestors is the modern, precise control). Belt-and-suspenders:
//     a buffering middleware upstream can re-emit XFO after this Del, so the
//     EFFECTIVE framing control is the CSP frame-ancestors directive below,
//     which browsers honour OVER X-Frame-Options (DECISIONS.md Phase-0 gotcha).
//   - CSP frame-ancestors 'self', the EMBEDDER (host page) is same-origin,
//     which is what frame-ancestors checks; this SUPersedes any XFO:DENY and is
//     the load-bearing framing permission.
//   - Cross-Origin-Resource-Policy: cross-origin, so the opaque ("null") frame
//     may fetch these public, secret-free static assets.
func writeAsset(w http.ResponseWriter, r *http.Request, b []byte, contentType string, framed bool, csp []string) {
	h := w.Header()
	// A framed asset's CSP is keyed to a request-controlled origin; a bad
	// origin means we cannot build a safe policy, so refuse rather than serve
	// the untrusted document with no / a poisoned CSP.
	var origin string
	if framed {
		var ok bool
		origin, ok = requestOrigin(r)
		if !ok {
			http.Error(w, "bad request origin", http.StatusBadRequest)
			return
		}
	}
	h.Set("Content-Type", contentType)
	// Never let a browser MIME-sniff a plugin asset into a more dangerous type.
	h.Set("X-Content-Type-Options", "nosniff")
	// no-store (not no-cache): these dev assets carry no cache validator, and the
	// frame document references editor.js/css by an un-versioned relative path, so
	// a stale browser copy could otherwise linger across rebuilds. no-store forces
	// a fresh fetch every load. (A prod build would content-hash the URLs instead.)
	h.Set("Cache-Control", "no-store, max-age=0")
	if framed {
		h.Del("X-Frame-Options")
		h.Set("Content-Security-Policy", framedCSP(origin, csp))
		h.Set("Cross-Origin-Resource-Policy", "cross-origin")
	}
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(b)
}
