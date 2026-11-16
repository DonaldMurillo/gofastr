package style

import (
	"reflect"
	"sort"
	"strings"
)

// schemeCSS emits the theme's color-scheme declarations and its dark
// palette: the root blocks keyed on <html data-color-scheme> with the
// prefers-color-scheme fallback, then the subtree blocks a
// data-color-scheme on any other element selects. The rules are
// ui.styling/scheme-is-declared-in-css and
// ui.styling/subtree-scheme-override. opts are the compiled component
// option lines a subtree re-declares; tokenCSS passes nil so a theme's
// identity never depends on the linked compiler.
func schemeCSS(t Theme, opts []string) string {
	dark, code := t.DarkColors, t.DarkCode
	if len(dark) == 0 {
		css := ":root {\n  color-scheme: light;\n}"
		if len(code) > 0 {
			css += "\n" + darkCodeCSS(code)
		}
		return css
	}
	var tokens []tokenKV
	walkTokens(reflect.ValueOf(t), &tokens)
	light := make(map[string]string, len(tokens))
	for _, kv := range tokens {
		light[kv.Key] = kv.Value
	}
	var darkLines, lightLines []string
	for _, name := range sortedMapKeys(dark) {
		darkLines = append(darkLines, "--color-"+name+": "+dark[name]+";")
		if v, ok := light["color-"+name]; ok {
			lightLines = append(lightLines, "--color-"+name+": "+v+";")
		}
	}
	for _, name := range sortedMapKeys(code) {
		darkLines = append(darkLines, "--tk-"+name+": "+code[name]+";")
		if v, ok := light["tk-"+name]; ok {
			lightLines = append(lightLines, "--tk-"+name+": "+v+";")
		}
	}
	// A custom property's var() computes on the element that declares
	// it, so a subtree re-declares every root line that reads another
	// variable: those would otherwise arrive resolved against the root.
	var rebind []string
	for _, kv := range tokens {
		if name, ok := strings.CutPrefix(kv.Key, "color-"); ok && dark[name] != "" {
			continue
		}
		if name, ok := strings.CutPrefix(kv.Key, "tk-"); ok && code[name] != "" {
			continue
		}
		if strings.Contains(kv.Value, "var(") {
			rebind = append(rebind, "--"+kv.Key+": "+kv.Value+";")
		}
	}
	sort.Strings(rebind)
	rebind = append(rebind, knobDecls(t.Knobs)...)
	rebind = append(rebind, opts...)
	paint := []string{"color: var(--color-text);", "background-color: var(--color-background);"}

	var b strings.Builder
	block := func(sel, indent string, groups ...[]string) {
		b.WriteString(indent + sel + " {\n")
		for _, lines := range groups {
			writeScopeLines(&b, indent+"  ", lines)
		}
		b.WriteString(indent + "}\n")
	}
	block(":root", "", []string{"color-scheme: light dark;"})
	block(`:root[data-color-scheme="light"]`, "", []string{"color-scheme: light;"})
	block(`:root[data-color-scheme="dark"]`, "", []string{"color-scheme: dark;"}, darkLines, paint)
	b.WriteString("@media (prefers-color-scheme: dark) {\n")
	block(`:root:not([data-color-scheme="light"])`, "  ", darkLines)
	b.WriteString("}\n")
	block(`:root [data-color-scheme="dark"]`, "", []string{"color-scheme: dark;"}, darkLines, rebind, paint)
	block(`:root [data-color-scheme="light"]`, "", []string{"color-scheme: light;"}, lightLines, rebind, paint)
	return strings.TrimSuffix(b.String(), "\n")
}

// darkCodeCSS is the dark syntax palette of a theme whose page stays
// light: a code-only dark palette does not flip the page, so it carries
// no paint lines and no color-scheme.
func darkCodeCSS(code map[string]string) string {
	lines := make([]string, 0, len(code))
	for _, name := range sortedMapKeys(code) {
		lines = append(lines, "--tk-"+name+": "+code[name]+";")
	}
	var b strings.Builder
	b.WriteString(":root[data-color-scheme=\"dark\"] {\n")
	writeScopeLines(&b, "  ", lines)
	b.WriteString("}\n@media (prefers-color-scheme: dark) {\n  :root:not([data-color-scheme=\"light\"]) {\n")
	writeScopeLines(&b, "    ", lines)
	b.WriteString("  }\n}")
	return b.String()
}
