package style

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"reflect"
	"regexp"
	"sort"
	"strings"
)

// ThemeHash is the canonical content address of a theme: a short digest of
// the :root custom properties it emits plus a canonical serialization of
// its Components map, so two themes that produce identical CSS hash
// identically — exactly the equivalence callers want: a theme differing
// only in its Name changes no pixel and should not bust a cache — while
// two themes that differ only in options hash apart EVEN where no
// component-options compiler is registered (a binary built on
// framework/uihost alone links none; without the canonical block its
// option-different themes would collide and the second registration
// would be dropped as a duplicate).
//
// This is the single implementation. Anything keying a cache, a URL, or an
// asset version on "which theme is this" must call it rather than hashing
// the theme itself: CSSCustomProperties() is byte-stable (its lines are
// sorted), whereas struct field order and unexported state are not.
//
// Six bytes is 48 bits, ample for distinguishing the handful of themes a
// process serves, and short enough to sit in a query string.
func ThemeHash(t Theme) string {
	// The options join the fingerprint in their FLAT form, over the
	// compiler-independent token CSS (tokenCSS, not CSSCustomProperties):
	// the compiled block only exists once a compiler is registered, and a
	// theme's identity must be the same in every binary, whichever layers
	// it links. Hashing therefore never touches the compiler hook, so a
	// hash computed during init cannot freeze it. sortedMapKeys is the
	// mapwriter discipline.
	var b strings.Builder
	b.WriteString(t.tokenCSS())
	b.WriteString("\n/* components */\n")
	for _, k := range sortedMapKeys(t.Components) {
		b.WriteString(k)
		b.WriteString("=")
		b.WriteString(t.Components[k])
		b.WriteString("\n")
	}
	return CSSFingerprint(b.String())
}

// CSSFingerprint is the content address of an arbitrary block of CSS, in the
// same width and encoding ThemeHash uses.
//
// Callers keying a cacheable URL must fingerprint the WHOLE response body, not
// just the theme it derives from. A stylesheet that also carries custom CSS or
// contributed fragments is not identified by its palette: reusing a
// palette-only key for it would let a browser serve one release's bytes under
// another release's URL.
func CSSFingerprint(css string) string {
	sum := sha256.Sum256([]byte(css))
	return hex.EncodeToString(sum[:6])
}

// tokenRefRe matches token references like {colors.primary}.
var tokenRefRe = regexp.MustCompile(`\{([a-zA-Z0-9_-]+\.[a-zA-Z0-9_-]+)\}`)

// ResolveAll replaces {category.name} references with their CSS-var
// equivalents. Always emits `var(--<category>-<name>)`, never the
// literal value, to keep section-level theme overrides working via
// the CSS cascade.
//
// Example: ResolveAll("padding: {spacing.md} {spacing.lg}") →
//
//	"padding: var(--spacing-md) var(--spacing-lg)"
func (t Theme) ResolveAll(s string) string {
	return tokenRefRe.ReplaceAllStringFunc(s, func(match string) string {
		inner := match[1 : len(match)-1]
		parts := strings.SplitN(inner, ".", 2)
		if len(parts) != 2 {
			return match
		}
		prefix := categoryPrefix(parts[0])
		if prefix == "" {
			return match
		}
		return "var(--" + prefix + "-" + parts[1] + ")"
	})
}

// categoryPrefix maps token reference category names to their CSS
// variable prefix. Singular and plural forms are both accepted for
// authoring ergonomics.
func categoryPrefix(category string) string {
	switch strings.ToLower(category) {
	case "colors", "color":
		return "color"
	case "spacing":
		return "spacing"
	case "radii", "radius":
		return "radii"
	case "fonts", "font":
		return "font"
	case "breakpoints", "breakpoint":
		return "breakpoint"
	case "shadows", "shadow":
		return "shadow"
	case "zindex", "z":
		return "z"
	case "durations", "duration":
		return "duration"
	case "easings", "easing":
		return "easing"
	case "typography", "text":
		return "text"
	case "code", "tk":
		return "tk"
	}
	return ""
}

