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
//  2. Pin the runtime-size goals from runtime-minification.md:
//     core ≤ 12 KB gz and every demand module ≤ 3 KB gz.
//
// Never add or raise an override to silence a regression: split or shrink the
// module instead.
func TestRuntimeModuleSizeBudgets(t *testing.T) {
	const (
		coreGoalGZ   = 12 * 1024
		moduleGoalGZ = 3 * 1024
	)

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

// Embed budgets. They are asymmetric because the two files land in completely
// different places.
//
// The loader runs on a customer's page — a site whose performance we do not
// control and whose owner did not choose GoFastr. It is the tightest budget
// here on purpose: crossing it means behaviour was added to the loader that
// belongs inside the frame, where it costs the host page nothing.
//
// The frame runtime blocks nothing on the host page, so it is not bound by the
// initial-congestion-window argument that sets core's 12 KB. It is still capped
// at the same number for a simpler reason: an embedded surface has no business
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
	// isn't, a fragment landed in embed that the full bundle does not carry —
	// which means the two are diverging rather than composing.
	full, err := RuntimeJS()
	if err != nil {
		t.Fatalf("RuntimeJS: %v", err)
	}
	if gzipSize(t, frame) >= gzipSize(t, full) {
		t.Errorf("embed runtime (%d gz) is not smaller than the full runtime (%d gz) — embed omits nav, so it must be", gzipSize(t, frame), gzipSize(t, full))
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
