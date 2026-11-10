package docs

import (
	"strings"
	"testing"
	"unicode/utf8"
)

// A topic's summary is its first paragraph. The docs are hard-wrapped, so
// the first line alone stopped mid-sentence; a topic that opens with a
// code block summarized as "```bash", and one that opens with a comment
// block summarized as the comment's second line.
func TestExtractSummaryIsTheFirstParagraph(t *testing.T) {
	cases := map[string]struct{ body, want string }{
		"wrapped paragraph": {
			"# Title\n\nGoFastr is SSR-first: every page is rendered\non first paint, and interactivity is server-driven.\n\nSecond paragraph.\n",
			"GoFastr is SSR-first: every page is rendered on first paint, and interactivity is server-driven.",
		},
		"leading fence": {
			"# CLI\n\n```bash\ngo install example@latest\n```\n\nWithout a Go toolchain, use\nthe install script.\n",
			"Without a Go toolchain, use the install script.",
		},
		"leading comment block": {
			"# Contract\n\n<!--\n  SYNC NOTE: internal.\n  more\n-->\n\nThe runtime contract.\n",
			"The runtime contract.",
		},
		"one-line comment": {
			"# T\n<!-- gofastr:compile -->\nProse here.\n",
			"Prose here.",
		},
		"blockquote": {
			"# T\n\n> **Careful.** Per-user data\n> needs an owner field.\n\nBody.\n",
			"**Careful.** Per-user data needs an owner field.",
		},
		"paragraph ends at a fence": {
			"# T\n\nInstall it:\n```sh\nx\n```\n",
			"Install it:",
		},
		"no prose": {"# T\n\n```go\nx\n```\n", ""},
	}
	for name, tc := range cases {
		if got := extractSummary([]byte(tc.body)); got != tc.want {
			t.Errorf("%s: summary %q, want %q", name, got, tc.want)
		}
	}
}

// The 200-byte cut lands on a rune boundary: cut mid-rune, the summary
// was invalid UTF-8 and reached an MCP client as U+FFFD.
func TestExtractSummaryCutsOnARune(t *testing.T) {
	got := extractSummary([]byte("# T\n\n" + strings.Repeat("a", 199) + "é and more text\n"))
	if !utf8.ValidString(got) {
		t.Fatalf("summary is not valid UTF-8: %q", got)
	}
	if want := strings.Repeat("a", 199) + "…"; got != want {
		t.Fatalf("summary %q, want %q", got, want)
	}
}

// No shipped topic summarizes as markup.
func TestShippedSummariesAreProse(t *testing.T) {
	topics, err := List()
	if err != nil {
		t.Fatal(err)
	}
	for _, tp := range topics {
		for _, bad := range []string{"```", "~~~", "<!--", "SYNC NOTE"} {
			if strings.HasPrefix(tp.Summary, bad) {
				t.Errorf("topic %s summarizes as %q", tp.Name, tp.Summary)
			}
		}
		if strings.HasPrefix(tp.Summary, ">") {
			t.Errorf("topic %s keeps its blockquote marker: %q", tp.Name, tp.Summary)
		}
	}
}