// ResolveColor returns `var(--color-<name>)` for a named color.
// Always a CSS variable reference, never the literal value.
func (t Theme) ResolveColor(name string) string {
	return "var(--color-" + name + ")"
}

// ResolveSpacing returns `var(--spacing-<name>)`.
func (t Theme) ResolveSpacing(name string) string {
	return "var(--spacing-" + name + ")"
}

// ResolveRadius returns `var(--radii-<name>)`.
func (t Theme) ResolveRadius(name string) string {
	return "var(--radii-" + name + ")"
}

// CSSCustomProperties generates the :root { --color-...; ... } block
// from the theme. Walks every typed token field of every set on the
// Theme via reflection, emits a CSS custom property per token.
// Output is byte-stable: fields enumerated in struct order, values
// formatted consistently.
//
// For an app's AppTheme that embeds Theme + extends with extra
// fields, callers can use CSSCustomPropertiesOf(any) on the outer
// struct to include the embedded extensions.
func (t Theme) CSSCustomProperties() string {
	css := CSSCustomPropertiesOf(t)
	if compiled := t.compiledOptionsCSS(); compiled != "" {
		css += "\n" + compiled
	}
	if dark := darkSchemeCSS(t.DarkColors, t.DarkCode); dark != "" {
		css += "\n" + dark
	}
	return css
}

// tokenCSS is CSSCustomProperties without the compiled component
// options: the tokens and the dark blocks, which depend on the theme
// alone. ThemeHash fingerprints this plus the options in their flat
// form, so a theme has the same identity in every binary, whether
// or not a compiler is linked; the compiled block is a function of
// the options and the linked layer, not part of what the theme is.
func (t Theme) tokenCSS() string {
	css := CSSCustomPropertiesOf(t)
	if dark := darkSchemeCSS(t.DarkColors, t.DarkCode); dark != "" {
		css += "\n" + dark
	}
	return css
}

// compiledOptionsCSS is the :root block of compiled component options.
// A theme with options of its own emits those; a theme with NONE emits
// the styled layer's registered default set instead — the :root floor.
// Without it, every host whose theme carries no Components (a bare
// style.DefaultTheme, the `gofastr theme init` scaffold, a host with no
// App.Theme) would emit no option variables at all and the component
// rules consuming them (`.fui-button--primary { background:
// var(--fui-button-primary-bg) }`) would resolve to nothing: an
// uncoloured, unshaped button. The floor is deliberately ROOT-only:
// scope blocks (ThemeOverrideCSS) emit a theme's own options or
// nothing, because a scoped theme with no options inherits its
// parent's variables — that is the nesting contract. With no compiler
// registered, or no defaults, the block is "" as before.
//
// The floor does not change a theme's identity: ThemeHash fingerprints
// the flattened options directly, so an optionless theme still hashes
// as optionless in every binary — the compiled block (floor included)
// is a function of the linked layer, not part of what the theme is.
func (t Theme) compiledOptionsCSS() string {
	// The compiled component options join the root block AFTER the
	// tokens they reference: a declaration like
	// --fui-button-bg: var(--color-primary) computes its var() at the
	// element it is declared on, so it must be re-declared at every
	// theme boundary (ThemeOverrideCSS does the scoped half) to pick up
	// each scope's palette instead of carrying the root's colours into
	// it. Sorted by name for the byte-stable output ThemeHash needs.
	opts := componentOptionDecls(t.Components)
	if len(opts) == 0 {
		opts = componentOptionDecls(componentCompilerDefaults())
	}
	if len(opts) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString(":root {\n")
	for _, line := range opts {
		b.WriteString("  ")
		b.WriteString(line)
		b.WriteString("\n")
	}
	b.WriteString("}")
	return b.String()
}

