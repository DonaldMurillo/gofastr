package runtime

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/internal/chromedptest"
	"github.com/chromedp/cdproto/emulation"
	"github.com/chromedp/chromedp"
)

func TestReducedMotionRetainsCachedTransitionPick(t *testing.T) {
	for _, envelope := range []bool{false, true} {
		t.Run(map[bool]string{false: "plain", true: "envelope"}[envelope], func(t *testing.T) {
			site := newResolveSite(t, envelope)
			site.pickOf["/a"] = "slide"
			site.pickOf["/b"] = "fade"
			ctx := chromedptest.Context(t)
			if err := chromedp.Run(ctx,
				emulation.SetEmulatedMedia().WithFeatures([]*emulation.MediaFeature{{Name: "prefers-reduced-motion", Value: "reduce"}}),
				chromedp.Navigate(site.srv.URL),
				chromedp.Poll(`!!window.__gofastr?.loadedModules?.transition`, nil),
			); err != nil {
				t.Fatal(err)
			}
			readTypes := captureTypes(ctx)
			for _, destination := range []struct{ selector, text, pick string }{
				{"#goA", "A", "slide"}, {"#goB", "B", "fade"},
			} {
				if err := chromedp.Run(ctx, chromedp.Click(destination.selector, chromedp.ByQuery)); err != nil {
					t.Fatal(err)
				}
				waitTextContent(t, ctx, "#main", destination.text)
				var pick string
				if err := chromedp.Run(ctx, chromedp.Evaluate(`history.state.vt || ''`, &pick)); err != nil {
					t.Fatal(err)
				}
				if pick != destination.pick {
					t.Errorf("reduced-motion history pick = %q, want %q", pick, destination.pick)
				}
			}
			if types := readTypes(); len(types) != 0 {
				t.Fatalf("reduced motion animated: %v", types)
			}
			var cached bool
			if err := chromedp.Run(ctx,
				emulation.SetEmulatedMedia().WithFeatures([]*emulation.MediaFeature{{Name: "prefers-reduced-motion", Value: "no-preference"}}),
				chromedp.Evaluate(`addEventListener('gofastr:navigate', event => window.__replayed = event.detail.cached)`, nil),
				chromedp.Click("#goA", chromedp.ByQuery),
				chromedp.WaitVisible("#page-a", chromedp.ByID),
				chromedp.Poll(`!document.documentElement.hasAttribute('aria-busy')`, nil),
				chromedp.Evaluate(`window.__replayed`, &cached),
			); err != nil {
				t.Fatal(err)
			}
			if types := readTypes(); !cached || len(types) != 1 || strings.Join(types[0], ",") != "forward,slide" {
				t.Fatalf("cached=%v transition types=%v, want cached forward,slide", cached, types)
			}
		})
	}
}
