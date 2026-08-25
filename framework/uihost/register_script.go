package uihost

import (
	"fmt"
	"net/url"
	"strings"
	"unicode"
)

// RegisterExternalScript adds an external same-origin <script src> URL to
// every full-page render, emitted just before </body> after runtime.js and
// the per-screen action scripts. It exists for plugins and batteries that
// wire themselves in Init, which runs after Mount — when the
// construction-time options (WithExtraScripts) are already frozen.
//
// src must be a same-origin absolute path ("/x.js"), optionally carrying a
// query string ("/x.js?v=abc" for hash-versioned URLs). Schemes, hosts,
// protocol-relative URLs, backslashes, control characters, fragments, and
// "." / ".." path segments (raw or percent-encoded) are rejected: the tag
// loads under the default CSP's script-src 'self', so every accepted form
// must resolve to a same-origin path.
//
// Registration is idempotent: a src already on the rail returns nil, and
// first-registration order is preserved. It returns an error once the host
// has rendered a page: a script registered after serving began would ship
// on some pages and not others.
//
// Pair with ScriptHandler and ScriptURL to serve the bytes with correct
// caching:
//
//	js := []byte("…")
//	r.Get("/plug.js", uihost.ScriptHandler(js))
//	ds.RegisterExternalScript(uihost.ScriptURL("/plug.js", js))
func (ds *UIHost) RegisterExternalScript(src string) error {
	if !validExternalScriptSrc(src) {
		return fmt.Errorf("uihost: RegisterExternalScript: invalid src %q: must be a same-origin absolute path (\"/x.js\"), optionally with a query (\"/x.js?v=abc\")", src)
	}
	ds.scriptMu.Lock()
	defer ds.scriptMu.Unlock()
	if ds.servingStarted.Load() {
		return fmt.Errorf("uihost: RegisterExternalScript(%q) refused: serving has already begun; register during plugin Init, before the first page render", src)
	}
	for _, s := range ds.extraScripts {
		if s == src {
			return nil // already on the rail; idempotent
		}
	}
	ds.extraScripts = append(ds.extraScripts, src)
	return nil
}

// markServingBegun latches on the first full-shell render. It takes
// scriptMu so the latch serializes with RegisterExternalScript's
// check+append: any registration that observes servingStarted==false
// completes its append before the calling render reads extraScripts, and
// once the flag is set no append can follow. The emission loop in
// injectChromeModeFor therefore needs no lock.
func (ds *UIHost) markServingBegun() {
	ds.scriptMu.Lock()
	ds.servingStarted.Store(true)
	ds.scriptMu.Unlock()
}

// validExternalScriptSrc reports whether src is safe to emit as
// <script src="src"> on every page. Same grammar as isSafePartialRedirect
// (single leading "/", no scheme, no host, no backslash, no control bytes,
// checked on both the raw input AND the percent-decoded path) plus
// script-rail specifics: no "." / ".." segments (traversal), no fragments,
// and query strings allowed (hash-versioned URLs like /x.js?v=abc need
// them).
func validExternalScriptSrc(src string) bool {
	if src == "" {
		return false
	}
	if !strings.HasPrefix(src, "/") || strings.HasPrefix(src, "//") {
		return false
	}
	// A ":" before the first "/" is a scheme (https:, javascript:, data:).
	// Unreachable after the leading-"/" rule above, but it keeps the
	// grammar explicit and holds if the checks are ever reordered.
	if i, j := strings.IndexByte(src, ':'), strings.IndexByte(src, '/'); i >= 0 && (j < 0 || i < j) {
		return false
	}
	u, err := url.Parse(src)
	if err != nil {
		return false
	}
	if u.Scheme != "" || u.Opaque != "" || u.Host != "" {
		return false
	}
	if strings.Contains(src, "#") || u.Fragment != "" {
		return false
	}
	// Raw input AND decoded path: "/a%5Cb.js" passes the raw checks but
	// decodes to a backslash the browser then normalizes.
	if hasControlOrBackslash(src) || hasControlOrBackslash(u.Path) {
		return false
	}
	if !strings.HasPrefix(u.Path, "/") || strings.HasPrefix(u.Path, "//") {
		return false
	}
	for _, seg := range strings.Split(u.Path, "/") {
		if seg == "." || seg == ".." {
			return false
		}
	}
	return true
}

func hasControlOrBackslash(s string) bool {
	return strings.ContainsRune(s, '\\') || strings.IndexFunc(s, unicode.IsControl) >= 0
}
