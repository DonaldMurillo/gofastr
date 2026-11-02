package runtime

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/internal/chromedptest"
	"github.com/chromedp/chromedp"
)

// A navigation answer with no X-Gofastr-Title (uihost now always names
// one, falling back to the app name; another server may not) left the
// runtime decoding document.title, which is never encoded: "50% Off"
// threw URIError and navigation gave up. A header that is not valid
// percent-encoding (a proxy or another server writing it raw) is kept
// as sent.
func TestNavKeepsPercentTitle(t *testing.T) {
	js, err := RuntimeJS()
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/__gofastr/runtime.js", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/javascript")
		fmt.Fprint(w, js)
	})
	handleRuntimeModules(t, mux)
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		if r.Header.Get("X-Gofastr-Navigate") == "1" {
			w.Header().Set("X-Gofastr-Partial", "true")
			if r.URL.Path == "/raw" {
				w.Header().Set("X-Gofastr-Title", "Raw 50% Off")
				fmt.Fprint(w, `<p id="raw">raw screen</p>`)
				return
			}
			fmt.Fprint(w, `<p id="other">other screen</p><a id="go-raw" href="/raw">Raw</a>`)
			return
		}
		fmt.Fprint(w, `<!doctype html><html><head><title>50% Off Co.</title>
  <script type="application/json" id="gofastr-routes">[{"path":"/"},{"path":"/other"},{"path":"/raw"}]</script>
</head><body><a id="go" href="/other">Other</a><main>home</main>
<span id="ready">ready</span><script src="/__gofastr/runtime.js"></script></body></html>`)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	ctx := chromedptest.Context(t, chromedptest.Timeout(60*time.Second))
	var path, stamp, title, rawPath, rawStamp, rawTitle string
	if err := chromedp.Run(ctx,
		chromedp.Navigate(srv.URL+"/"),
		chromedp.WaitVisible(`#ready`, chromedp.ByID),
		chromedp.Evaluate(`window.__stamp = 'alive'`, nil),
		chromedp.Click(`#go`, chromedp.ByID),
		chromedp.WaitVisible(`#other`, chromedp.ByID),
		chromedp.Evaluate(`location.pathname`, &path),
		chromedp.Evaluate(`window.__stamp || 'gone'`, &stamp),
		chromedp.Evaluate(`document.title`, &title),
		chromedp.Click(`#go-raw`, chromedp.ByID),
		chromedp.WaitVisible(`#raw`, chromedp.ByID),
		chromedp.Evaluate(`location.pathname`, &rawPath),
		chromedp.Evaluate(`window.__stamp || 'gone'`, &rawStamp),
		chromedp.Evaluate(`document.title`, &rawTitle),
	); err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	if path != "/other" || stamp != "alive" || title != "50% Off Co." {
		t.Fatalf("path %q stamp %q title %q, want a client navigation keeping the title", path, stamp, title)
	}
	if rawPath != "/raw" || rawStamp != "alive" || rawTitle != "Raw 50% Off" {
		t.Fatalf("path %q stamp %q title %q, want a client navigation keeping the raw header", rawPath, rawStamp, rawTitle)
	}
}
