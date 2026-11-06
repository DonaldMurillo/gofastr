package desktop

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/DonaldMurillo/gofastr/internal/chromedptest"
	"github.com/chromedp/chromedp"
)

func TestBridgeScriptsIgnoreChildFrames(t *testing.T) {
	script := BootstrapJS("main", false) + ";\n" + string(BridgeJS(Manifest{}))
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprintf(w, `<script>window.loads=0;window.__gofastr={loadModule:()=>{window.loads++;return Promise.resolve()},desktop:{}};</script><script>%s</script>`, script)
		if r.URL.Path == "/" {
			fmt.Fprint(w, `<iframe src="/child"></iframe>`)
		}
	}))
	defer server.Close()
	ctx := chromedptest.Context(t)
	var result struct {
		Main, Child bool
		Loads       int
	}
	if err := chromedp.Run(ctx, chromedp.Navigate(server.URL), chromedp.Poll(`document.querySelector('iframe').contentWindow.document.readyState === 'complete'`, nil), chromedp.Evaluate(`({Main:!!window.__gofastr_desktop,Child:!!frames[0].__gofastr_desktop,Loads:frames[0].loads})`, &result)); err != nil {
		t.Fatal(err)
	}
	if !result.Main || result.Child || result.Loads != 0 {
		t.Fatalf("top-frame scripts: %+v, want main marker only and no child bridge load", result)
	}
}
