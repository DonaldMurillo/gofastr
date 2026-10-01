package ui

import (
	"strings"
	"sync"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/render"
)

// Pins: concurrent SSR renders through Responsive are race-free.
// The original finding was an unsynchronized per-breakpoint style cache
// (responsiveStyleCache) raced by concurrent first renders — a fatal
// "concurrent map writes" that took the process down. That cache is gone:
// Responsive now registers one sheet at package init and renders read
// nothing mutable, so the race has no surface left. The test stays as the
// package's concurrent-render contract for this primitive (like
// TestCarouselConcurrentRenderUniqueIDs), covering both postures.
func TestResponsiveCacheRace(t *testing.T) {
	const goroutines = 64
	const iterations = 50
	var wg sync.WaitGroup
	wg.Add(goroutines)
	for g := range goroutines {
		go func(i int) {
			defer wg.Done()
			below := StackBelowMD
			if i%2 == 1 {
				below = StackBelowLG
			}
			for range iterations {
				h := Responsive(ResponsiveConfig{Below: below},
					render.Text("d"), render.Text("m"))
				if len(h) == 0 {
					t.Errorf("SECURITY: [responsive-cache-race] render returned empty output for %q", below)
				}
			}
		}(g)
	}
	wg.Wait()

	got := string(Responsive(ResponsiveConfig{Below: StackBelowLG},
		render.Text("d"), render.Text("m")))
	for _, want := range []string{"fui-responsive__desktop", "fui-responsive__mobile", "d", "m"} {
		if !strings.Contains(got, want) {
			t.Errorf("SECURITY: [responsive-cache-race] post-concurrency render missing %q:\n%s", want, got)
		}
	}
}
