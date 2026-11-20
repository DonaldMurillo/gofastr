// Package registry is the process-global catalog of component styles,
// behaviours, templates and data blocks the runtime loads on demand.
package registry

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"maps"
	"reflect"
	"runtime"
	"slices"
	"strings"
	"sync"

	"github.com/DonaldMurillo/gofastr/core-ui/runtime/minify"
	"github.com/DonaldMurillo/gofastr/core-ui/style"
)

// LoadMode controls when a component's CSS is fetched by the browser.
type LoadMode int

const (
	// LoadAuto loads the sheet when its marker first renders.
	LoadAuto LoadMode = iota

	// LoadPrewarm is LoadAuto plus an idle-time prefetch.
	LoadPrewarm

	// LoadAlways links the sheet on every page.
	LoadAlways
)

// Entry is one row in the catalog.
type Entry struct {
	Name    string
	StyleFn func(style.Theme) string
	Load    LoadMode

	mu           sync.Mutex
	versionCache map[string]string
	cssCache     map[string]string
	servedCache  map[string]string
}

// CSSFor returns the entry's CSS under theme, cached per theme.
func (e *Entry) CSSFor(theme style.Theme) string {
	return e.CSSForHash(theme, themeHash(theme))
}

// CSSForHash is CSSFor for a caller holding h, which must be style.ThemeHash(theme).
func (e *Entry) CSSForHash(theme style.Theme, h string) string {
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
	return css
}

// ServedCSSFor is CSSFor as sent to a browser: minified when minify.Enabled.
func (e *Entry) ServedCSSFor(theme style.Theme) string {
	return e.ServedCSSForHash(theme, themeHash(theme))
}

// ServedCSSForHash is ServedCSSFor with the theme hash supplied.
func (e *Entry) ServedCSSForHash(theme style.Theme, h string) string {
	css := e.CSSForHash(theme, h)
	if !minify.Enabled() {
		return css
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.servedCache == nil {
		e.servedCache = make(map[string]string, 1)
	}
	if out, ok := e.servedCache[h]; ok {
		return out
	}
	out := minify.CSS(css)
	e.servedCache[h] = out
	return out
}

// VersionFor returns the content hash of the served CSS under theme.
func (e *Entry) VersionFor(theme style.Theme) string {
	return e.VersionForHash(theme, themeHash(theme))
}

// VersionForHash is VersionFor with the theme hash supplied.
func (e *Entry) VersionForHash(theme style.Theme, h string) string {
	served := e.ServedCSSForHash(theme, h)
	e.mu.Lock()
	defer e.mu.Unlock()
	if v, ok := e.versionCache[h]; ok {
		return v
	}
	sum := sha256.Sum256([]byte(h + "\x00" + served))
	v := hex.EncodeToString(sum[:6])
	e.versionCache[h] = v
	return v
}

// EvictTheme drops this entry's caches for hash.
func (e *Entry) EvictTheme(hash string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	delete(e.cssCache, hash)
	delete(e.versionCache, hash)
	delete(e.servedCache, hash)
}

// HasCachedTheme reports whether this entry holds CSS cached for hash.
func (e *Entry) HasCachedTheme(hash string) bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	_, ok := e.cssCache[hash]
	return ok
}

// Option configures an Entry at registration time.
type Option func(*Entry)

// WithLoad sets the load mode. Default is LoadAuto.
func WithLoad(m LoadMode) Option { return func(e *Entry) { e.Load = m } }

// Style is the handle RegisterStyle returns.
type Style struct{ e *Entry }

// Name returns the registered name.
func (s *Style) Name() string { return s.e.Name }

// Entry returns the underlying Entry (for the catalog endpoint).
func (s *Style) Entry() *Entry { return s.e }

var (
	mu      sync.Mutex
	entries = map[string]*Entry{}
	frozen  bool
)

// Freeze marks the catalog built; RegisterStyle and RegisterBehavior then refuse new names.
func Freeze() {
	mu.Lock()
	frozen = true
	mu.Unlock()
}

