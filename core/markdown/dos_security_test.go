package markdown

import (
	"strings"
	"testing"
	"time"
)

// renderWithin renders input and fails if it takes longer than budget,
// guarding against super-linear (quadratic) CPU blowup on adversarial
// markdown. The work runs in a goroutine so a runaway render can't hang
// the whole suite.
func renderWithin(t *testing.T, name, input string, budget time.Duration) {
	t.Helper()
	done := make(chan struct{})
	go func() {
		_ = RenderHTML(input)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(budget):
		t.Fatalf("SECURITY: [markdown] %s exceeded %s on %d-byte input — super-linear blowup (CPU DoS).", name, budget, len(input))
	}
}

// TestMarkdown_NestedBlockquoteBounded verifies that a long run of '>'
// blockquote prefixes does not cause quadratic re-parsing. Attack:
// "> > > ... x" with tens of thousands of nesting levels in one request.
func TestMarkdown_NestedBlockquoteBounded(t *testing.T) {
	// Happy path: a normal shallow blockquote still renders.
	if got := string(RenderHTML("> hello\n> world")); !strings.Contains(got, "<blockquote>") {
		t.Fatalf("expected blockquote in output, got: %s", got)
	}
	// Attack: ~80 KB of nested blockquote prefixes. With O(n^2) behaviour
	// this burns multiple seconds; a bounded renderer finishes well under
	// the budget.
	attack := strings.Repeat("> ", 40000) + "x"
	renderWithin(t, "nested blockquote", attack, 1500*time.Millisecond)
}

// TestMarkdown_UnmatchedEmphasisBounded verifies that many unmatched
// emphasis delimiters do not cause quadratic closing-delimiter scans.
// Attack: "____...____x" (a long run of underscores with no closer).
func TestMarkdown_UnmatchedEmphasisBounded(t *testing.T) {
	// Happy path: matched emphasis still renders.
	if got := string(RenderHTML("*hi*")); !strings.Contains(got, "<em>hi</em>") {
		t.Fatalf("expected <em> in output, got: %s", got)
	}
	// Attack: ~200 KB of unmatched delimiters.
	attack := strings.Repeat("_", 200000) + "x"
	renderWithin(t, "unmatched emphasis", attack, 1500*time.Millisecond)
}

// TestMarkdown_NestedInlineBounded verifies that deeply nested inline
// constructs (links and emphasis) do not drive renderInline into
// unbounded recursion — a stack-exhaustion / CPU DoS. Attack shapes:
// nested links "[[[...x...](u)](u)](u)" and nested emphasis
// "*** ... ***x*** ... ***".
func TestMarkdown_NestedInlineBounded(t *testing.T) {
	// Happy path: a normal nested link/emphasis still renders.
	if got := string(RenderHTML("[*hi*](u)")); !strings.Contains(got, "<a href=\"u\"><em>hi</em></a>") {
		t.Fatalf("expected nested link/em in output, got: %s", got)
	}

	// Attack 1: ~200k levels of nested link text. parseLink matches the
	// balanced brackets so renderInline recurses once per level.
	n := 200000
	nestedLinks := strings.Repeat("[", n) + "x" + strings.Repeat("](u)", n)
	renderWithin(t, "nested links", nestedLinks, 1500*time.Millisecond)

	// Attack 2: deeply nested single-char emphasis. Each matched pair
	// recurses on its inner content.
	nestedEm := strings.Repeat("*", n) + "x" + strings.Repeat("*", n)
	renderWithin(t, "nested emphasis", nestedEm, 1500*time.Millisecond)
}
