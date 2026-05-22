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
//  2. Surface known debt: ROADMAP §8 targets are tighter than today's
//     sizes (core ≤ 12 KB gz, each demand module ≤ 3 KB gz). The
//     overrides below name each module that's currently above target
//     so the gap is visible in the test, not buried in a doc.
//
// When you shrink a module: tighten its override toward the ROADMAP
// target. When you delete an override entirely: the module meets the
// default budget. Never raise an override to silence a regression —
// fix the module instead.
func TestRuntimeModuleSizeBudgets(t *testing.T) {
	const (
		coreROADMAPGoalGZ   = 12 * 1024
		moduleROADMAPGoalGZ = 3 * 1024
	)

	// Current high-water marks. Treat each override as a TODO to
	// shrink toward the ROADMAP goal above. Update DOWN when a module
	// shrinks; never update up.
	moduleOverrides := map[string]int{
		"widgets":  7 * 1024,  // goal 3 KB. Bulk: focus-trap + modal stack + dismiss machinery.
		"lightbox": 5 * 1024,  // goal 3 KB. Bulk: pinch-zoom + pan + keyboard nav.
	}
	const coreOverride = 28 * 1024 // goal 12 KB. Bulk: legacy bundled runtime; Phase 2 file-split has not yet carved core.

	core, err := RuntimeJS()
	if err != nil {
		t.Fatalf("RuntimeJS: %v", err)
	}
	coreBudget := coreOverride
	if coreBudget == 0 {
		coreBudget = coreROADMAPGoalGZ
	}
	if got := gzipSize(t, core); got > coreBudget {
		t.Errorf("core runtime.js gzip = %d bytes — exceeds %d byte budget (ROADMAP goal %d)", got, coreBudget, coreROADMAPGoalGZ)
	}

	for _, name := range ModuleNames() {
		src, ok := Module(name)
		if !ok {
			t.Errorf("module %q not embedded", name)
			continue
		}
		budget := moduleROADMAPGoalGZ
		if o, ok := moduleOverrides[name]; ok {
			budget = o
		}
		if got := gzipSize(t, src); got > budget {
			t.Errorf("module %s gzip = %d bytes — exceeds %d byte budget (ROADMAP goal %d)", name, got, budget, moduleROADMAPGoalGZ)
		}
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
