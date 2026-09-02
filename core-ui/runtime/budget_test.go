package runtime

import (
	"bytes"
	"compress/gzip"
	"testing"
)

const (
	// 12984, raised 176 bytes from 12808 on 2026-09-01 for #372's
	// document-capability boundary, under the same documented RULE
	// EXCEPTION as every raise below: the shrink was measured, not
	// skipped.
	//
	// What bought the bytes: crossesDocBoundary and its four gates in
	// the nav fragment. A script registered document-scoped
	// (data-fui-doc on the tag, docScripts in the route manifest)
	// installs capabilities INTO the document — WebMCP's
	// navigator.modelContext tools are the case — and removing the tag
	// does not uninstall them, so a soft swap across the scope's edge
	// would leave the destination document carrying the origin's
	// capabilities. Every soft-nav entry point (click hijack, navigate,
	// popstate, loadPage's redirect leg) must compare the destination's
	// manifest set against the live document's tags and stand down for
	// a real document load.
	//
	// It cannot be carved into a demand module: the click gate runs
	// synchronously before preventDefault, before any pointerover
	// prefetch could be relied on, and a cold module at keyboard-Enter
	// time is a missed boundary, which is the capability leak the check
	// exists to stop — the same fatal-for-the-path class as the confirm
	// gate and the click bridge.
	//
	// The merged bundle measures 12976 at level 6; the cheapest correct
	// spelling (sorted-src join compare, one helper) is what ships. The
	// line carries 8 bytes of clearance, the same margin the 12808 raise
	// used. Re-measure after a merge, not before.
	coreGoalGZ = 12*1024 + 696
	// 14.7 KB, not the 14 KB initial congestion window it started as.
	//
	// The window is still the constraint that matters, and the artifact still
	// fits inside it in every deployment: nothing in the framework compresses
	// runtime.js, so the bytes a browser receives are compressed by nginx, a
	// CDN, or whatever proxy fronts the app, all of which use zlib. This line
	// measures Go's own BestSpeed encoder as a worst-case proxy for those.
	//
	// Go 1.27 made that proxy pessimistic. Its compress/flate BestSpeed
	// encoder emits ~2% more for identical input: the same 41812-byte bundle
	// went from 14306 bytes under Go 1.26.6 to 14602 under Go 1.27.0, with the
	// runtime source unchanged by a single byte. The old line was clearing by
	// 30 bytes, so a compressor revision was always going to decide it.
	//
	// Same call as the 12 KB to 12.5 KB move recorded on
	// TestTypicalPagePayloadBudget: the code did not grow, the ruler moved.
	// That doc comment has the policy for the other case, where the code DOES
	// grow; a ruler change is not a licence to skip it. Before re-baselining
	// again, dump the bundle on both toolchains and confirm the byte count is
	// identical, the way this was confirmed.
	//
	// The value is bracketed on BOTH sides and cannot simply be raised.
	// TestCoreBudgetRejectsCliffOverflow builds a bundle sitting exactly on the
	// level-6 goal and requires it to cross this line; raise the line past that
	// fixture and the guard stops guarding instead of making room. The ceiling
	// is between 14800 (still guards) and 14825 (vacuous), measured by moving
	// the constant and watching that test, not by arithmetic.
	//
	// The band is tighter than it looks. The bundle measured 14602 when this
	// line was first re-baselined against a 41812-byte artifact; v0.69.0's
	// click-path work took it to 41968 raw and 14660 compressed, and the
	// native-submit confirm gate (#279: data-fui-confirm honored on plain POST
	// forms, which until then submitted unconfirmed) took it to 42215 raw and
	// 14745 compressed. The gate cannot be carved into a demand module: a
	// native submit navigates away before a module could load, the same class
	// of fatal-for-the-path as the click bridge. So the line moved to 14784.
	//
	// Three changes then landed against three different mains and each
	// measured this independently: the prefetch-failure retry
	// (mark-on-success), forwarding the widget trigger's ctx through the
	// core boot, and loadModule's module-name shape check -- the last of
	// which stops a "../../../evil" value in data-fui-prefetch from
	// normalizing out of the runtime serve route onto an arbitrary
	// same-origin script. None could see the others, so every one of them
	// reported more headroom than exists. The value below was re-measured
	// on the merged bundle, which is the only measurement that means
	// anything. Re-measure after a merge, not before.
	// 14984, raised 196 bytes from 14788 on 2026-09-01 under the same
	// exception recorded on coreGoalGZ above, for the same change
	// (#372's document-capability boundary gates; see that comment).
	// The real merged bundle measures 14981 at level 1; the line carries
	// 3 bytes of clearance. The ceiling bracket above (14800–14825)
	// moved with the bundle: with core sitting 8 bytes under the level-6
	// goal, the fixture's level-1 size (14991) sits 10 bytes over the
	// real bundle's, and the line was verified against the cliff test by
	// running it, not by arithmetic.
	coreCongestionWindowGZ = 14*1024 + 648
)

