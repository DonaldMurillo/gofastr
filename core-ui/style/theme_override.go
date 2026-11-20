package style

import (
	"fmt"
	"reflect"
	"slices"
	"sort"
	"strings"
	"sync"
)

// ThemeRef is a handle to a registered theme override.
type ThemeRef struct {
	rec *themeOverrideRecord
}

type themeOverrideRecord struct {
	theme Theme
	once  sync.Once
	hash  string
}

func (r *themeOverrideRecord) contentHash() string {
	r.once.Do(func() { r.hash = ThemeHash(r.theme) })
	return r.hash
}

// Hash returns the override's content address, computing it on first use.
func (r ThemeRef) Hash() string { return r.record().contentHash() }

func (r ThemeRef) record() *themeOverrideRecord {
	if r.rec == nil {
		panic("style: a zero ThemeRef has no theme: take the handle RegisterThemeOverride returns")
	}
	return r.rec
}

// Class returns the CSS class name applied to wrapped subtrees.
func (r ThemeRef) Class() string { return "cui-theme-" + r.record().contentHash() }

var (
	themeOverrideMu sync.Mutex
	themeOverrides  []*themeOverrideRecord
)

// RegisterThemeOverride records a theme override and returns its handle.
func RegisterThemeOverride(t Theme) ThemeRef {
	rec := &themeOverrideRecord{theme: t.Clone()}
	themeOverrideMu.Lock()
	defer themeOverrideMu.Unlock()
	themeOverrides = append(themeOverrides, rec)
	return ThemeRef{rec: rec}
}

// AllThemeOverrides returns a snapshot of every registered theme, keyed by
// hash, each one a Clone.
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
		out[h] = rec.theme.Clone()
	}
	return out
}

// ThemeOverrideCSS emits the class-scoped blocks for one override.
func ThemeOverrideCSS(hash string, t Theme) string {
	var lines []string
	collectTokenDecls(reflect.ValueOf(t), &lines)
	sort.Strings(lines)
	lines = append(lines, knobDecls(t.Knobs)...)
	opts := componentOptionDecls(withDefaultOptions(t.Components))
	lines = append(lines, opts...)
	var b strings.Builder
	fmt.Fprintf(&b, ":root.cui-theme-%s,\n.cui-theme-%s {\n", hash, hash)
	writeScopeLines(&b, "  ", lines)
	b.WriteString("  color: var(--color-text);\n")
	b.WriteString("  background: var(--color-background);\n")
	b.WriteString("  color-scheme: light;\n")
	b.WriteString("}")
	if len(t.DarkColors) == 0 && len(t.DarkCode) == 0 {
		return b.String()
	}
	darkLines := darkScopeLines(t, opts)
	b.WriteString("\n[data-color-scheme=\"dark\"]:root.cui-theme-" + hash + ",\n[data-color-scheme=\"dark\"] .cui-theme-" + hash + " {\n")
	writeScopeLines(&b, "  ", darkLines)
	b.WriteString("}\n")
	b.WriteString("@media (prefers-color-scheme: dark) {\n")
	b.WriteString("  :root.cui-theme-" + hash + ":not([data-color-scheme=\"light\"]),\n  :root:not([data-color-scheme=\"light\"]) .cui-theme-" + hash + " {\n")
	writeScopeLines(&b, "    ", darkLines)
	b.WriteString("  }\n")
	b.WriteString("}")
	return b.String()
}

func darkScopeLines(t Theme, opts []string) []string {
	lines, _ := paletteLines(t.DarkColors, t.DarkCode, nil)
	lines = append(lines, opts...)
	if len(t.DarkColors) > 0 {
		lines = append(lines, "color-scheme: dark;")
	}
	return lines
}

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

// AllThemeOverridesCSS emits every registered override as a concatenated CSS
// block.
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
