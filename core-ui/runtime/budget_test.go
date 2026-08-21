package runtime

import (
	"bytes"
	"compress/gzip"
	"testing"
)

const (
	coreGoalGZ             = 12*1024 + 512
	coreCongestionWindowGZ = 14 * 1024
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
	const step = 64
	for i := 0; i+step <= len(filler); i += step {
		candidate := grown + filler[i:i+step]
		if gzipSize(t, candidate) > coreGoalGZ {
			break
		}
		grown = candidate
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
		t.Errorf("core runtime.js gzip at level 1 = %d bytes — exceeds the %d-byte initial congestion window; carve a feature into a demand module, do not raise the line", got, limit)
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
// When either budget trips, the answer is carving a feature into a
// demand module rather than raising the line, but nav and island RPC
// must stay in core: a demand module costs one request at first use,
// which is fine for drag-dismiss and fatal for the click path.
//
// The line moved once, from 12 KB to 12.5 KB, when the measurement was
// corrected from gzip level 9 to level 6 (see gzipSize). That was a
// ruler correction, not a concession, the artifact did not grow. Treat
// it as the last one: the cliff is a property of TCP, not of taste.
// At nginx's default gzip level 1 the core is 14156 bytes. The binding
// assertion in TestRuntimeModuleSizeBudgets caps that wire form at 14336 bytes,
// so the level-6 budget's remaining slack cannot carry core past the cliff
// unnoticed.
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
		t.Errorf("typical page payload (core+widgets) gzip = %d bytes — exceeds %d byte budget; carve a feature into a demand module, don't raise the line", got, typicalBudgetGZ)
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