func coreBudgetViolation(t *testing.T, src string, budget int) (level, got, limit int) {
	t.Helper()
	if got := gzipSize(t, src); got > budget {
		return gzip.DefaultCompression, got, budget
	}
	if got := gzipSizeAt(t, src, gzip.BestSpeed); got > coreCongestionWindowGZ {
		return gzip.BestSpeed, got, coreCongestionWindowGZ
	}
	return 0, 0, 0
}
func TestCoreBudgetRejectsCliffOverflow(t *testing.T) {
	core, err := RuntimeJS()
	if err != nil {
		t.Fatalf("RuntimeJS: %v", err)
	}
	filler, ok := Module("sortablelist")
	if !ok {
		t.Fatal("sortablelist module not embedded")
	}

	grown := core
	// Fill the level-6 headroom as finely as it takes. A fixed 64-byte
	// step loses the last chunk once the real core sits close enough
	// to the goal (the sidebar scanner widening in #298 consumed
	// exactly that margin): the fixture then stops short of the window
	// and this self-test fails for no budget reason. Halving the step
	// keeps the fixture sitting ON the goal; if even 4-byte chunks
	// cannot push it past the window, the core has genuinely outgrown
	// the line and the bracket comment on coreCongestionWindowGZ is
	// the thing to revisit.
	const step = 64
	chunk := step
	for i := 0; i+chunk <= len(filler); {
		candidate := grown + filler[i:i+chunk]
		if gzipSize(t, candidate) > coreGoalGZ {
			if chunk <= 1 {
				break
			}
			chunk /= 2
			continue
		}
		grown = candidate
		i += chunk
	}
	defaultSize := gzipSize(t, grown)
	bestSpeedSize := gzipSizeAt(t, grown, gzip.BestSpeed)
	if defaultSize > coreGoalGZ {
		t.Fatalf("fixture exceeds the level-6 budget: %d > %d", defaultSize, coreGoalGZ)
	}
	if bestSpeedSize <= coreCongestionWindowGZ {
		t.Fatalf("fixture does not cross the level-1 congestion window: %d <= %d", bestSpeedSize, coreCongestionWindowGZ)
	}

	level, _, _ := coreBudgetViolation(t, grown, coreGoalGZ)
	if level != gzip.BestSpeed {
		t.Fatalf("core budget accepted %d bytes at level 6 although level 1 is %d bytes, past the %d-byte congestion window",
			defaultSize, bestSpeedSize, coreCongestionWindowGZ)
	}
}

