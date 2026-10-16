package uihost

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"slices"
	"testing"

	"github.com/chromedp/chromedp"

	"github.com/DonaldMurillo/gofastr/core-ui/registry"
	"github.com/DonaldMurillo/gofastr/core-ui/runtime/minify"
	_ "github.com/DonaldMurillo/gofastr/framework/ui" // registers every component style
	"github.com/DonaldMurillo/gofastr/framework/ui/theme"
	"github.com/DonaldMurillo/gofastr/internal/chromedptest"
)

// TestMinifiedCSSParsesTheSame hands Chrome every component sheet and the
// app sheet twice, as written and as minified, and compares the rule trees
// the parser builds. A string comparison against hand-written expectations
// only proves the minifier does what its author thought CSS means; this
// asks the engine that will read the bytes. A dropped space that turns
// `and (` into the function `and(`, or joins two values, changes or drops
// a rule and shows up here.
func TestMinifiedCSSParsesTheSame(t *testing.T) {
	if testing.Short() {
		t.Skip("browser test")
	}
	th := theme.Default()
	sheets := map[string]string{"app.css": newTestUIHost().AppCSSFor(th)}
	for _, e := range registry.All() {
		sheets[e.Name] = e.StyleFn(th)
	}
	if len(sheets) < 50 {
		t.Fatalf("only %d sheets: framework/ui did not register its styles", len(sheets))
	}

	ctx := chromedptest.Context(t)
	if err := chromedp.Run(ctx, chromedp.Navigate("about:blank")); err != nil {
		t.Fatal(err)
	}
	for _, name := range sortedKeys(sheets) {
		raw := sheets[name]
		min := minify.CSS(raw)
		want, got := parseInChrome(t, ctx, raw), parseInChrome(t, ctx, min)
		if len(want) == 0 && raw != "" {
			t.Errorf("%s: Chrome parsed no rules from the source sheet", name)
			continue
		}
		if len(got) != len(want) {
			t.Errorf("%s: %d rules minified, %d in the source", name, len(got), len(want))
			continue
		}
		for i := range want {
			if got[i] != want[i] {
				t.Errorf("%s rule %d differs\nsource:   %s\nminified: %s", name, i, want[i], got[i])
				break
			}
		}
	}
}

// parseInChrome returns the serialization of every top-level rule Chrome
// builds from css. Nested rules serialize inside their parent's cssText.
//
// Chrome canonicalizes most values, but a value holding var() and every
// custom property keep their source text, so `var(--x, 4rem)` and
// `var(--x,4rem)` serialize apart while tokenizing the same. tokenText
// folds exactly the whitespace CSS syntax ignores: comments, runs, and
// the space beside a comma or a parenthesis and after a colon, outside strings. Spaces
// between two values survive it, so a joined value still fails.
func parseInChrome(t *testing.T, ctx context.Context, css string) []string {
	t.Helper()
	lit, err := json.Marshal(css)
	if err != nil {
		t.Fatal(err)
	}
	var rules []string
	expr := fmt.Sprintf(`(() => {
		const tokenText = (s) => {
			let out = "", sp = false;
			for (let i = 0; i < s.length; i++) {
				const c = s[i];
				if (c === '"' || c === "'") {
					let j = i + 1;
					while (j < s.length && s[j] !== c) j += s[j] === "\\" ? 2 : 1;
					if (sp && out && !/[,(:]$/.test(out)) out += " ";
					sp = false;
					out += s.slice(i, j + 1);
					i = j;
					continue;
				}
				if (c === "/" && s[i + 1] === "*") {
					const end = s.indexOf("*/", i + 2);
					i = end < 0 ? s.length : end + 1;
					sp = true;
					continue;
				}
				if (/\s/.test(c)) { sp = true; continue; }
				if (sp && out && !/[,(:]$/.test(out) && c !== "," && c !== ")") out += " ";
				sp = false;
				out += c;
			}
			return out;
		};
		const s = new CSSStyleSheet();
		s.replaceSync(%s);
		return Array.from(s.cssRules, r => tokenText(r.cssText));
	})()`, lit)
	if err := chromedp.Run(ctx, chromedp.Evaluate(expr, &rules)); err != nil {
		t.Fatal(err)
	}
	return rules
}

func sortedKeys(m map[string]string) []string {
	return slices.Sorted(maps.Keys(m))
}
