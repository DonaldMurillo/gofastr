package uihost

import (
	"fmt"
	"net/url"
	"slices"
	"sort"
	"strings"
	"unicode"
)

type externalScript struct {
	src   string
	scope func(path string) bool
}

// RegisterExternalScript adds a same-origin <script defer src> to every
// full-page render, after runtime.js and the action scripts, for plugins
// and batteries that wire themselves in Init, after WithExtraScripts is
// frozen. src is an absolute path ("/x.js"), optionally with a query;
// anything else is an error. A src already registered returns nil. It
// returns an error once the host has rendered a page. Serve the bytes
// with ScriptHandler and name them with ScriptURL:
//
//	r.Get("/plug.js", uihost.ScriptHandler(js))
//	ds.RegisterExternalScript(uihost.ScriptURL("/plug.js", js))
func (ds *UIHost) RegisterExternalScript(src string) error {
	return ds.registerScript(externalScript{src: src}, "RegisterExternalScript")
}

// RegisterDocumentScript adds a same-origin <script defer src> that ships,
// marked data-cui-doc, only on pages whose route scope accepts. scope is
// asked about the registered route pattern ("/session/:id"), or the path
// itself for a page no screen matches. A navigation between pages that
// disagree on a document script loads a whole document. src follows
// RegisterExternalScript's grammar; a nil scope, a src already on the
// rail, or a call after the first render is an error.
func (ds *UIHost) RegisterDocumentScript(src string, scope func(path string) bool) error {
	if scope == nil {
		return fmt.Errorf("uihost: RegisterDocumentScript(%q): scope must be non-nil; an every-page script is RegisterExternalScript", src)
	}
	return ds.registerScript(externalScript{src: src, scope: scope}, "RegisterDocumentScript")
}

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
		return fmt.Errorf("uihost: %s(%q): src is already on the script rail; pick one lifetime and one scope per src", caller, s.src)
	}
	ds.extraScripts = append(ds.extraScripts, s)
	return nil
}

// ExtraScriptSrcs returns, sorted and deduplicated, the src of every
// script a page of one of routes loads: every-page scripts always, and a
// document script when its scope accepts one of routes. routes are route
// patterns, or the path itself for a page no screen matches.
func (ds *UIHost) ExtraScriptSrcs(routes []string) []string {
	ds.scriptMu.Lock()
	scripts := slices.Clone(ds.extraScripts)
	ds.scriptMu.Unlock()

	seen := make(map[string]bool, len(scripts))
	out := make([]string, 0, len(scripts))
	for _, s := range scripts {
		if s.scope != nil && !slices.ContainsFunc(routes, s.scope) {
			continue
		}
		if !seen[s.src] {
			seen[s.src] = true
			out = append(out, s.src)
		}
	}
	sort.Strings(out)
	return out
}

func (ds *UIHost) markServingBegun() {
	ds.scriptMu.Lock()
	ds.servingStarted.Store(true)
	ds.scriptMu.Unlock()
}

func validExternalScriptSrc(src string) bool {
	if src == "" {
		return false
	}
	if !strings.HasPrefix(src, "/") || strings.HasPrefix(src, "//") {
		return false
	}
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
