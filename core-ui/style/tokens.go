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

// ThemeHash is the canonical content address of a theme.
func ThemeHash(t Theme) string {
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
func CSSFingerprint(css string) string {
	sum := sha256.Sum256([]byte(css))
	return hex.EncodeToString(sum[:6])
}

var tokenRefRe = regexp.MustCompile(`\{([a-zA-Z0-9_-]+\.[a-zA-Z0-9_-]+)\}`)

// ResolveAll replaces {category.name} references with their CSS-var
// equivalents.
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

func categoryPrefix(category string) string {
	switch strings.ToLower(category) {
	case "colors", "color":
		return "color"
	case "spacing":
		return "spacing"
	case "radii", "radius":
		return "radii"
	case "strokes", "stroke":
		return "stroke"
	case "leading":
		return "leading"
	case "tracking":
		return "tracking"
	case "opacities", "opacity":
		return "opacity"
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
	case "size", "sizes":
		return "size"
	case "font-weight", "fontweights", "weight":
		return "font-weight"
	}
	return ""
}

var tokenCategories = []string{
	"color", "spacing", "radii", "stroke", "leading", "tracking", "opacity",
	"font", "breakpoint", "shadow", "z",
	"duration", "easing", "text", "tk", "size", "font-weight",
}

// TokenCategory returns the category of a token key, the prefix its type
// emits.
func TokenCategory(key string) string {
	best := ""
	for _, c := range tokenCategories {
		if len(c) > len(best) && strings.HasPrefix(key, c+"-") {
			best = c
		}
	}
	if best != "" {
		return best
	}
	if i := strings.Index(key, "-"); i >= 0 {
		return key[:i]
	}
	return key
}

// ResolveColor returns `var(--color-<name>)` for a named color.
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

// CSSCustomProperties generates the :root { --color-...; ...
func (t Theme) CSSCustomProperties() string {
	css := CSSCustomPropertiesOf(t)
	if knobs := knobsCSS(t.Knobs); knobs != "" {
		css += "\n" + knobs
	}
	if compiled := t.compiledOptionsCSS(); compiled != "" {
		css += "\n" + compiled
	}
	return css + "\n" + schemeCSS(t, componentOptionDecls(withDefaultOptions(t.Components)))
}

func (t Theme) tokenCSS() string {
	css := CSSCustomPropertiesOf(t)
	if knobs := knobsCSS(t.Knobs); knobs != "" {
		css += "\n" + knobs
	}
	css += "\n" + schemeCSS(t, nil)
	return css
}

func (t Theme) compiledOptionsCSS() string {
	opts := componentOptionDecls(withDefaultOptions(t.Components))
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

func knobsCSS(knobs map[string]string) string {
	lines := knobDecls(knobs)
	if len(lines) == 0 {
		return ""
	}
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

func knobDecls(knobs map[string]string) []string {
	lines := make([]string, 0, len(knobs))
	for _, k := range sortedMapKeys(knobs) {
		lines = append(lines, "--"+k+": "+knobs[k]+";")
	}
	return lines
}

func componentCompilerDefaults() map[string]string {
	componentCompiler.mu.Lock()
	defer componentCompiler.mu.Unlock()
	return componentCompiler.defaults
}

// CSSCustomPropertiesOf walks any struct (including the app's embedding Theme)
// and emits a :root { --….
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

type tokenKV struct {
	Key, Value string
}

func walkTokens(v reflect.Value, out *[]tokenKV) {
	for v.Kind() == reflect.Pointer || v.Kind() == reflect.Interface {
		if v.IsNil() {
			return
		}
		v = v.Elem()
	}
	if v.Kind() == reflect.Slice {
		for i := range v.Len() {
			walkTokens(v.Index(i), out)
		}
		return
	}
	if v.Kind() != reflect.Struct {
		return
	}
	v = withOptionalDefaults(v)
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
	case Stroke:
		if t.Name == "" || t.Value == "" {
			return "", "", false
		}
		return "stroke-" + t.Name, t.Value, true
	case LineHeight:
		if t.Name == "" || t.Value == "" {
			return "", "", false
		}
		return "leading-" + t.Name, t.Value, true
	case LetterSpacing:
		if t.Name == "" || t.Value == "" {
			return "", "", false
		}
		return "tracking-" + t.Name, t.Value, true
	case Opacity:
		if t.Name == "" || t.Value == "" {
			return "", "", false
		}
		return "opacity-" + t.Name, t.Value, true
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
	case Size:
		if t.Name == "" {
			return "", "", false
		}
		return "size-" + t.Name, t.Value, true
	case FontWeight:
		if t.Name == "" {
			return "", "", false
		}
		return "font-weight-" + t.Name, fmt.Sprintf("%d", t.Value), true
	case CodeColor:
		if t.Name == "" || t.Value == "" {
			return "", "", false
		}
		return "tk-" + t.Name, t.Value, true
	}
	return "", "", false
}

func collectTokenDecls(v reflect.Value, out *[]string) {
	var pairs []tokenKV
	walkTokens(v, &pairs)
	for _, p := range pairs {
		*out = append(*out, "--"+p.Key+": "+p.Value+";")
	}
}
