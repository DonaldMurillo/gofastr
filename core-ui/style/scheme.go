package style

import (
	"reflect"
	"sort"
	"strings"
)

func schemeCSS(t Theme, opts []string) string {
	dark, code := t.DarkColors, t.DarkCode
	if len(dark) == 0 {
		css := ":root {\n  color-scheme: light;\n}"
		if len(code) > 0 {
			css += "\n" + darkCodeCSS(code)
		}
		return css
	}
	var tokens []tokenField
	walkTokens(reflect.ValueOf(t), "", &tokens)
	light := make(map[string]string, len(tokens))
	for _, kv := range tokens {
		light[kv.Key] = kv.Value
	}
	darkLines, lightLines := paletteLines(dark, code, light)
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

func paletteLines(colors, code, light map[string]string) (darkLines, lightLines []string) {
	darkLines = make([]string, 0, len(colors)+len(code))
	for _, palette := range []struct {
		prefix string
		values map[string]string
	}{
		{"color-", colors},
		{"tk-", code},
	} {
		for _, name := range sortedMapKeys(palette.values) {
			key := palette.prefix + name
			property := "--" + key + ": "
			darkLines = append(darkLines, property+palette.values[name]+";")
			if value, ok := light[key]; ok {
				lightLines = append(lightLines, property+value+";")
			}
		}
	}
	return darkLines, lightLines
}
func darkCodeCSS(code map[string]string) string {
	lines, _ := paletteLines(nil, code, nil)
	var b strings.Builder
	b.WriteString(":root[data-color-scheme=\"dark\"] {\n")
	writeScopeLines(&b, "  ", lines)
	b.WriteString("}\n@media (prefers-color-scheme: dark) {\n  :root:not([data-color-scheme=\"light\"]) {\n")
	writeScopeLines(&b, "    ", lines)
	b.WriteString("  }\n}")
	return b.String()
}
