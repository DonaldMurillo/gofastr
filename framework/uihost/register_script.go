package uihost

import (
	"fmt"
	"net/url"
	"strings"
	"unicode"
)

// externalScript is one entry on the extra-script rail. scope != nil
// marks a DOCUMENT-lifetime script (RegisterDocumentScript): emitted
// only on pages the scope accepts, tagged data-fui-doc, and carried in
// the route manifest so the client runtime loads a real document when a
// navigation crosses the scope edge. scope == nil is the every-page
// lifetime (RegisterExternalScript, WithExtraScripts).
type externalScript struct {
	src   string
	scope func(path string) bool
}

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
	return ds.registerScript(externalScript{src: src}, "RegisterExternalScript")
}

// RegisterDocumentScript adds an external same-origin <script src> to
// the rail with a DOCUMENT lifetime: the tag ships only on pages the
// scope predicate accepts and carries the data-fui-doc marker; every
// other page omits it entirely.
//
// Use it for scripts that install capabilities into the document
// itself — the WebMCP bridge registering tools on
// navigator.modelContext is the driving case. Two browser facts make
// the page scope a document boundary, and the client runtime enforces
// it: removing a script tag does not uninstall what the script
// installed, and a partial (SPA) swap never runs a body script. A
// navigation whose source and destination disagree about ANY document
// script therefore performs a real document load instead of a partial
// swap; same-set navigations stay partial. The route manifest carries
// each route's document-script set as `docScripts`, and back/forward
// across an edge loads the destination fresh, so a document only ever
// carries its own capabilities.
//
// scope is called with the page path: the concrete request path when a
// page renders ("/session/42") and the route pattern when the manifest
// is built ("/session/:id"). Write prefix-style predicates
// (strings.HasPrefix(path, "/session/")) so both calls agree; a
// predicate that answers differently for a pattern and its concrete
// paths degrades every in-scope navigation to a full load — safe, but
// it forfeits the soft-nav cache.
//
// src follows the same same-origin grammar as RegisterExternalScript.
// A duplicate every-page registration is idempotent; a src already on
// the rail under the other lifetime, or already document-scoped (two
// scopes on one src are not comparable), is refused, as is any
// registration once serving has begun.
func (ds *UIHost) RegisterDocumentScript(src string, scope func(path string) bool) error {
	if scope == nil {
		return fmt.Errorf("uihost: RegisterDocumentScript(%q): scope must be non-nil; an every-page script is RegisterExternalScript", src)
	}
	return ds.registerScript(externalScript{src: src, scope: scope}, "RegisterDocumentScript")
}

// registerScript validates and appends one rail entry under a single
// lock, sharing the grammar check, the serving latch, and the dedupe
// order between both lifetimes.
func (ds *UIHost) registerScript(s externalScript, caller string) error {
	if !validExternalScriptSrc(s.src) {
		return fmt.Errorf("uihost: %s: invalid src %q: must be a same-origin absolute path (\"/x.js\"), optionally with a query (\"/x.js?v=abc\")", caller, s.src)
	}
	ds.scriptMu.Lock()
	defer ds.scriptMu.Unlock()
	if ds.servingStarted.Load() {
		return fmt.Errorf("uihost: %s(%q) refused: serving has already begun; register during plugin Init, before the first page render", caller, s.src)
	}
	for _, e := range ds.extraScripts {
		if e.src != s.src {
			continue
		}
		if e.scope == nil && s.scope == nil {
			return nil // already on the rail; idempotent
		}
		// Either the lifetimes contradict (every-page vs
		// document-scoped) or the src is already document-scoped: two
		// scope funcs cannot be compared, and silently keeping the
		// first would ship a boundary the caller never declared.
		return fmt.Errorf("uihost: %s(%q): src is already on the script rail; pick one lifetime and one scope per src", caller, s.src)
	}
	ds.extraScripts = append(ds.extraScripts, s)
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
