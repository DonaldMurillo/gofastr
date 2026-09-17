package style

import (
	"fmt"
	"reflect"
	"slices"
	"sort"
	"strings"
	"sync"
)

// ThemeRef is a handle to a registered theme override. The framework
// emits a `.fui-theme-<hash>` CSS block in app.css that re-declares
// every changed token; wrapping a subtree with this class scopes the
// override to that part of the DOM via the CSS variable cascade.
//
// Apps register an override once at boot:
//
//	var Dark = style.RegisterThemeOverride(darkTheme)
//
// And wrap any subtree:
//
//	ui.Themed(style.Dark, ui.Card{...})
//
// Registration does NOT hash; Hash/Class compute the content address on
// first use. That ordering is what makes the package-level pattern above
// safe: a `var Dark = …` in a library package that does not import
// framework/ui runs during init, before the component-options compiler
// is registered, and a hash computed there would freeze the compiler
// hook with none registered and panic framework/ui's later init. With
// lazy hashing, init order stops mattering for registration; only a
// package-level Class()/ThemeHash call still hashes before the styled
// layer's init (the panic RegisterComponentOptionsCompiler raises names
// that cause).
//
// Hash is content-addressed (sha256 of the override's :root output)
// so registering the same theme twice yields the same hash and class.
type ThemeRef struct {
	rec *themeOverrideRecord
}

// themeOverrideRecord is one registration: the deep-cloned theme as
// registered, plus its content hash computed on first use. Records are
// immutable once RegisterThemeOverride returns, so the lazy hash needs
// no lock beyond its own Once.
type themeOverrideRecord struct {
	theme Theme
	once  sync.Once
	hash  string
}

// contentHash returns the record's ThemeHash, computing it on first use.
func (r *themeOverrideRecord) contentHash() string {
	r.once.Do(func() { r.hash = ThemeHash(r.theme) })
	return r.hash
}

// Hash returns the override's content address, computing it on first
// use. Registering the same theme twice yields the same hash.
func (r ThemeRef) Hash() string { return r.record().contentHash() }

// record refuses the zero ThemeRef with a reason: a handle comes from
// RegisterThemeOverride, and a zero value used by mistake would
// otherwise fail on a nil pointer with nothing to say.
func (r ThemeRef) record() *themeOverrideRecord {
	if r.rec == nil {
		panic("style: a zero ThemeRef has no theme: take the handle RegisterThemeOverride returns")
	}
	return r.rec
}

// Class returns the CSS class name applied to wrapped subtrees:
// `fui-theme-<hash>`.
func (r ThemeRef) Class() string { return "fui-theme-" + r.record().contentHash() }

var (
	themeOverrideMu sync.Mutex
	themeOverrides  []*themeOverrideRecord // registration order; deduped by hash on read
)

// RegisterThemeOverride records a theme override and returns its
// handle. Idempotent by content: the same theme registered twice yields
// the same hash and class, and AllThemeOverrides emits one block for it.
//
// The override is registered against the FULL theme (not "diffs vs
// default"). When emitted, the framework walks the theme and emits
// every token under the override class, the browser's cascade
// handles the actual delta vs the canonical :root.
//
// The theme is deep-cloned BEFORE it is stored, and the hash is
// computed from that clone on first use, never here. Theme travels by
// value, but its maps (DarkColors, DarkCode, Components) are
// references: without the clone a caller-side write after registration
// would change what every page serves while the hash — computed once,
// from the bytes as they were — kept naming the old content, and the
// write could race a request reading the same map. And without the
// lazy hash, the package-level registration pattern taught above would
// hash during a library package's init and freeze the compiler hook
// before framework/ui registers it (see ThemeRef). The hash, when it
// is computed, is ThemeHash, the one canonical content address.
func RegisterThemeOverride(t Theme) ThemeRef {
	t.DarkColors = copyStringMap(t.DarkColors)
	t.DarkCode = copyStringMap(t.DarkCode)
	t.Components = copyStringMap(t.Components)
	rec := &themeOverrideRecord{theme: t}
	themeOverrideMu.Lock()
	defer themeOverrideMu.Unlock()
	themeOverrides = append(themeOverrides, rec)
	return ThemeRef{rec: rec}
}

// AllThemeOverrides returns a snapshot of every registered theme,
// keyed by hash, with the nested maps deep-copied: a caller mutating a
// returned theme changes nothing the process serves. Used by the
// uihost to emit `.fui-theme-<hash>` blocks in app.css.
//
// Each record is hashed HERE, at call time: registration order cannot
// influence the hash (there is none yet), and the same content
// registered twice dedupes to one entry, so the CSS ships once and
// both handles name the same class.
func AllThemeOverrides() map[string]Theme {
	themeOverrideMu.Lock()
	recs := slices.Clone(themeOverrides)
	themeOverrideMu.Unlock()
	out := make(map[string]Theme, len(recs))
	for _, rec := range recs {
		h := rec.contentHash()
		if _, dup := out[h]; dup {
			continue
		}
		t := rec.theme
		t.DarkColors = copyStringMap(t.DarkColors)
		t.DarkCode = copyStringMap(t.DarkCode)
		t.Components = copyStringMap(t.Components)
		out[h] = t
	}
	return out
}

