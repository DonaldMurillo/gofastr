package runtime

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/internal/chromedptest"
	"github.com/chromedp/chromedp"
)

// A destroyed embed says nothing more. The loader's 20-second "never
// loaded" watchdog outlived destroy(), so a host page that unmounted an
// embed before it was ready got onError({code:'no-ready'}) for a frame it
// had already removed (2026-10-27 bug bash U44).
//
// The page shortens the 20-second timer to 100 ms and mounts two
// surfaces whose frames never load (the server answers 404). The kept
// one's no-ready report proves the watchdog ran; the destroyed one must
// stay silent.
func TestEmbedLoaderDestroyStopsWatchdog(t *testing.T) {
	if testing.Short() {
		t.Skip("browser test")
	}
	loader := MustEmbedLoaderJS()
	mux := http.NewServeMux()
	mux.HandleFunc("/loader.js", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/javascript")
		_, _ = w.Write([]byte(loader))
	})
	mux.HandleFunc("/{$}", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(`<!doctype html><html><head><title>host</title></head><body>
<div id="kept"></div><div id="gone"></div>
<script>
  window.__errors = [];
  const realSetTimeout = window.setTimeout;
  window.setTimeout = (fn, ms, ...rest) => realSetTimeout(fn, ms === 20000 ? 100 : ms, ...rest);
</script>
<script src="/loader.js" data-target="#none"></script>
</body></html>`))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	ctx := chromedptest.Context(t)
	var errs []string
	if err := chromedp.Run(ctx,
		chromedp.Navigate(srv.URL+"/"),
		chromedp.Poll(`!!window.GoFastrEmbed`, nil, chromedp.WithPollingTimeout(5*time.Second)),
		chromedp.Evaluate(`(() => {
			const mount = (name) => GoFastrEmbed.mount({
				origin: location.origin, surface: name, token: 't', target: '#' + name,
				onError: (d) => window.__errors.push(name + ':' + d.code),
			});
			mount('kept');
			mount('gone').destroy();
		})()`, nil),
		chromedp.Poll(`window.__errors.includes('kept:no-ready')`, nil, chromedp.WithPollingTimeout(5*time.Second)),
		chromedp.Sleep(300*time.Millisecond),
		chromedp.Evaluate(`window.__errors`, &errs),
	); err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	for _, e := range errs {
		if strings.HasPrefix(e, "gone:") {
			t.Fatalf("a destroyed embed reported %q; errors = %v", e, errs)
		}
	}
}
