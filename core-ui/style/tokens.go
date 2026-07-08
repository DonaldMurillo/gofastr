package style

import (
	"fmt"
	"reflect"
	"regexp"
	"sort"
	"strings"
)

// tokenRefRe matches token references like {colors.primary}.
var tokenRefRe = regexp.MustCompile(`\{([a-zA-Z0-9_-]+\.[a-zA-Z0-9_-]+)\}`)

// ResolveAll replaces {category.name} references with their CSS-var
// equivalents. Always emits `var(--<category>-<name>)` — never the
// literal value — to keep section-level theme overrides working via
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
// Always a CSS variable reference — never the literal value.
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
	if dark := darkSchemeCSS(t.DarkColors, t.DarkCode); dark != "" {
		css += "\n" + dark
	}
	return css
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
// scope lines only accompany a color re-declaration — a code-only
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

// collectTokenDecls walks any struct value (including embedded
// structs) and records `--<category>-<name>: <value>;` declarations
// for every typed token it finds. Recursion stops at primitive
// scalars (typed tokens implement their own emission).
func collectTokenDecls(v reflect.Value, out *[]string) {
	for v.Kind() == reflect.Ptr || v.Kind() == reflect.Interface {
		if v.IsNil() {
			return
		}
		v = v.Elem()
	}
	if v.Kind() != reflect.Struct {
		return
	}

	// Handle the token types directly — they know how to render.
	if decl := tokenDecl(v); decl != "" {
		*out = append(*out, decl)
		return
	}

	for i := 0; i < v.NumField(); i++ {
		f := v.Field(i)
		if !f.CanInterface() {
			continue
		}
		collectTokenDecls(f, out)
	}
}

// tokenDecl checks if v is one of the typed token structs and
// returns a "--name: value;" CSS line. Returns "" when v is some
// other struct (caller should recurse into its fields).
func tokenDecl(v reflect.Value) string {
	switch t := v.Interface().(type) {
	case Color:
		if t.Name == "" {
			return ""
		}
		return fmt.Sprintf("--color-%s: %s;", t.Name, t.Value)
	case Spacing:
		if t.Name == "" {
			return ""
		}
		return fmt.Sprintf("--spacing-%s: %dpx;", t.Name, t.Value)
	case Radius:
		if t.Name == "" {
			return ""
		}
		return fmt.Sprintf("--radii-%s: %dpx;", t.Name, t.Value)
	case Font:
		if t.Name == "" {
			return ""
		}
		return fmt.Sprintf("--font-%s: %s;", t.Name, t.Value)
	case Breakpoint:
		if t.Name == "" {
			return ""
		}
		return fmt.Sprintf("--breakpoint-%s: %dpx;", t.Name, t.Value)
	case Shadow:
		if t.Name == "" {
			return ""
		}
		return fmt.Sprintf("--shadow-%s: %s;", t.Name, t.Value)
	case ZIndexValue:
		if t.Name == "" {
			return ""
		}
		return fmt.Sprintf("--z-%s: %d;", t.Name, t.Value)
	case Duration:
		if t.Name == "" {
			return ""
		}
		return fmt.Sprintf("--duration-%s: %s;", t.Name, t.FormattedValue())
	case Easing:
		if t.Name == "" {
			return ""
		}
		return fmt.Sprintf("--easing-%s: %s;", t.Name, t.Value)
	case FontSize:
		if t.Name == "" {
			return ""
		}
		return fmt.Sprintf("--text-%s: %s;", t.Name, t.Value)
	case CodeColor:
		// Optional token: emitted only when fully set (an unset slot
		// leaves the component-CSS fallback palette in charge).
		if t.Name == "" || t.Value == "" {
			return ""
		}
		return fmt.Sprintf("--tk-%s: %s;", t.Name, t.Value)
	}
	return ""
}
