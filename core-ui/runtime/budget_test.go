package runtime

import (
	"bytes"
	"compress/gzip"
	"testing"
)

// Per-module gzip size budget.
//
// Two purposes:
//
//  1. Catch regressions: if a module grows past its current high-water
//     mark, fail loudly. Cheaper than waiting for a Lighthouse drop.
//  2. Surface known debt: the runtime-size goals (see
//     runtime-minification.md) are tighter than today's
//     sizes (core ≤ 12 KB gz, each demand module ≤ 3 KB gz). The
//     overrides below name each module that's currently above target
//     so the gap is visible in the test, not buried in a doc.
//
// When you shrink a module: tighten its override toward the
// target. When you delete an override entirely: the module meets the
// default budget. Never raise an override to silence a regression —
// fix the module instead.
func TestRuntimeModuleSizeBudgets(t *testing.T) {
	const (
		coreGoalGZ   = 12 * 1024
		moduleGoalGZ = 3 * 1024
	)

	// Current high-water marks. Treat each override as a TODO to
	// shrink toward the goal above. Update DOWN when a module
	// shrinks; never update up.
	//
	// Post-minify the bundled runtime meets the 12 KB gz goal on its
	// own; widgets is the only module that still needs further carving
	// to hit the per-module 3 KB goal (lightbox cleared it).
	moduleOverrides := map[string]int{
		// goal 3 KB. Bulk: focus-trap + modal stack + dismiss machinery.
		// Net DOWN from 5120: the widget-poll loop moved to the
		// demand-loaded poll module (#112, −320 B), then the
		// data-fui-rpc-refresh cross-widget target read added back +21 B.
		"widgets": 4821,
	}
	// 12 KB (12288) is the goal; the extra bytes are (a) the
	// screen-group-aware SPA nav fix (#89, +12 B gz after golfing) and
	// (b) the #112 stateless-session rollover recovery (+86 B gz):
	// partial navs rewire the SSE meta from X-Gofastr-Session, and
	// cross-layout navs copy the freshly-fetched head's meta — without
	// both, a restart/key-rotation strands SSE in a 401 reconnect loop
	// until a hard reload. Golfed (shared sseMeta accessor, no
	// encodeURIComponent on the base64url id); the remainder is
	// irreducible recovery logic that must live in core (it runs during
	// navigation, before any demand module is guaranteed loaded).
	// TODO: reclaim toward the 12288 goal.
	const coreOverride = 12406

	core, err := RuntimeJS()
	if err != nil {
		t.Fatalf("RuntimeJS: %v", err)
	}
	coreBudget := coreOverride
	if coreBudget == 0 {
		coreBudget = coreGoalGZ
	}
	if got := gzipSize(t, core); got > coreBudget {
		t.Errorf("core runtime.js gzip = %d bytes — exceeds %d byte budget (goal %d)", got, coreBudget, coreGoalGZ)
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
// nearly every real app loads — any page that mounts a widget pulls
// it), keeping the core number pure while the payload users actually
// download quietly bloats. This test pins the realistic first-load
// cost.
//
// Why these numbers: TCP's initial congestion window is ~10 packets
// (≈14 KB), so the CORE arriving in the first round trip is what the
// 12 KB budget protects — that's the cliff; shrinking below it buys
// nothing, exceeding it costs a whole RTT on cold connections. The
// typical-page line (20 KB) is core 12 + widgets 5 + 3 KB of drift
// room. When either budget trips, the answer is carving a feature into
// a demand module, never raising the line — but nav and island RPC
// must stay in core: a demand module costs one request at first use,
// which is fine for drag-dismiss and fatal for the click path.
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

func gzipSize(t *testing.T, s string) int {
	t.Helper()
	var buf bytes.Buffer
	w, err := gzip.NewWriterLevel(&buf, gzip.BestCompression)
	if err != nil {
		t.Fatalf("gzip writer: %v", err)
	}
	if _, err := w.Write([]byte(s)); err != nil {
		t.Fatalf("gzip write: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("gzip close: %v", err)
	}
	return buf.Len()
}