// componentCompilerDefaults snapshots the registered default option
// set under the lock; nil when no compiler (or no defaults) is
// registered.
func componentCompilerDefaults() map[string]string {
	componentCompiler.mu.Lock()
	defer componentCompiler.mu.Unlock()
	return componentCompiler.defaults
}

// DarkSchemeCSS emits the dark-scheme token overrides for a theme's DarkColors
// map (token name → CSS value), or "" when empty. Two selectors cover both ways
// the scheme is chosen: an explicit `data-color-scheme="dark"` on <html> (set by
// a ui.ThemeToggle / the color-scheme bootstrap) and the OS preference (unless
// the user has explicitly forced light). Both re-declare the same tokens, so any
// surface emitting the theme CSS recolors via the CSS-variable cascade. `color`
// + `background-color` are set on the scope so bare text/elements without their
// own token rule still flip.
func DarkSchemeCSS(dark map[string]string) string {
	return darkSchemeCSS(dark, nil)
}

// darkSchemeCSS is DarkSchemeCSS plus the optional dark syntax
// palette (Theme.DarkCode): code entries emit `--tk-<name>` lines in
// the same two dark-scheme blocks. The `color` + `background-color`
// scope lines only accompany a color re-declaration, a code-only
// dark palette shouldn't imply the page itself flips.
func darkSchemeCSS(dark, code map[string]string) string {
	if len(dark) == 0 && len(code) == 0 {
		return ""
	}
	sortedKeys := func(m map[string]string) []string {
		names := make([]string, 0, len(m))
		for name := range m {
			names = append(names, name)
		}
		sort.Strings(names)
		return names
	}
	colorNames, codeNames := sortedKeys(dark), sortedKeys(code)
	writeDecls := func(b *strings.Builder, indent string) {
		for _, name := range colorNames {
			fmt.Fprintf(b, "%s--color-%s: %s;\n", indent, name, dark[name])
		}
		for _, name := range codeNames {
			fmt.Fprintf(b, "%s--tk-%s: %s;\n", indent, name, code[name])
		}
	}
	var b strings.Builder
	b.WriteString(":root[data-color-scheme=\"dark\"] {\n")
	writeDecls(&b, "  ")
	if len(dark) > 0 {
		b.WriteString("  color: var(--color-text);\n  background-color: var(--color-background);\n")
	}
	b.WriteString("}\n")
	b.WriteString("@media (prefers-color-scheme: dark) {\n")
	b.WriteString("  :root:not([data-color-scheme=\"light\"]) {\n")
	writeDecls(&b, "    ")
	b.WriteString("  }\n")
	b.WriteString("}")
	return b.String()
}

// CSSCustomPropertiesOf walks any struct (including the app's
// embedding Theme) and emits a :root { --…: ...; } block for every
// typed token field. Used by SSG and the live :root emission for
// app-extended themes.
func CSSCustomPropertiesOf(theme any) string {
	var lines []string
	collectTokenDecls(reflect.ValueOf(theme), &lines)
	sort.Strings(lines)
	var b strings.Builder
	b.WriteString(":root {\n")
	for _, line := range lines {
		b.WriteString("  ")
		b.WriteString(line)
		b.WriteString("\n")
	}
	b.WriteString("}")
	return b.String()
}

// tokenKV is a single typed token flattened to its CSS custom-property
// identifier (Key, WITHOUT the leading "--") and the exact value string
// the :root block emits after the colon. Both CSSCustomPropertiesOf and
// ThemeToTokens produce these via the single walkTokens walk, so the two
// flatteners can never disagree about which tokens exist or their values.
type tokenKV struct {
	Key, Value string
}

