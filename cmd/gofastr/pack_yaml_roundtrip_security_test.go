package main

import (
	"testing"

	coreyaml "github.com/DonaldMurillo/gofastr/core/yaml"
)

// The invariant, stated end-to-end rather than as a predicate: for any key,
// pack either REFUSES it or emits a file core/yaml can read back. Never a
// third outcome.
//
// yamlKeyRejectReason's table checks that each branch fires for the right
// reason. This checks the property that motivates having branches at all,
// and it is the test that would have caught the quote bypass: `it's` passed
// every branch, and the emitted `it's: "It's #1"` was unreadable because the
// key's apostrophe desynced the comment scanner from the correctly-quoted
// value. No predicate-level assertion covers an interaction like that.
//
// A new writer path, a new key source, or a new parser rule that the reject
// list has not learned shows up here.
func TestPackNeverEmitsAnUnreadableFile(t *testing.T) {
	keys := []string{
		// Shapes the guard is expected to refuse.
		"", "a\nb", "a\rb", "a:b", "a#b", "a[b", "a]b", "a{b", "a}b",
		"- a", "a\tb", " a", "a ", `"status`, "'status", "it's", `a"b`,
		`"quoted"`, "'quoted'",
		// Shapes that must round-trip untouched.
		"name", "created_at", "data-label", "primary-fg", "text-muted",
		"icon", "-a", "a-b", "a.b", "a b", "Title", "id", "C", "x1",
		// Awkward but legitimate.
		"a/b", "a|b", "a?b", "a!b", "a@b", "a%b", "a&b", "a*b", "a+b",
		"a=b", "a<b", "a>b", "a(b)", "café", "日本語",
	}
	for _, key := range keys {
		t.Run(keyLabel(key), func(t *testing.T) {
			// Values are deliberately hostile too: the bypass that motivated
			// this test only appeared when a quoted VALUE met a quote-bearing
			// key on the same line.
			for _, val := range []string{"open", `It's #1`, `a "b" c`, "x # y"} {
				bp := Blueprint{
					App: BlueprintApp{Name: "probe", Module: "example.com/probe"},
					Seed: []BlueprintSeedEntity{{
						Entity: "notes",
						Rows:   []map[string]any{{key: val}},
					}},
				}
				out, err := encodeBlueprintYAML(bp)
				if err != nil {
					continue // refused: the other acceptable outcome
				}
				if _, err := coreyaml.Parse(out); err != nil {
					t.Fatalf("pack accepted key %q with value %q and emitted a file "+
						"core/yaml cannot read (%v):\n%s", key, val, err, out)
				}
			}
		})
	}
}

func keyLabel(k string) string {
	if k == "" {
		return "empty"
	}
	out := make([]rune, 0, len(k))
	for _, r := range k {
		switch r {
		case '\n':
			out = append(out, '\\', 'n')
		case '\r':
			out = append(out, '\\', 'r')
		case '\t':
			out = append(out, '\\', 't')
		case ' ':
			out = append(out, '_')
		default:
			out = append(out, r)
		}
	}
	return string(out)
}
