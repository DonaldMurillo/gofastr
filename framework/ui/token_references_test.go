package ui

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/registry"
	"github.com/DonaldMurillo/gofastr/core-ui/style"
)

// Every var(--color-X) a component references must resolve to a token
// the theme actually emits (a canonical ColorSet field). A reference
// to an undefined token is NOT a build error. The hardcoded fallback
// silently applies, and those fallbacks are tuned for light themes,
// so dark themes render light-on-light hover states and similar
// contrast failures (found live: ui-copy-btn:hover once referenced a
// muted-surface token that never existed, giving a near-white button
// with light text on dark themes).
func TestEveryColorTokenReferenceResolves(t *testing.T) {
	defined := map[string]bool{}

	// Canonical tokens: walk the default theme's emitted :root block,
	// which includes every ColorSet field.
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
		t.Errorf("%s referenced but never defined (silently renders its light-theme fallback in every theme) — used in %v; define it in ColorSet", tok, dedupe(files))
	}
}

// The retired alias tokens (--color-muted, --color-warn, …) are gone:
// no theme emits them and no reader may reference one. A reintroduced
// alias fails HERE, not as a silently-falling-back contrast bug on a
// dark theme. Two surfaces are gated: every registered stylesheet
// (a sheet can build its CSS dynamically, where a source scan cannot
// see the reference) and every non-test Go/JS source under the trees
// that ship the design system and its consumers. Test files are
// exempt: asserting a name is absent requires writing it.
func TestNoRemovedAliasTokenReferences(t *testing.T) {
	names := []string{
		"--color-muted", "--color-surface-hover", "--color-border-subtle",
		"--color-border-hover", "--color-primary-hover", "--color-primary-foreground",
		"--color-ring", "--color-warn", "--color-warn-soft", "--color-warn-strong",
	}
	// \b after the alternation keeps --color-warning out of the
	// --color-warn probe (a word char follows "warn" there); the
	// prefix names (warn, warn-soft…) are all banned, so a boundary
	// hit inside another banned name is still a hit.
	re := regexp.MustCompile(`--color-(muted|surface-hover|border-subtle|border-hover|primary-hover|primary-foreground|ring|warn|warn-soft|warn-strong)\b`)
	referenced := map[string][]string{}
	note := func(where string, body string) {
		for _, m := range re.FindAllString(body, -1) {
			referenced[m] = append(referenced[m], where)
		}
	}

	// Registered sheets: dynamic CSS a source scan cannot see.
	theme := style.DefaultTheme()
	for _, e := range registry.All() {
		note("sheet "+e.Name, e.CSSFor(theme))
	}

	// Sources: the design-system trees and every consumer the repo ships.
	root := filepath.Join("..", "..")
	for _, dir := range []string{"framework", "core-ui", "battery", "kiln", "cmd/gofastr", "examples"} {
		err := filepath.WalkDir(filepath.Join(root, dir), func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				if strings.HasPrefix(d.Name(), ".") {
					return fs.SkipDir
				}
				return nil
			}
			name := d.Name()
			if !(strings.HasSuffix(name, ".go") || strings.HasSuffix(name, ".js")) ||
				strings.HasSuffix(name, "_test.go") {
				return nil
			}
			src, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			rel, _ := filepath.Rel(root, path)
			note(rel, string(src))
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	for _, name := range names {
		if files, ok := referenced[name]; ok {
			t.Errorf("%s is referenced again (%v) — it is a removed alias; use the canonical ColorSet token", name, dedupe(files))
		}
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
