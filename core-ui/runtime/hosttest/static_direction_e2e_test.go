package hosttest

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	_ "github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core-ui/runtime"
	"github.com/DonaldMurillo/gofastr/internal/chromedptest"
	"github.com/chromedp/chromedp"
)

func TestStaticEnvelopeLoadsDirection(t *testing.T) {
	script, err := runtime.StaticJS()
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/__gofastr/runtime.js", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/javascript")
		fmt.Fprint(w, script)
	})
	mux.HandleFunc("/__gofastr/runtime/", func(w http.ResponseWriter, request *http.Request) {
		name := strings.TrimSuffix(strings.TrimPrefix(request.URL.Path, "/__gofastr/runtime/"), ".js")
		module, ok := runtime.Module(name)
		if !ok {
			http.NotFound(w, request)
			return
		}
		w.Header().Set("Content-Type", "application/javascript")
		fmt.Fprint(w, module)
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, request *http.Request) {
		language, direction := "en", "ltr"
		if request.URL.Path == "/ar" {
			language, direction = "ar", "rtl"
		}
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprintf(w, `<!doctype html><html lang="%s" dir="%s"><head><title>Direction</title>
<script type="application/json" id="gofastr-routes">[{"path":"/","layouts":["l:site"]},{"path":"/ar","layouts":["l:site"]}]</script>
<script type="application/json" id="gofastr-behaviors">{"doc-dir":{"s":["[data-cui-dir=\"rtl\"]"]}}</script>
</head><body><div data-cui-layout="site" data-cui-layout-key="l:site" data-cui-lang="%s" data-cui-dir="%s">
<aside data-cui-outlet="l:site#aside"></aside><main data-cui-layout-slot="l:site" role="main"><p id="page">%s</p><a id="arabic" href="/ar">Arabic</a><a id="english" href="/">English</a></main>
</div><script src="/__gofastr/runtime.js"></script></body></html>`, language, direction, language, direction, language)
	})
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)
	ctx := chromedptest.Context(t)
	if err := chromedp.Run(ctx, chromedp.Navigate(server.URL), chromedp.WaitVisible(`#arabic`, chromedp.ByID)); err != nil {
		t.Fatal(err)
	}
	var loaded bool
	if err := chromedp.Run(ctx, chromedp.Evaluate(`!!window.__gofastr.loadedModules?.['doc-dir']`, &loaded)); err != nil {
		t.Fatal(err)
	}
	if loaded {
		t.Fatal("LTR boot loaded direction behavior")
	}
	if err := chromedp.Run(ctx, chromedp.Click(`#arabic`, chromedp.ByID), chromedp.Poll(`document.documentElement.lang === 'ar' && document.documentElement.dir === 'rtl'`, nil, chromedp.WithPollingTimeout(4*time.Second))); err != nil {
		t.Fatalf("static envelope did not switch to RTL: %v", err)
	}
	if err := chromedp.Run(ctx, chromedp.Click(`#english`, chromedp.ByID), chromedp.Poll(`document.documentElement.lang === 'en' && document.documentElement.dir === 'ltr'`, nil, chromedp.WithPollingTimeout(4*time.Second))); err != nil {
		t.Fatalf("cached English page did not restore LTR: %v", err)
	}
}
