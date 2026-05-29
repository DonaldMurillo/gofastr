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
