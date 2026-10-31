package runtime

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	cdruntime "github.com/chromedp/cdproto/runtime"
	"github.com/chromedp/chromedp"

	"github.com/DonaldMurillo/gofastr/internal/chromedptest"
)

// widgetHelpersServer serves body on a French page whose strings block
// carries the reader's words, with the runtime and its modules.
func widgetHelpersServer(t *testing.T, body string) string {
	t.Helper()
	js, err := RuntimeJS()
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/__gofastr/runtime.js", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/javascript")
		_, _ = w.Write([]byte(js))
	})
	mux.HandleFunc("/__gofastr/runtime/", func(w http.ResponseWriter, r *http.Request) {
		name := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/__gofastr/runtime/"), ".js")
		src, ok := Module(name)
		if !ok {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/javascript")
		_, _ = w.Write([]byte(src))
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprintf(w, `<!doctype html><html lang="fr"><head><meta charset="utf-8"><title>helpers</title>
<script type="application/json" id="gofastr-strings">{"runtime.charcount":"{{n}} caractères","runtime.elapsed_s":"{{n}} s"}</script>
<script>window.__cleared = 0; const ci = window.clearInterval; window.clearInterval = function (id) { window.__cleared++; return ci(id); };</script>
</head><body><main role="main">%s</main><script src="/__gofastr/runtime.js"></script></body></html>`, body)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv.URL
}

// loadWidgetHelpers makes the call widgets.js makes when a mounted widget
// holds a helper marker: the module scans the document once it registers.
func loadWidgetHelpers() chromedp.Action {
	return chromedp.ActionFunc(func(ctx context.Context) error {
		if err := chromedp.Poll(`typeof window.__gofastr?.loadModule === 'function'`, nil, chromedp.WithPollingTimeout(10*time.Second)).Do(ctx); err != nil {
			return err
		}
		return chromedp.Evaluate(`window.__gofastr.loadModule('widgethelpers')`, nil, func(p *cdruntime.EvaluateParams) *cdruntime.EvaluateParams {
			return p.WithAwaitPromise(true)
		}).Do(ctx)
	})
}

// The character counter and the elapsed ticker wrote "1234 chars" and
// "2.4s" on every locale (2026-10-27 R4): both say their words and write
// the number in the page's language.
func TestHelpersSayCountsInPageLocale(t *testing.T) {
	start := time.Now().Add(-2300 * time.Millisecond).UnixMilli()
	url := widgetHelpersServer(t, fmt.Sprintf(`<textarea id="src">%s</textarea>
<span id="count" data-cui-charcount-source="#src"></span>
<span id="tick" data-cui-tick-elapsed="%d"></span>`, strings.Repeat("a", 1234), start))
	ctx := chromedptest.Context(t)
	var count, want, tick string
	if err := chromedp.Run(ctx,
		chromedp.Navigate(url),
		loadWidgetHelpers(),
		chromedp.Poll(`document.getElementById('count').textContent !== ''`, nil, chromedp.WithPollingTimeout(10*time.Second)),
		chromedp.Evaluate(`document.getElementById('count').textContent`, &count),
		chromedp.Evaluate(`(1234).toLocaleString('fr') + ' caractères'`, &want),
		chromedp.Poll(`document.getElementById('tick').textContent !== ''`, nil, chromedp.WithPollingTimeout(10*time.Second)),
		chromedp.Evaluate(`document.getElementById('tick').textContent`, &tick),
	); err != nil {
		t.Fatal(err)
	}
	if count != want {
		t.Errorf("charcount = %q, want %q", count, want)
	}
	// "2,4 s", or "3 s" on a slow load that rounds to a whole second:
	// the catalog's unit, and never the English "2.4s".
	if !strings.HasSuffix(tick, " s") || strings.Contains(tick, ".") {
		t.Errorf("elapsed = %q, want the catalog's unit and a French decimal (2,4 s)", tick)
	}
}

// The elapsed ticker ran querySelectorAll five times a second for the
// life of the page, after a navigation removed every ticker
// (2026-10-27 R5). It stops when no ticker is left.
func TestTickerStopsWithNoNodes(t *testing.T) {
	url := widgetHelpersServer(t, fmt.Sprintf(`<span id="tick" data-cui-tick-elapsed="%d"></span>`, time.Now().UnixMilli()))
	ctx := chromedptest.Context(t)
	if err := chromedp.Run(ctx,
		chromedp.Navigate(url),
		loadWidgetHelpers(),
		chromedp.Poll(`document.getElementById('tick').textContent !== ''`, nil, chromedp.WithPollingTimeout(10*time.Second)),
		chromedp.Evaluate(`document.getElementById('tick').remove()`, nil),
		chromedp.Poll(`window.__cleared > 0`, nil, chromedp.WithPollingTimeout(3*time.Second)),
	); err != nil {
		t.Fatalf("ticker kept running after its last node left: %v", err)
	}
	// A ticker that arrives later (the next navigation's page) starts it
	// again when the module's scanner sees it.
	if err := chromedp.Run(ctx,
		chromedp.Evaluate(`(() => { const s = document.createElement('span'); s.id = 'tick2'; s.setAttribute('data-cui-tick-elapsed', String(Date.now())); document.body.appendChild(s); window.__gofastr._moduleScanners.widgethelpers(document); })()`, nil),
		chromedp.Poll(`document.getElementById('tick2').textContent !== ''`, nil, chromedp.WithPollingTimeout(3*time.Second)),
	); err != nil {
		t.Fatalf("a ticker added after the stop never started: %v", err)
	}
}