// ThemeOverrideCSS emits the class-scoped blocks for one override:
//
//	.fui-theme-<hash> {
//	  --color-primary: …;
//	  …every typed token…
//	  …the :root-only alias tokens, re-emitted…
//	  …the compiled component options, re-emitted…
//	  color: var(--color-text);
//	  background: var(--color-background);
//	}
//
// and, when the theme carries a dark palette (DarkColors or DarkCode),
// the same declarations under the document's dark scheme:
//
//	[data-color-scheme="dark"] .fui-theme-<hash> { …dark tokens… }
//	@media (prefers-color-scheme: dark) {
//	  :root:not([data-color-scheme="light"]) .fui-theme-<hash> { …dark tokens… }
//	}
//
// The light block re-declares every typed token, AND sets `color` +
// `background` on the wrapper itself. The `color` declaration is
// load-bearing: text inside the wrapper inherits the overridden
// color, so plain `<p>` / `<span>` elements (which don't carry
// their own `color: var(--*)` rule) still pick up the dark theme.
// Descendant components reading `var(--color-primary)` get the
// overridden value via the CSS variable cascade.
//
// # Why the aliases and the component options are re-emitted inside
//
// A custom property's var() references compute at the element the
// declaration sits on, before inheritance. --color-primary-foreground
// and the --hui-* option variables are declared at :root only, so
// without re-declaration a scope with a different palette would
// inherit the ROOT's resolved colours. Every scope block therefore
// carries the alias lines (aliasTokenDecls) and the compiled option
// lines (componentOptionDecls) after its own tokens, rebound to the
// scope's palette.
//
// # Dark mode follows the document, not the wrapper
//
// `data-color-scheme` is written on <html> (the color-scheme
// bootstrap / ui.ThemeToggle), never on the wrapper, so the dark
// blocks key on the document element the same way darkSchemeCSS
// does: the explicit attribute wins, the prefers-color-scheme media
// query is the fallback while the user has not forced light. The
// wrapper's own `color`/`background` from the light block re-resolve
// against the re-declared dark tokens, so no separate paint lines are
// needed in the dark blocks. A scope with NO dark palette stays light
// in dark mode: its light declarations block inheritance, by design —
// a dark section on a light page is a theme WITH a dark palette, not
// an inheritance accident.
func ThemeOverrideCSS(hash string, t Theme) string {
	var lines []string
	collectTokenDecls(reflect.ValueOf(t), &lines)
	sort.Strings(lines)
	lines = append(lines, aliasTokenDecls()...)
	lines = append(lines, componentOptionDecls(t.Components)...)
	var b strings.Builder
	fmt.Fprintf(&b, ".fui-theme-%s {\n", hash)
	writeScopeLines(&b, "  ", lines)
	// The wrapper itself adopts the overridden palette so inherited
	// `color` flows down. Without this, descendants that don't
	// explicitly set color: var(--color-text) inherit from outside
	// the wrapper.
	b.WriteString("  color: var(--color-text);\n")
	b.WriteString("  background: var(--color-background);\n")
	b.WriteString("}")
	if len(t.DarkColors) == 0 && len(t.DarkCode) == 0 {
		return b.String()
	}
	darkLines := darkScopeLines(t)
	b.WriteString("\n[data-color-scheme=\"dark\"] .fui-theme-" + hash + " {\n")
	writeScopeLines(&b, "  ", darkLines)
	b.WriteString("}\n")
	b.WriteString("@media (prefers-color-scheme: dark) {\n")
	b.WriteString("  :root:not([data-color-scheme=\"light\"]) .fui-theme-" + hash + " {\n")
	writeScopeLines(&b, "    ", darkLines)
	b.WriteString("  }\n")
	b.WriteString("}")
	return b.String()
}

// darkScopeLines builds the declaration lines for a scope's dark
// blocks: the dark tokens first (so the aliases and options that
// follow rebind against them), then the same alias and compiled-option
// lines the light block carries. No color/background paint lines: the
// light block's `color: var(--color-text)` re-resolves here against
// the re-declared token.
func darkScopeLines(t Theme) []string {
	lines := make([]string, 0, len(t.DarkColors)+len(t.DarkCode)+16)
	for _, name := range sortedMapKeys(t.DarkColors) {
		lines = append(lines, fmt.Sprintf("--color-%s: %s;", name, t.DarkColors[name]))
	}
	for _, name := range sortedMapKeys(t.DarkCode) {
		lines = append(lines, fmt.Sprintf("--tk-%s: %s;", name, t.DarkCode[name]))
	}
	lines = append(lines, aliasTokenDecls()...)
	lines = append(lines, componentOptionDecls(t.Components)...)
	return lines
}

// sortedMapKeys is the deterministic iteration every map-written CSS
// block here uses (the mapwriter discipline: never range a map while
// writing output).
func sortedMapKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func writeScopeLines(b *strings.Builder, indent string, lines []string) {
	for _, line := range lines {
		b.WriteString(indent)
		b.WriteString(line)
		b.WriteString("\n")
	}
}

// AllThemeOverridesCSS emits every registered override as a
// concatenated CSS block. Sorted by hash for byte-stable output.
func AllThemeOverridesCSS() string {
	all := AllThemeOverrides()
	if len(all) == 0 {
		return ""
	}
	hashes := make([]string, 0, len(all))
	for h := range all {
		hashes = append(hashes, h)
	}
	sort.Strings(hashes)
	var b strings.Builder
	for i, h := range hashes {
		if i > 0 {
			b.WriteString("\n")
		}
		b.WriteString(ThemeOverrideCSS(h, all[h]))
	}
	return b.String()
}
