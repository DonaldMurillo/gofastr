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

// TestMarkdown_NonAdvancingBlockquoteTerminates pins the always-advance
// invariant: a line a classifier matches but the handler won't consume
// must not spin the block loop. Found by FuzzRenderHTML: "\f>" is seen
// as a blockquote (TrimSpace treats \f as space) but the consumer can't
// strip the marker, so the parser never advanced: infinite loop + OOM.
func TestMarkdown_NonAdvancingBlockquoteTerminates(t *testing.T) {
	for _, in := range []string{"\f>", "\v>", "\f> a", " >"} {
		renderWithin(t, "non-advancing blockquote", in, 500*time.Millisecond)
	}
}

// TestMarkdown_MalformedTableNoPanic pins that a table whose separator
// row is wider than its header row does not panic the renderer. Found by
// FuzzRenderHTML: "|\n||:" indexed the header-sized align slice with the
// separator's cell count (index out of range → request-goroutine crash).
func TestMarkdown_MalformedTableNoPanic(t *testing.T) {
	for _, in := range []string{"|\n||:", "|\n|:-:|:-:|", "a|b\n|", "|\n|||||"} {
		// A panic here crashes the test goroutine and fails the run.
		_ = RenderHTML(in)
	}
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
// unbounded recursion, a stack-exhaustion / CPU DoS. Attack shapes:
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

// TestMarkdown_UnpairedBacktickTailBounded pins linear cost for an unpaired
// backtick run followed by a long non-backtick tail.
//
// renderInlineDepth's '`' branch called findCodeEnd(input, i) at EVERY
// position of a run that has no closer. findCodeEnd counts the remaining
// run (open), starts scanning at i+open — i.e. at the END of the run — and
// then walks the whole tail looking for a matching run that does not exist.
// It fails, the main loop advanced by ONE byte, and the next position of
// the same run rescanned the same tail. A run of L backticks followed by a
// tail of T non-backtick bytes costs L×T: quadratic. (Fixed by consuming
// the whole unmatched run; see the '`' case in renderInlineDepth for why
// the emphasis branch's noCloser memo cannot be reused here.)
//
// A line BEGINNING with ``` is a fence and never reaches the inline path,
// so the attack prefixes one ordinary character. Attack input is one line,
// so it is one paragraph, one renderInline call.
//
// Measured before the fix existed (M4 Pro):
//
//	run=32000 tail=32000 (64 KiB) → 639 ms   (2.6 ms at 4 KiB, 4× each doubling)
//
// A 1 MiB request body would hold the render goroutine for minutes.
func TestMarkdown_UnpairedBacktickTailBounded(t *testing.T) {
	// Happy path: a paired code span still renders.
	if got := string(RenderHTML("a `code` b")); !strings.Contains(got, "<code>code</code>") {
		t.Fatalf("expected inline code to render, got: %s", got)
	}
	// And an unpaired run still renders literally, not dropped.
	if got := string(RenderHTML("a `` unclosed")); strings.Contains(got, "<code>") {
		t.Fatalf("unpaired backticks must not open a code span: %s", got)
	}

	// Attack: 48 KiB run + 48 KiB tail ≈ 96 KiB on one line. Quadratic
	// behaviour costs well over the budget (≈3.6 s measured); a linear
	// renderer finishes in single-digit milliseconds.
	attack := "a" + strings.Repeat("`", 48*1024) + strings.Repeat("x", 48*1024)
	renderWithin(t, "unpaired backtick run + tail", attack, 1500*time.Millisecond)
}

// TestMarkdown_UnpairedBacktickTailScaling makes the super-linearity itself
// the assertion, independent of machine speed: doubling the input must not
// quadruple the render time. Timing-ratio tests are coarse, so the ratio
// threshold is generous (linear ≈ 2×, quadratic = 4×).
func TestMarkdown_UnpairedBacktickTailScaling(t *testing.T) {
	if testing.Short() {
		t.Skip("timing test")
	}
	sizes := []int{8 * 1024, 16 * 1024, 32 * 1024, 64 * 1024}
	var prev time.Duration
	for i, sz := range sizes {
		in := "a" + strings.Repeat("`", sz) + strings.Repeat("x", sz)
		start := time.Now()
		_ = RenderHTML(in)
		elapsed := time.Since(start)
		t.Logf("run=%d tail=%d took %v", sz, sz, elapsed)
		if i > 0 && prev > 0 {
			ratio := float64(elapsed) / float64(prev)
			if ratio > 3.5 {
				t.Fatalf("SECURITY: [markdown] render time grew %.1fx on a 2x input — super-linear (unpaired backtick run + tail, findCodeEnd rescans the tail per run position)", ratio)
			}
		}
		prev = elapsed
	}
}

// TestMarkdown_DistinctBacktickRunLengthsBounded pins the second-order
// shape: many backtick runs of DISTINCT lengths (1, 2, 3, … K), none with
// a same-length closer. Skipping the whole unmatched run (the fix for the
// single-run quadratic above) still leaves one full-tail scan per distinct
// length: K runs need K(K+1)/2 bytes, so K scans over ~K²/2 bytes cost
// O(n^1.5) — measured 552 ms on a 1 MB document. The backtick-run index
// (buildBacktickRuns) turns closer lookups into binary searches so this
// shape is linear too. A leading '*' also drives findClosingDelim's
// code-span skipping across the same runs.
func TestMarkdown_DistinctBacktickRunLengthsBounded(t *testing.T) {
	distinctRuns := func(k int) string {
		var sb strings.Builder
		sb.WriteString("*")
		for l := 1; l <= k; l++ {
			sb.WriteString(strings.Repeat("`", l))
			sb.WriteString("x")
		}
		return sb.String()
	}
	// Absolute backstop: ~1 MB of adversarial runs renders in budget.
	renderWithin(t, "distinct-length backtick runs", distinctRuns(1414), 1500*time.Millisecond)

	// Machine-independent assertion: quadrupling the input (k ×4 → n ×4)
	// must cost ≈4×, not the 8× of O(n^1.5).
	if testing.Short() {
		t.Skip("timing test")
	}
	var prev time.Duration
	for i, k := range []int{350, 700, 1400} {
		start := time.Now()
		_ = RenderHTML(distinctRuns(k))
		elapsed := time.Since(start)
		t.Logf("k=%d n=%d took %v", k, 4*k*k, elapsed)
		if i > 0 && prev > 0 {
			ratio := float64(elapsed) / float64(prev)
			if ratio > 6 {
				t.Fatalf("SECURITY: [markdown] render time grew %.1fx on a 4x input — super-linear (distinct-length unmatched backtick runs)", ratio)
			}
		}
		prev = elapsed
	}
}
