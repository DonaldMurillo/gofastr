package ui

import (
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/registry"
	"github.com/DonaldMurillo/gofastr/core-ui/style"
)

// Every var(--<family>-X) a component references must resolve to a
// token the theme actually emits, for every family the theme emits
// (color, shadow, font, z, duration, easing, text, spacing, radii, …).
// A reference to an undefined token is NOT a build error. The
// hardcoded fallback silently applies, so a theme override never
// reaches the component: colour fallbacks are tuned for light themes,
// so dark themes render light-on-light hover states (found live:
// ui-copy-btn:hover once referenced a muted-surface token that never
// existed), and a --shadows-sm / --fonts-mono / --zindex-popover
// spelling ignored every Shadows, Fonts and ZIndex override.
//
// The families also cover the misspellings that read as the theme's
// own Go field names (Shadows → --shadows-*, Fonts → --fonts-*,
// ZIndex → --zindex-*), plus --ring-*: none is a family the theme
// emits, so any reference to one is a token that cannot resolve.
func TestEveryThemeTokenReferenceResolves(t *testing.T) {
	defined := map[string]bool{}
	families := map[string]bool{
		"shadows": true, "fonts": true, "zindex": true, "durations": true,
		"easings": true, "colors": true, "radius": true, "ring": true,
	}

	// Canonical tokens: walk the default theme's emitted :root block.
	// Every family a name there starts with is gated.
	theme := style.DefaultTheme()
	declRe := regexp.MustCompile(`(--[a-z][a-z0-9-]*)\s*:`)
	for _, m := range declRe.FindAllStringSubmatch(theme.CSSCustomProperties(), -1) {
		defined[m[1]] = true
		families[strings.SplitN(strings.TrimPrefix(m[1], "--"), "-", 2)[0]] = true
	}
	// --fui-* and --ui-* are per-component override knobs, unset unless
	// a host or Theme.Components sets them: their fallback is the
	// design, not a missed token (the contracts rule GOFASTR1806 holds
	// the same posture).
	delete(families, "fui")
	delete(families, "ui")

	// A sheet may declare a custom property of its own and read it
	// back; such a name resolves wherever its rule applies.
	sheets := map[string]string{}
	for _, e := range registry.All() {
		css := e.CSSFor(theme)
		sheets["sheet "+e.Name] = css
		for _, m := range declRe.FindAllStringSubmatch(css, -1) {
			defined[m[1]] = true
		}
	}

	// References: every var(--family-X) in this package's component
	// sources, core-ui/app's layout sheets, and every registered sheet
	// (a sheet can build its CSS dynamically, where a source scan
	// cannot see the reference).
	refRe := regexp.MustCompile(`var\(\s*(--([a-z][a-z0-9]*)-[a-z0-9-]+)`)
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	layouts, err := filepath.Glob(filepath.Join("..", "..", "core-ui", "app", "*.go"))
	if err != nil {
		t.Fatal(err)
	}
	files = append(files, layouts...)
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		src, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		sheets[f] = string(src)
	}
	missing := map[string][]string{}
	for where, body := range sheets {
		for _, m := range refRe.FindAllStringSubmatch(body, -1) {
			if families[m[2]] && !defined[m[1]] {
				missing[m[1]] = append(missing[m[1]], where)
			}
		}
	}
	for _, tok := range slices.Sorted(maps.Keys(missing)) {
		files := missing[tok]
		slices.Sort(files)
		t.Errorf("%s referenced but never defined (its fallback renders in every theme and no override reaches it) — used in %v; use the token the theme emits, or define it upstream in core-ui/style", tok, dedupe(files))
	}
}

func dedupe(in []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(in))
	for _, s := range in {
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	return out
}
