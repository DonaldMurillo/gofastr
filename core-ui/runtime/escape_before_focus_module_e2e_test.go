package runtime

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/internal/chromedptest"
	"github.com/chromedp/chromedp"
	"github.com/chromedp/chromedp/kb"
)

// Escape pressed while widgetfocus.js is still in flight closes the modal once it lands.
func TestEscapeBeforeFocusModuleLoadsClosesModal(t *testing.T) {
	js, err := RuntimeJS()
	if err != nil {
		t.Fatal(err)
	}
	mod, ok := Module("widgetfocus")
	if !ok {
		t.Fatal("widgetfocus module not embedded")
	}
	release := make(chan struct{})
	var once sync.Once
	free := func() { once.Do(func() { close(release) }) }
	mux := http.NewServeMux()
	handleRuntimeModules(t, mux)
	mux.HandleFunc("/__gofastr/runtime/widgetfocus.js", func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-release:
		case <-r.Context().Done():
			return
		}
		w.Header().Set("Content-Type", "application/javascript")
		fmt.Fprint(w, mod)
	})
	mux.HandleFunc("/__gofastr/runtime.js", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/javascript")
		fmt.Fprint(w, js)
	})
	mux.HandleFunc("/__gofastr/widgets", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `[{"hidden":true,"cfg":{"name":"dlg","chromePath":"/chrome/dlg","backdrop":true,"closeOnEscape":true}}]`)
	})
	mux.HandleFunc("/chrome/dlg", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, `<div class="cui-widget" data-cui-widget="dlg" role="dialog" aria-label="dlg"><button>ok</button></div>`)
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, `<!doctype html><html><head><title>esc</title></head><body><main>home</main>
<span id="ready">ready</span><script src="/__gofastr/runtime.js"></script></body></html>`)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	t.Cleanup(free)

	ctx := chromedptest.Context(t, chromedptest.Timeout(60*time.Second))
	var loadedAtEsc, open bool
	if err := chromedp.Run(ctx,
		chromedp.Navigate(srv.URL+"/"),
		chromedp.WaitVisible(`#ready`, chromedp.ByID),
		chromedp.Evaluate(`window.__gofastr.openWidget('dlg')`, nil, chromedp.EvalAsValue),
		chromedp.WaitVisible(`[data-cui-widget="dlg"] button`, chromedp.ByQuery),
		chromedp.Poll(`document.activeElement === document.querySelector('[data-cui-widget="dlg"] button')`, nil, chromedp.WithPollingTimeout(10*time.Second)),
		chromedp.Evaluate(`!!window.__gofastr.loadedModules.widgetfocus`, &loadedAtEsc),
		chromedp.KeyEvent(kb.Escape),
		chromedp.ActionFunc(func(context.Context) error { free(); return nil }),
		chromedp.Poll(`!!window.__gofastr.loadedModules.widgetfocus`, nil, chromedp.WithPollingTimeout(10*time.Second)),
		chromedp.Sleep(300*time.Millisecond),
		chromedp.Evaluate(`!!document.querySelector('[data-cui-widget="dlg"]:not([hidden])')`, &open),
	); err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	if loadedAtEsc {
		t.Fatal("setup: widgetfocus loaded before Escape; the held response did not hold")
	}
	if open {
		t.Fatal("modal still open: the Escape pressed before widgetfocus loaded was lost")
	}
}
