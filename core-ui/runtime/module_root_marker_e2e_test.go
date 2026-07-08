package runtime

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/chromedp/chromedp"
)

// A lazily-mounted widget's chrome is appended to <body> as a single
// root element that itself carries the module marker (e.g.
// data-fui-drag-dismiss="true" on a BottomSheet root emitted by
// defaultSkeleton). The MutationObserver hands that root node to
// _scanForModules, which must match the node ITSELF, not just its
// descendants — otherwise root-marker modules (dragdismiss) never load
// and the sheet's drag handle is dead DOM.
func TestModuleLoadsForRootMarkerNode(t *testing.T) {
	js, err := RuntimeJS()
	if err != nil {
		t.Fatal(err)
	}
	mod, ok := Module("dragdismiss")
	if !ok {
		t.Fatal("dragdismiss module not embedded")
	}

	var modHits atomic.Int32
	mux := http.NewServeMux()
	mux.HandleFunc("/__gofastr/runtime.js", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/javascript")
		w.Write([]byte(js))
	})
	mux.HandleFunc("/__gofastr/runtime/dragdismiss.js", func(w http.ResponseWriter, r *http.Request) {
		modHits.Add(1)
		w.Header().Set("Content-Type", "application/javascript")
		w.Write([]byte(mod))
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		// No marker in the initial DOM — the module must NOT load at boot.
		fmt.Fprint(w, `<!doctype html><html><head><title>root-marker</title></head><body>
  <main role="main"><span id="ready">ready</span></main>
  <script src="/__gofastr/runtime.js"></script>
</body></html>`)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	ctx := newSeedBrowserCtx(t)

	var loaded bool
	if err := chromedp.Run(ctx,
		chromedp.Navigate(srv.URL+"/"),
		chromedp.WaitVisible(`#ready`, chromedp.ByID),
		// Simulate a lazily-mounted widget: the appended node IS the
		// widget root and carries the marker attribute itself.
		chromedp.Evaluate(`(() => {
            const el = document.createElement('div');
            el.className = 'fui-widget fui-pos-bottom';
            el.setAttribute('data-fui-widget', 'probe-sheet');
            el.setAttribute('data-fui-drag-dismiss', 'true');
            el.innerHTML = '<div class="fui-widget-drag-handle" data-fui-drag-handle="true"></div>';
            document.body.appendChild(el);
        })()`, nil),
		chromedp.Sleep(600*time.Millisecond),
		chromedp.Evaluate(`!!(window.__gofastr.loadedModules && window.__gofastr.loadedModules.dragdismiss)`, &loaded),
	); err != nil {
		t.Fatalf("chromedp: %v", err)
	}

	if modHits.Load() == 0 || !loaded {
		t.Fatalf("dragdismiss never loaded for a root-marker node (fetches=%d loaded=%v) — _scanForModules must match the scope node itself", modHits.Load(), loaded)
	}
}