// Per-module gzip size budget.
//
// Two purposes:
//
//  1. Catch regressions: if a module grows past its current high-water
//     mark, fail loudly. Cheaper than waiting for a Lighthouse drop.
//  2. Pin the runtime-size goals from runtime-minification.md:
//     core ≤ 12.5 KB at gzip level 6, core ≤ 14 KB at level 1, and every
//     demand module ≤ 3 KB at level 6.
//
// Never add or raise an override to silence a regression: split or shrink the
// module instead.
func TestRuntimeModuleSizeBudgets(t *testing.T) {
	// 12.5 KB, not 12: the budget was 12 KB measured at gzip level 9,
	// which nothing ships at. Re-measuring at the level browsers
	// actually receive (see gzipSize) moved the same artifact from 12287
	// to 12317 bytes, the code did not grow, the ruler was wrong. The
	// line is set where it covers the real number with room to work
	// rather than where it silently passed.
	const moduleGoalGZ = 3 * 1024

	// No overrides: optional widget form helpers and shortcut/autogrow
	// behavior live in marker-driven demand modules, keeping both core and
	// every feature module within their real budgets.
	moduleOverrides := map[string]int{}
	const coreOverride = 0

	core, err := RuntimeJS()
	if err != nil {
		t.Fatalf("RuntimeJS: %v", err)
	}
	coreBudget := coreOverride
	if coreBudget == 0 {
		coreBudget = coreGoalGZ
	}
	level, got, limit := coreBudgetViolation(t, core, coreBudget)
	switch level {
	case gzip.DefaultCompression:
		t.Errorf("core runtime.js gzip = %d bytes — exceeds %d byte budget (goal %d)", got, limit, coreGoalGZ)
	case gzip.BestSpeed:
		t.Errorf("core runtime.js gzip at level 1 = %d bytes — exceeds the %d-byte initial congestion window; carve a feature into a demand module first, and if it cannot be carved, move the line by the smallest step that fits and say in the commit what bought it", got, limit)
	}

	for _, name := range ModuleNames() {
		src, ok := Module(name)
		if !ok {
			t.Errorf("module %q not embedded", name)
			continue
		}
		budget := moduleGoalGZ
		if o, ok := moduleOverrides[name]; ok {
			budget = o
		}
		if got := gzipSize(t, src); got > budget {
			t.Errorf("module %s gzip = %d bytes — exceeds %d byte budget (goal %d)", name, got, budget, moduleGoalGZ)
		}
	}
}

func TestComputeModuleSizeBudget(t *testing.T) {
	const budgetGZ = 3 * 1024
	src, ok := Module("compute")
	if !ok {
		t.Fatal("compute module not embedded")
	}
	got := gzipSize(t, src)
	t.Logf("compute module gzip = %d bytes", got)
	if got > budgetGZ {
		t.Fatalf("compute module gzip = %d bytes — exceeds %d byte budget", got, budgetGZ)
	}
}

// Typical-page payload budget: core + the widgets module.
//
// The per-module budgets above keep the core honest, but they have a
// blind spot: features can migrate out of core into widgets.js (which
// nearly every real app loads, any page that mounts a widget pulls
// it), keeping the core number pure while the payload users actually
// download quietly bloats. This test pins the realistic first-load
// cost.
//
// Why these numbers: TCP's initial congestion window is ~10 packets
// (≈14 KB), so the CORE arriving in the first round trip is what the
// 12.5 KB budget protects, that's the cliff; shrinking below it buys
// nothing, exceeding it costs a whole RTT on cold connections. The
// typical-page line (20 KB) is core 12.5 + widgets 5 + drift room.
// When either budget trips, the FIRST answer is carving a feature into a
// demand module, but nav and island RPC must stay in core: a demand
// module costs one request at first use, which is fine for drag-dismiss
// and fatal for the click path. When carving is not available, see the
// policy below for what moving the line costs and what has to be said.
//
// These lines move. Not often and not quietly, but a budget that can
// never move is one that gets lied to instead, so the rule is what has to
// be true before it does, not that it never does. Two kinds of change
// move it and they are not the same kind of decision.
//
// A RULER change re-measures the same bytes. The level-6 line went from
// 12 KB to 12.5 KB when the measurement was corrected from gzip level 9
// to level 6 (see gzipSize): the artifact did not grow, the ruler was
// wrong. A compressor revision is the same thing arriving from outside,
// and Go's compress/flate has changed what BestSpeed emits for identical
// input before. Re-baseline, and say in the commit which release moved
// it and by how much, so the number keeps meaning something.
//
// A SOURCE change is the concession, and gets written down here. The
// level-1 line went from 14336 to 14464 for one: HTML matches the
// underscore target keywords ASCII-case-insensitively, so `target="_SELF"`
// is `_self`, and comparing raw turned a soft navigation into a full page
// load. The fix cost 5 gzipped bytes and the core had 2.
//
// Carve first: that fix took the bytes because the usual answer was
// unavailable, not because it was easier. The guard IS the click path,
// which this comment already says stays in core. When a feature CAN move
// to a demand module, it moves. When it cannot, take the smallest step
// that fits and name what bought it.
//
// What does not move is the cliff. TCP's initial congestion window is
// about 10 packets, ~14600 bytes at a 1460-byte MSS, and a core past it
// costs a whole round trip on a cold connection. Every byte above 14336
// is borrowed from the first paint of every cold visit, so spend it
// deliberately and keep the running total in view.
func TestTypicalPagePayloadBudget(t *testing.T) {
	const typicalBudgetGZ = 20 * 1024

	core, err := RuntimeJS()
	if err != nil {
		t.Fatalf("RuntimeJS: %v", err)
	}
	widgets, ok := Module("widgets")
	if !ok {
		t.Fatal("widgets module not embedded")
	}
	got := gzipSize(t, core) + gzipSize(t, widgets)
	if got > typicalBudgetGZ {
		t.Errorf("typical page payload (core+widgets) gzip = %d bytes — exceeds %d byte budget; carve a feature into a demand module first, and if it cannot be carved, move the line by the smallest step that fits and say in the commit what bought it", got, typicalBudgetGZ)
	}
}

