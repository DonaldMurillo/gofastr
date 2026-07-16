package ui

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/style"
)

// Every var(--color-X) a component references must resolve: either a
// canonical ColorSet token (emitted per-theme) or a derived alias from
// style's aliasTokenCSS block. A reference to an undefined token is NOT a
// build error — the hardcoded fallback silently applies, and those fallbacks
// are tuned for light themes, so dark themes render light-on-light hover
// states and similar contrast failures (found live: ui-copy-btn:hover used
// --color-muted, which never existed, giving a near-white button with light
// text on dark themes).
func TestEveryColorTokenReferenceResolves(t *testing.T) {
	defined := map[string]bool{}

	// Canonical tokens: walk the default theme's emitted :root block —
	// includes every ColorSet field plus the alias block.
	theme := style.DefaultTheme()
	re := regexp.MustCompile(`--color-[a-z0-9-]+`)
	for _, tok := range re.FindAllString(theme.CSSCustomProperties(), -1) {
		defined[tok] = true
	}

	// References: every var(--color-X) in this package's component sources.
	refRe := regexp.MustCompile(`var\((--color-[a-z0-9-]+)`)
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	missing := map[string][]string{}
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		src, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		for _, m := range refRe.FindAllStringSubmatch(string(src), -1) {
			if !defined[m[1]] {
				missing[m[1]] = append(missing[m[1]], f)
			}
		}
	}
	for tok, files := range missing {
		t.Errorf("%s referenced but never defined (silently renders its light-theme fallback in every theme) — used in %v; define it in ColorSet or style.aliasTokenCSS", tok, dedupe(files))
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
