package markdown

import (
	"os"
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
//
// The sizes moved up 32× when the code-span fix landed, and the reason is
// worth keeping: the fix made this path about 100× faster, and the old
// 8–64 KiB ladder then ran entirely under 200 µs, where a ratio measures
// the scheduler rather than the algorithm. It failed CI at
// 33 µs → 55 µs → 225 µs — "4.1× growth" that is 170 µs of noise. A test
// can be outgrown by the code it guards.
//
// At 256 KiB and up the work is back above the clock's reach. Measured
// after the fix (M4 Pro, best of 5): 256 KiB 730 µs, 1 MiB 3.30 ms,
// 4 MiB 12.2 ms — clean 4× per 4× step.
//
// Verified against the real pre-fix implementation rather than a mutation.
// That distinction cost two wrong attempts and is worth recording: undoing
// the whole-run skip leaves the run index in place (still O(n log n), test
// passes), and replacing the index lookup with a "linear scan" scans RUNS,
// not tail bytes, so it stays linear too. Neither reproduces the old cost
// model, because the fix changed findCodeEnd's shape rather than its
// constant. Restoring core/markdown/inline.go from before the fix does:
//
//	run=262144 took 41.2s
//	run=524288 took 2m47s   → 4.1× on a 2× input, FAIL
//
// The firstShotCeiling below exists because of those numbers: without it a
// regression takes ~3.5 minutes to surface, since the ratio needs two
// measurements and the second is the expensive one. The ceiling is ~2700×
// the correct time at this size, so only a genuine complexity change trips
// it, and it aborts in ~40s instead.
func TestMarkdown_UnpairedBacktickTailScaling(t *testing.T) {
	if testing.Short() {
		t.Skip("timing test")
	}
	// The RATIO half of this test does not work on a shared CI runner, and
	// three attempts is enough to call it.
	//
	//   attempt 1  sizes too small - 33/55/225us, measuring the scheduler
	//   attempt 2  single sample   - 10.9ms then 7.3ms, the LARGER input
	//              faster, one-time allocator cost sitting in the floor
	//   attempt 3  best-of-3       - 4.7/11.3/49.6ms, a 4.4x step where
	//              this machine shows 1.9x
	//
	// Each fix addressed a real defect and CI failed anyway. At 1 MiB the
	// runner's memory behaviour dominates, and no amount of sampling
	// removes a cost that grows with allocation pressure. A ratio
	// reporting 4.4x for linear code is not a weak signal, it is the wrong
	// instrument. The ceiling below is the half that works, and the margin
	// is not close: ~1ms correct, 41s regressed, 3s ceiling. So the
	// ceiling runs everywhere and the ladder is opt-in for local work:
	//
	//	MARKDOWN_SCALING_LADDER=1 go test ./core/markdown/ -run Scaling
	//
	// This repo already carries #342, #353 and #363 for gates encoding
	// machine-speed assumptions. A fourth would be worse than admitting
	// the ratio wants an operation counter, not a clock.
	ladder := os.Getenv("MARKDOWN_SCALING_LADDER") == "1"

	// A quadratic regression spends 41s on the FIRST size, so waiting for
	// the ratio means waiting for the second one too. Correct is ~1 ms
	// here; this ceiling is roughly 2700× that, far outside anything a slow
	// or loaded runner produces and far inside the 41s a real regression
	// takes.
	const firstShotCeiling = 3 * time.Second

	// Take the BEST of a few runs per size rather than one sample. A single
	// shot on a shared runner measures whatever else the box was doing:
	// CI produced 10.92ms at 256 KiB then 7.29ms at 512 KiB — the larger
	// input FASTER than the smaller one — and the inflated first sample
	// then made the next ratio read 3.7x. The minimum is the right
	// estimator for "how fast can this go": noise only ever adds time, so
	// the floor is the signal and everything above it is the machine.
	const samples = 3
	// Bails as soon as one sample is already over the ceiling: a quadratic
	// regression spends ~41s per render here, and sampling it three times
	// would triple the time to surface a failure the first sample already
	// proves.
	best := func(in string) time.Duration {
		lo := time.Duration(1 << 62)
		for i := 0; i < samples; i++ {
			start := time.Now()
			_ = RenderHTML(in)
			d := time.Since(start)
			if d < lo {
				lo = d
			}
			if d > firstShotCeiling {
				return d
			}
		}
		return lo
	}

	sizes := []int{256 * 1024, 512 * 1024, 1024 * 1024, 2048 * 1024}
	var prev time.Duration
	for i, sz := range sizes {
		in := "a" + strings.Repeat("`", sz) + strings.Repeat("x", sz)
		elapsed := best(in)
		t.Logf("run=%d tail=%d best-of-%d %v", sz, sz, samples, elapsed)
		if i == 0 && elapsed > firstShotCeiling {
			t.Fatalf("SECURITY: [markdown] %d-byte run+tail took %v, over the %v ceiling — a linear renderer needs about a millisecond here, so this is a complexity regression, not a slow machine", sz, elapsed, firstShotCeiling)
		}
		if ladder && i > 0 && prev > 0 {
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

	// What is NOT guarded here, stated plainly rather than pretended.
	//
	// The absolute backstop above is the whole gate. There is no scaling
	// assertion for this shape, because two attempts at one were each worse
	// than none:
	//
	//  1. A ratio ladder (k = 350, 700, 1400) passed here at 1.46x then
	//     3.45x and FAILED on CI at 4.8x then 10x on identical code. What
	//     the large step measures on a shared runner is the allocator, not
	//     the parser. This repo already carries two tickets for flaky
	//     blocking checks; a third is not an improvement.
	//     (That ladder also logged "n = 4k²" while distinctRuns(k) is
	//     ≈ k²/2 bytes — it overstated its own inputs 8x, which is why it
	//     looked like it was exercising 7.84 MB when k=1400 is ~1 MB.)
	//
	//  2. Normalising against a benign render of the same length in the
	//     same process fixes the machine-dependence but is not sensitive:
	//     replacing the run index with a linear scan — the exact regression
	//     it would exist to catch — moved the adversarial render from
	//     0.47x to 1.12x of benign. Any ceiling loose enough to survive CI
	//     is far too loose to catch that.
	//
	// The residual is second-order: K distinct run lengths need ≈K²/2 bytes
	// and cost one scan each, so it is O(n^1.5) in the pre-index code, not
	// quadratic. At the sizes a real document reaches, that is milliseconds
	// either way — which is exactly why wall-clock cannot separate them, and
	// also why it is not urgent.
	//
	// Reference numbers on an M4 Pro at n≈982 KB, for comparing a future
	// regression against rather than guessing:
	//
	//	pre-fix (per-position tail rescan)  552 ms
	//	whole-run skip only                 ~6.8x growth per 4x input
	//	with the run index                  1.1 ms   (benign text: 2.4 ms)
	//
	// A gate for this wants an operation counter, not a clock.
}

// TestMarkdown_GiantBacktickRunNoInt32Wrap pins that offsets into a block
// are not narrowed to int32 on the way into the backtick-run index.
// buildBacktickRuns stored starts and lengths as int32 while Render imposes
// no document-size limit, so a block over math.MaxInt32 wrapped them: a
// single backtick run of 2^31+2 made lens negative, findCodeEnd returned a
// negative "open", and the caller sliced input[i+open:end] — a panic in the
// render goroutine, the same crash class TestMarkdown_MalformedTableNoPanic
// pins. Offsets stay int end to end now; this test holds that line.
//
// The input is a >2 GiB allocation, so the test opts in via
// MARKDOWN_BIGINPUT=1 rather than running in every suite (the green path
// is one linear pass plus one bulk write; measured 2.1s and a 6.2 GiB
// peak RSS on a 48 GB M4 Pro). The leading 'x' keeps the line off the
// fence classifier: a line beginning with ``` is a fence and never
// reaches the inline path.
func TestMarkdown_GiantBacktickRunNoInt32Wrap(t *testing.T) {
	if os.Getenv("MARKDOWN_BIGINPUT") != "1" {
		t.Skip("allocates a >2 GiB block; opt in with MARKDOWN_BIGINPUT=1")
	}
	runLen := 1<<31 + 2
	input := "x" + strings.Repeat("`", runLen)
	doc := Render(input)
	got := string(doc.HTML)
	// The giant run has no same-length closer, so CommonMark leaves it
	// literal text: one paragraph, escaped (backticks need no escaping).
	if !strings.HasPrefix(got, "<p>x``") || !strings.HasSuffix(got, "```</p>\n") {
		t.Fatalf("render mangled the giant run: prefix %q suffix %q", got[:16], got[len(got)-16:])
	}
	if want := len("<p>x") + runLen + len("</p>\n"); len(got) != want {
		t.Fatalf("len(doc.HTML) = %d, want %d", len(got), want)
	}
}
