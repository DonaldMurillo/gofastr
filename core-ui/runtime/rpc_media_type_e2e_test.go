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

func TestRPCMixedCaseJSONResponse(t *testing.T) {
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
	mux.HandleFunc("/rpc", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", r.URL.Query().Get("ct"))
		fmt.Fprint(w, `{"v":1}`)
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, `<!doctype html><html><body>
<button id="go" data-cui-rpc="/rpc" data-cui-rpc-signal="result">Go</button>
<script src="/__gofastr/runtime.js"></script></body></html>`)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	ctx := chromedptest.Context(t, chromedptest.Timeout(30*time.Second))
	if err := chromedp.Run(ctx, chromedp.Navigate(srv.URL), chromedp.WaitVisible(`#go`)); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		ct   string
		want string
	}{
		{"Application/JSON; charset=utf-8", "object"},
		{"", "string"},
		{"text/plain", "string"},
	} {
		if err := chromedp.Run(ctx,
			chromedp.Evaluate(fmt.Sprintf(`window.__gofastr.setSignal('result', null); document.querySelector('#go').setAttribute('data-cui-rpc', '/rpc?ct=' + encodeURIComponent(%q))`, tc.ct), nil),
			chromedp.Click(`#go`),
			chromedp.Poll(fmt.Sprintf(`typeof window.__gofastr._signals.result?.value === %q && window.__gofastr._signals.result.value !== null`, tc.want), nil),
		); err != nil {
			t.Fatalf("Content-Type %q: %v", tc.ct, err)
		}
	}
}
