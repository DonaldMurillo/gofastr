// Package registry is the process-global catalog of components whose
// CSS is shipped as real stylesheets and loaded on demand by the
// runtime.
//
// Registration is handle-based: RegisterStyle returns a *Style. The
// caller stashes the handle in a package var and reuses it at every
// render site. The component name lives in exactly one place.
//
//	// modal/modal.go
//	var Style = registry.RegisterStyle("modal", modalCSS)
//
//	func modalCSS(t style.Theme) string {
//	    return style.NewComponentSheet("modal", t).
//	        Rule(".header").Set("font-weight", "700").End().
//	        MustBuild()
//	}
//
//	// at a render site:
//	func (s *Screen) Render() render.HTML {
//	    return modal.Style.Render(&modal.Modal{Title: "Hi"})
//	}
//
// Style.Render wraps the component, injects data-fui-comp="<name>"
// onto its outermost tag (no extra DOM node), and records the name
// into a request-scoped collector so the SSR host can emit a <link>
// in <head> before first paint. After hydration, the runtime takes
// over: any data-fui-comp marker in newly inserted DOM triggers
// loadComponentCSS, which dedups on the link's data-fui-style attr.
//
// Component CSS is always scoped to [data-fui-comp="<name>"]. Global
// rules belong in theme.css or WithCustomCSS — see the design doc at
// core-ui/ARCHITECTURE.md.
package registry

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"reflect"
	"runtime"
	"sort"
	"sync"

	"github.com/gofastr/gofastr/core-ui/style"
)

// LoadMode controls when a component's CSS is fetched by the browser.
type LoadMode int

const (
	// LoadAuto loads the component's CSS the first time its marker
	// appears in the DOM. The SSR host also emits a <link> in <head>
	// for any LoadAuto component rendered on the initial page, so
	// there is no FOUC on hard load.
	LoadAuto LoadMode = iota

	// LoadPrewarm behaves like LoadAuto + a throttled idle-time
	// prefetch. Use for components that are likely to appear soon
	// (a command palette, a modal opened from a hotkey).
	LoadPrewarm

	// LoadAlways emits the <link> in <head> on every page,
	// regardless of whether the page renders the component. Use for
	// page chrome — headers, layout primitives — that almost every
	// screen touches.
	LoadAlways
)

// Entry is one row in the catalog.
type Entry struct {
	Name    string
	StyleFn func(style.Theme) string
	Load    LoadMode

	// internal caches; keyed by themeHash.
	mu           sync.Mutex
	versionCache map[string]string
	cssCache     map[string]string
}

// CSSFor returns the scoped CSS bytes for this entry under the given
// theme, building and caching on first call.
func (e *Entry) CSSFor(theme style.Theme) string {
	h := themeHash(theme)
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.cssCache == nil {
		e.cssCache = make(map[string]string, 1)
		e.versionCache = make(map[string]string, 1)
	}
	if css, ok := e.cssCache[h]; ok {
		return css
	}
	css := e.StyleFn(theme)
	e.cssCache[h] = css
	sum := sha256.Sum256([]byte(h + "\x00" + css))
	e.versionCache[h] = hex.EncodeToString(sum[:6])
	return css
}

// VersionFor returns the content-addressed version string for this
// entry's CSS under the given theme. Build is shared with CSSFor.
func (e *Entry) VersionFor(theme style.Theme) string {
	_ = e.CSSFor(theme) // ensure cache is warm
	h := themeHash(theme)
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.versionCache[h]
}

// Option configures an Entry at registration time.
type Option func(*Entry)

// WithLoad sets the load mode. Default is LoadAuto.
func WithLoad(m LoadMode) Option { return func(e *Entry) { e.Load = m } }

// Style is the handle returned by RegisterStyle. Authors keep it in a
// package var and call .Render at every use site.
type Style struct{ e *Entry }

// Name returns the registered name.
func (s *Style) Name() string { return s.e.Name }

// Entry returns the underlying Entry (for the catalog endpoint).
func (s *Style) Entry() *Entry { return s.e }

var (
	mu      sync.Mutex
	entries = map[string]*Entry{}
)

// RegisterStyle registers a component's stylesheet builder under a
// process-wide unique name and returns a handle. Identical
// re-registration (same StyleFn pointer + same options) is a no-op;
// any other duplicate panics so misnames surface at startup.
func RegisterStyle(name string, fn func(style.Theme) string, opts ...Option) *Style {
	if name == "" {
		panic("registry.RegisterStyle: name must be non-empty")
	}
	if fn == nil {
		panic("registry.RegisterStyle(" + name + "): StyleFn must be non-nil")
	}
	e := &Entry{Name: name, StyleFn: fn, Load: LoadAuto}
	for _, o := range opts {
		o(e)
	}
	mu.Lock()
	defer mu.Unlock()
	if existing, ok := entries[name]; ok {
		if !sameEntry(existing, e) {
			panic(fmt.Sprintf(
				"registry.RegisterStyle: duplicate name %q with different definition — pick a unique name in one of the two call sites\n"+
					"  existing: load=%d styleFn=%s\n"+
					"  new:      load=%d styleFn=%s",
				name, existing.Load, fnLocation(existing.StyleFn), e.Load, fnLocation(e.StyleFn),
			))
		}
		return &Style{e: existing}
	}
	entries[name] = e
	return &Style{e: e}
}

// Lookup returns the entry registered under name.
func Lookup(name string) (*Entry, bool) {
	mu.Lock()
	defer mu.Unlock()
	e, ok := entries[name]
	return e, ok
}

// All returns a snapshot of every registered entry, sorted by name.
func All() []*Entry {
	mu.Lock()
	defer mu.Unlock()
	out := make([]*Entry, 0, len(entries))
	for _, e := range entries {
		out = append(out, e)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// reset is for tests only.
func reset() {
	mu.Lock()
	defer mu.Unlock()
	entries = map[string]*Entry{}
}

func sameEntry(a, b *Entry) bool {
	return a.Name == b.Name &&
		a.Load == b.Load &&
		fnPtr(a.StyleFn) == fnPtr(b.StyleFn)
}

func fnPtr(fn func(style.Theme) string) uintptr {
	return reflect.ValueOf(fn).Pointer()
}

// fnLocation returns "name (file:line)" for the given function value,
// so duplicate-registration panics tell the user which call site to
// rename instead of printing raw uintptrs.
func fnLocation(fn func(style.Theme) string) string {
	pc := reflect.ValueOf(fn).Pointer()
	rf := runtime.FuncForPC(pc)
	if rf == nil {
		return fmt.Sprintf("0x%x", pc)
	}
	file, line := rf.FileLine(pc)
	return fmt.Sprintf("%s (%s:%d)", rf.Name(), file, line)
}

// themeHash hashes the theme's deterministic CSS custom properties.
// Two themes that resolve identically produce the same hash.
func themeHash(t style.Theme) string {
	sum := sha256.Sum256([]byte(t.CSSCustomProperties()))
	return hex.EncodeToString(sum[:6])
}