// RegisterStyle registers a component's stylesheet builder under a unique name and returns its handle.
func RegisterStyle(name string, fn func(style.Theme) string, opts ...Option) *Style {
	return registerStyle(name, fn, false, opts)
}

func registerStyle(name string, fn func(style.Theme) string, late bool, opts []Option) *Style {
	if !behaviorName.MatchString(name) {
		panic(fmt.Sprintf("registry.RegisterStyle: name %q must match ^[a-z][a-z0-9-]{0,63}$ — it is a URL segment and the data-cui-comp marker value", name))
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
				"registry.RegisterStyle: duplicate name %q with different definition. Pick a unique name in one of the two call sites\n"+
					"  existing: load=%d styleFn=%s\n"+
					"  new:      load=%d styleFn=%s",
				name, existing.Load, fnLocation(existing.StyleFn), e.Load, fnLocation(e.StyleFn),
			))
		}
		return &Style{e: existing}
	}
	if frozen && !late {
		panic(fmt.Sprintf("registry: style %q registered after the component catalog froze; register styles at package init", name))
	}
	entries[name] = e
	return &Style{e: e}
}

// RegisterRuntimeStyle is RegisterStyle for a sheet built at run time, allowed after Freeze.
func RegisterRuntimeStyle(name string, fn func(style.Theme) string, opts ...Option) *Style {
	return registerStyle(name, fn, true, opts)
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
	return slices.SortedFunc(maps.Values(entries), func(a, b *Entry) int {
		return strings.Compare(a.Name, b.Name)
	})
}

// EvictTheme drops every entry's caches for hash.
func EvictTheme(hash string) {
	if hash == "" {
		return
	}
	mu.Lock()
	for _, e := range entries {
		e.EvictTheme(hash)
	}
	mu.Unlock()
}

func reset() {
	mu.Lock()
	defer mu.Unlock()
	entries = map[string]*Entry{}
	behaviors = map[string]*BehaviorEntry{}
	behaviorMarkerAttrs = map[string]bool{}
	templates = newTemplates()
	frozen = false
}

type testCleanup interface{ Cleanup(func()) }

// IsolateForTest gives the test an empty registry and restores the original
// when it ends; tests that use it must not run in parallel.
func IsolateForTest(t testCleanup) {
	mu.Lock()
	saved := entries
	savedBehaviors := behaviors
	savedMarkerAttrs := behaviorMarkerAttrs
	savedReserved := reservedBehaviorNames
	savedFrozen := frozen
	savedTemplates := templates
	savedDataBlocks := dataBlocks
	dataBlocks = map[string]dataBlock{}
	entries = map[string]*Entry{}
	behaviors = map[string]*BehaviorEntry{}
	behaviorMarkerAttrs = map[string]bool{}
	templates = newTemplates()
	frozen = false
	reservedBehaviorNames = map[string]bool{}
	mu.Unlock()
	t.Cleanup(func() {
		mu.Lock()
		entries = saved
		behaviors = savedBehaviors
		behaviorMarkerAttrs = savedMarkerAttrs
		reservedBehaviorNames = savedReserved
		templates = savedTemplates
		dataBlocks = savedDataBlocks
		frozen = savedFrozen
		mu.Unlock()
	})
}

func sameEntry(a, b *Entry) bool {
	return a.Name == b.Name &&
		a.Load == b.Load &&
		fnPtr(a.StyleFn) == fnPtr(b.StyleFn)
}

func fnPtr(fn func(style.Theme) string) uintptr {
	return reflect.ValueOf(fn).Pointer()
}

func fnLocation(fn func(style.Theme) string) string {
	pc := reflect.ValueOf(fn).Pointer()
	rf := runtime.FuncForPC(pc)
	if rf == nil {
		return fmt.Sprintf("0x%x", pc)
	}
	file, line := rf.FileLine(pc)
	return fmt.Sprintf("%s (%s:%d)", rf.Name(), file, line)
}

func themeHash(t style.Theme) string { return style.ThemeHash(t) }
