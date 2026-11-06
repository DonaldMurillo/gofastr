package headless

import (
	"fmt"
	"net/http"
	"testing"

	"github.com/chromedp/chromedp"
)

func TestBackToTopThresholdTracksPage(t *testing.T) {
	page := func(threshold int) string {
		return fmt.Sprintf(`<a href="#" data-hui-back-to-top data-hui-back-to-top-threshold="%d">Top</a>`, threshold)
	}
	browser := startBehaviorServer(t, page(950), func(mux *http.ServeMux) {
		mux.HandleFunc("/next", func(writer http.ResponseWriter, request *http.Request) {
			writer.Header().Set("Content-Type", "text/html")
			fmt.Fprint(writer, `<main>`+page(130)+page(370)+`</main>`)
		})
		mux.HandleFunc("/empty", func(writer http.ResponseWriter, request *http.Request) {
			writer.Header().Set("Content-Type", "text/html")
			fmt.Fprint(writer, `<main id="empty">No links</main>`)
		})
	})
	ctx := behaviorPage(t, browser)
	if !pollTrue(ctx, `document.getElementById('cui-backtotop-sentinel')?.style.height === '950px'`) {
		t.Fatal("initial threshold was not armed")
	}
	for _, next := range []struct {
		path string
		want int
	}{{"/next", 370}, {"/empty", 0}} {
		if err := chromedp.Run(ctx, chromedp.Evaluate(`window.__gofastr.navigate('`+next.path+`')`, nil)); err != nil {
			t.Fatal(err)
		}
		if !pollTrue(ctx, fmt.Sprintf(`location.pathname === %q && document.getElementById('cui-backtotop-sentinel').style.height === '%dpx'`, next.path, next.want)) {
			t.Fatalf("%s did not recompute the connected links' maximum threshold to %d", next.path, next.want)
		}
	}
}

func TestBackToTopRemovalLowersThreshold(t *testing.T) {
	for _, thresholds := range [][2]int{{100, 800}, {170, 640}} {
		t.Run(fmt.Sprint(thresholds), func(t *testing.T) {
			page := fmt.Sprintf(`<a href="#" data-hui-back-to-top data-hui-back-to-top-threshold="%d">Top</a>
				<div data-cui-signal="threshold" data-cui-signal-mode="html"><a href="#" data-hui-back-to-top data-hui-back-to-top-threshold="%d">Top</a></div>`, thresholds[0], thresholds[1])
			ctx := behaviorPage(t, startBehaviorServer(t, page))
			if !pollTrue(ctx, fmt.Sprintf(`document.getElementById('cui-backtotop-sentinel')?.style.height === '%dpx'`, thresholds[1])) {
				t.Fatal("initial threshold was not armed")
			}
			if err := chromedp.Run(ctx, chromedp.Evaluate(`window.__gofastr.setSignal('threshold', '')`, nil)); err != nil {
				t.Fatal(err)
			}
			if !pollTrue(ctx, fmt.Sprintf(`document.getElementById('cui-backtotop-sentinel').style.height === '%dpx'`, thresholds[0])) {
				t.Fatalf("removal-only island did not lower threshold to %d", thresholds[0])
			}
			if err := chromedp.Run(ctx, chromedp.Evaluate(`document.querySelector('[data-hui-back-to-top]').remove()`, nil)); err != nil {
				t.Fatal(err)
			}
			if !pollTrue(ctx, `document.getElementById('cui-backtotop-sentinel').style.height === '0px'`) {
				t.Fatal("removing the final link did not clear its threshold")
			}
		})
	}
}
