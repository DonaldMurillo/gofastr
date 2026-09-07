//go:build red && race

package ui

import (
	"strings"
	"sync"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/render"
)

// RED TEST — open finding, 2026-09-06 adversarial pass (round 5; tests-only; no fix applied).
// RACE-TAGGED: only compiles under red+race; the -race detector's DATA RACE
// report IS the red proof. Do NOT run plain — without the detector the same
// access is Go's unrecoverable "concurrent map writes" fatal, which kills the
// whole host process with every in-flight request.
// Property: concurrent SSR renders are race-free — the ui package's contract,
// pinned by TestCarouselConcurrentRenderUniqueIDs,
// TestDataTable_ConcurrentRender and TestAutoIDUniqueAcrossConcurrent.
// registry.RegisterStyle itself is mutex-guarded; only this local cache
// diverges.
// Surfaces: framework/ui/responsive.go::getOrRegisterResponsiveStyle +
// package var responsiveStyleCache — a plain map[int]*registry.Style with a
// check-then-set body (responsive.go:85-98); the file imports no sync.
// Finding: two concurrent Responsive calls with uncached breakpoints race on
// the map (read at :88 vs write at :96) with no synchronization. Trigger is
// plain traffic — the first two concurrent requests after a deploy on routes
// with different breakpoints. Under -race the access pair is flagged; without
// it, the interleaved map writes are the runtime's fatal "concurrent map
// writes" throw, taking down the process.
// Fix direction: guard responsiveStyleCache with a sync.Mutex (or sync.Map /
// registry-side dedupe) in getOrRegisterResponsiveStyle.

func TestResponsiveRedCacheRace(t *testing.T) {
	const goroutines = 64
	const iterations = 50
	var wg sync.WaitGroup
	wg.Add(goroutines)
	for g := range goroutines {
		go func(i int) {
			defer wg.Done()
			// Each goroutine owns an uncached breakpoint (480 + i*7 for
			// i in [0,64) spans 480..921; nothing else in the package
			// registers these), so every first call takes the miss path:
			// map read at responsive.go:88 racing the other goroutines'
			// map write at :96.
			bp := 480 + i*7
			for range iterations {
				h := Responsive(ResponsiveConfig{Breakpoint: bp},
					render.Text("d"), render.Text("m"))
				if len(h) == 0 {
					t.Errorf("SECURITY: [responsive-cache-race] render returned empty output for breakpoint %d", bp)
				}
			}
		}(g)
	}
	wg.Wait()

	// Canary after the join: the primitive still renders a well-formed
	// swap serially. This asserts the SECURE shape so the test reads as a
	// contract, not a crash script.
	got := string(Responsive(ResponsiveConfig{Breakpoint: 1024},
		render.Text("d"), render.Text("m")))
	for _, want := range []string{"ui-responsive__desktop", "ui-responsive__mobile", "d", "m"} {
		if !strings.Contains(got, want) {
			t.Errorf("SECURITY: [responsive-cache-race] post-concurrency render missing %q — the unsynchronized responsiveStyleCache left the swap malformed:\n%s", want, got)
		}
	}
}