// walkTokens walks v, recursing into struct fields and dereferencing
// pointers/interfaces, and appends every emittable typed token as a
// tokenKV. The Key is the CSS custom-property identifier WITHOUT the
// leading "--" ("color-primary", "spacing-md", "duration-fast",
// "tk-kw"); the Value is exactly what CSSCustomProperties emits after
// the colon ("#4F46E5", "8px", "150ms", "var(--color-code-text)").
//
// This is the single shared reflection walk. collectTokenDecls (the
// :root emitter) and ThemeToTokens (the token map) both route through
// it, which is what guarantees a theme flattened to a map and re-applied
// reproduces identical CSS, the round-trip property ThemeHash asserts.
// The two flatteners that disagree is precisely the bug the token-map
// layer exists to prevent, so the walk is shared rather than duplicated.
func walkTokens(v reflect.Value, out *[]tokenKV) {
	for v.Kind() == reflect.Pointer || v.Kind() == reflect.Interface {
		if v.IsNil() {
			return
		}
		v = v.Elem()
	}
	if v.Kind() != reflect.Struct {
		return
	}
	if key, val, ok := tokenPair(v); ok {
		*out = append(*out, tokenKV{key, val})
		return
	}
	for _, f := range v.Fields() {
		if !f.CanInterface() {
			continue
		}
		walkTokens(f, out)
	}
}

// tokenPair returns the (cssVarName, cssValue) pair for a typed token
// struct, matching exactly what the :root emitter writes as
// "--<cssVarName>: <cssValue>;". ok is false for any non-token struct
// (the caller recurses into its fields) and for a token the emitter
// skips, one with an empty Name, or an optional CodeColor whose Value
// is unset. Centralizing the var-naming convention and the skip rules
// in one place keeps emission and the token map in lockstep: changing
// the prefix for one category here changes it for both consumers at
// once.
func tokenPair(v reflect.Value) (key, value string, ok bool) {
	switch t := v.Interface().(type) {
	case Color:
		if t.Name == "" {
			return "", "", false
		}
		return "color-" + t.Name, t.Value, true
	case Spacing:
		if t.Name == "" {
			return "", "", false
		}
		return "spacing-" + t.Name, fmt.Sprintf("%dpx", t.Value), true
	case Radius:
		if t.Name == "" {
			return "", "", false
		}
		return "radii-" + t.Name, fmt.Sprintf("%dpx", t.Value), true
	case Font:
		if t.Name == "" {
			return "", "", false
		}
		return "font-" + t.Name, t.Value, true
	case Breakpoint:
		if t.Name == "" {
			return "", "", false
		}
		return "breakpoint-" + t.Name, fmt.Sprintf("%dpx", t.Value), true
	case Shadow:
		if t.Name == "" {
			return "", "", false
		}
		return "shadow-" + t.Name, t.Value, true
	case ZIndexValue:
		if t.Name == "" {
			return "", "", false
		}
		return "z-" + t.Name, fmt.Sprintf("%d", t.Value), true
	case Duration:
		if t.Name == "" {
			return "", "", false
		}
		return "duration-" + t.Name, t.FormattedValue(), true
	case Easing:
		if t.Name == "" {
			return "", "", false
		}
		return "easing-" + t.Name, t.Value, true
	case FontSize:
		if t.Name == "" {
			return "", "", false
		}
		return "text-" + t.Name, t.Value, true
	case CodeColor:
		// Optional token: emitted only when fully set (an unset slot
		// leaves the component-CSS fallback palette in charge).
		if t.Name == "" || t.Value == "" {
			return "", "", false
		}
		return "tk-" + t.Name, t.Value, true
	}
	return "", "", false
}

// collectTokenDecls walks any struct value (including embedded structs)
// and records `--<category>-<name>: <value>;` declarations for every
// typed token it finds. Thin formatter over walkTokens: the walk and
// the var-naming live in one place (walkTokens / tokenPair); this just
// stitches the "--" / ": " / ";" punctuation the :root block needs.
func collectTokenDecls(v reflect.Value, out *[]string) {
	var pairs []tokenKV
	walkTokens(v, &pairs)
	for _, p := range pairs {
		*out = append(*out, "--"+p.Key+": "+p.Value+";")
	}
}