// Embed budgets. They are asymmetric because the two files land in completely
// different places.
//
// The loader runs on a customer's page, a site whose performance we do not
// control and whose owner did not choose GoFastr. It is the tightest budget
// here on purpose: crossing it means behaviour was added to the loader that
// belongs inside the frame, where it costs the host page nothing.
//
// The loader is currently 1586 bytes at gzip level 1. Its 1536-byte level-6
// line is a discipline budget, not a transport cliff, so applying core's
// level-1 rule here would invent a second limit with no physical basis.
//
// The frame runtime blocks nothing on the host page, so it is not bound by the
// initial-congestion-window argument that sets core's line. It is still capped
// at 12 KB for a simpler reason: an embedded surface has no business
// shipping MORE javascript than a first-party page does. It ships less today
// (no nav fragment).
func TestEmbedSizeBudgets(t *testing.T) {
	const (
		loaderBudgetGZ = 1536
		frameBudgetGZ  = 12 * 1024
	)

	loader, err := EmbedLoaderJS()
	if err != nil {
		t.Fatalf("EmbedLoaderJS: %v", err)
	}
	if got := gzipSize(t, loader); got > loaderBudgetGZ {
		t.Errorf("embed loader gzip = %d bytes — exceeds %d byte budget; move the behaviour into boot-embed rather than raising the line, the loader runs on someone else's page", got, loaderBudgetGZ)
	} else {
		t.Logf("embed loader gzip = %d bytes (budget %d)", got, loaderBudgetGZ)
	}

	frame, err := EmbedJS()
	if err != nil {
		t.Fatalf("EmbedJS: %v", err)
	}
	if got := gzipSize(t, frame); got > frameBudgetGZ {
		t.Errorf("embed runtime gzip = %d bytes — exceeds %d byte budget", got, frameBudgetGZ)
	} else {
		t.Logf("embed runtime gzip = %d bytes (budget %d)", got, frameBudgetGZ)
	}

	// The embed composition must stay SMALLER than the full one. If it ever
	// isn't, a fragment landed in embed that the full bundle does not carry,
	// which means the two are diverging rather than composing.
	full, err := RuntimeJS()
	if err != nil {
		t.Fatalf("RuntimeJS: %v", err)
	}
	if gzipSize(t, frame) >= gzipSize(t, full) {
		t.Errorf("embed runtime (%d gz) is not smaller than the full runtime (%d gz) — embed omits nav, so it must be", gzipSize(t, frame), gzipSize(t, full))
	}
}

// gzipSize measures at gzip level 6, the DEFAULT, and what actually
// reaches a browser.
//
// It used to measure at level 9. Nothing ships at level 9: GoFastr
// installs no compression middleware, so the wire bytes are produced by
// whatever proxy or CDN fronts the app, at its own setting. Go's
// httptest and most CDNs use 6; nginx's gzip_comp_level default is 1.
// Measuring the single most favourable level meant the gate certified a
// number no user receives, the core measured 12287 against a 12288
// budget while the level-6 artifact was already 12317.
func gzipSize(t *testing.T, s string) int {
	t.Helper()
	return gzipSizeAt(t, s, gzip.DefaultCompression)
}

func gzipSizeAt(t *testing.T, s string, level int) int {
	t.Helper()
	var buf bytes.Buffer
	w, err := gzip.NewWriterLevel(&buf, level)
	if err != nil {
		t.Fatalf("gzip writer at level %d: %v", level, err)
	}
	if _, err := w.Write([]byte(s)); err != nil {
		t.Fatalf("gzip write: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("gzip close: %v", err)
	}
	return buf.Len()
}
