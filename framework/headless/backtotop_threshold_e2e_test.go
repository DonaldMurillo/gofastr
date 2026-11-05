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
