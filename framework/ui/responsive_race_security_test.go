package ui

import (
	"strings"
	"sync"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/render"
)

// Pins: concurrent SSR renders through Responsive are race-free — the
// package-level concurrent-render contract
// (TestCarouselConcurrentRenderUniqueIDs, TestDataTable_ConcurrentRender,
// TestAutoIDUniqueAcrossConcurrent) now covers responsiveStyleCache too.
// Meaningful under -race (the detector sees the pre-fix unsynchronized
// map pair); safe untagged since the fix (mutex-guarded cache).
// Property: concurrent SSR renders are race-free. registry.RegisterStyle
// was always mutex-guarded; responsiveStyleCache (getOrRegisterResponsiveStyle)
// was the unsynchronized one before the fix, and this test's 64 concurrent
// uncached-breakpoint renders are the traffic shape that exposed it.
// Surfaces: framework/ui/responsive.go::getOrRegisterResponsiveStyle +
// responsiveStyleMu + responsiveStyleCache (check-then-set under the mutex).
// Finding (pre-fix): two concurrent Responsive calls with uncached
// breakpoints raced the map read/write pair; under -race the pair is
// flagged, without it the interleaved writes are the runtime's fatal
// "concurrent map writes" throw, taking the process down.

func TestResponsiveCacheRace(t *testing.T) {
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
