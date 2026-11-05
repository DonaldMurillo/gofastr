package runtime

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/DonaldMurillo/gofastr/internal/chromedptest"
	"github.com/chromedp/chromedp"
)

func TestPartsHandlesEnvelopeLoadFailure(t *testing.T) {
	kernel, err := RuntimeJS()
	if err != nil {
		t.Fatal(err)
	}
	var attempts atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.URL.Path == "/runtime.js" {
			w.Header().Set("Content-Type", "application/javascript")
			fmt.Fprint(w, kernel)
			return
		}
		if strings.HasPrefix(req.URL.Path, "/__gofastr/runtime/") {
			name := strings.TrimSuffix(strings.TrimPrefix(req.URL.Path, "/__gofastr/runtime/"), ".js")
			if name == "envelope" {
				attempts.Add(1)
				http.NotFound(w, req)
				return
			}
			if source, ok := Module(name); ok {
				w.Header().Set("Content-Type", "application/javascript")
				fmt.Fprint(w, source)
				return
			}
			http.NotFound(w, req)
			return
		}
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, `<!doctype html><head><script>
window.__errors=[];
addEventListener('unhandledrejection',event=>__errors.push(String(event.reason)));
addEventListener('error',event=>{
  if(event.target instanceof HTMLScriptElement && event.target.src.includes('/runtime/envelope.js')) {
    window.__failedEnvelope=true;
    setTimeout(()=>setTimeout(()=>window.__failureSettled=true,0),0);
  }
},true);
</script><script type="application/json" id="gofastr-routes">[{"path":"/","deferred":["l:site#aside"]}]</script></head><body><main>Ready</main><aside data-cui-outlet="l:site#aside">Inline content</aside><script src="/runtime.js"></script></body>`)
	}))
	defer server.Close()
	ctx := chromedptest.Context(t)
	var failures []string
	if err := chromedp.Run(ctx, chromedp.Navigate(server.URL),
		chromedp.Poll(`!!window.__gofastr?.loadedModules?.parts`, nil),
		chromedp.Poll(`!!window.__failedEnvelope && !!window.__failureSettled`, nil),
		chromedp.Evaluate(`window.__errors`, &failures)); err != nil {
		t.Fatal(err)
	}
	if attempts.Load() == 0 {
		t.Fatal("envelope module was never requested")
	}
	if len(failures) != 0 {
		t.Fatalf("unhandled module rejection: %v", failures)
	}
}
