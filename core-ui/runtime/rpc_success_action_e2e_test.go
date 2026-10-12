package runtime

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/internal/chromedptest"
	"github.com/chromedp/chromedp"
)

// An RPC carrying data-cui-rpc-success-action puts that action on its
// success toast: a button labelled as the attribute says, wired as the
// attribute's RPC, so pressing Undo posts the undo endpoint and toasts
// its own answer. The toast lives longer than a plain one, so there is
// time to press it.
func TestRPCSuccessToastCarriesItsAction(t *testing.T) {
	js, err := RuntimeJS()
	if err != nil {
		t.Fatal(err)
	}
	var undone atomic.Int32
	mux := http.NewServeMux()
	mux.HandleFunc("/__gofastr/runtime.js", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/javascript")
		w.Write([]byte(js))
	})
	handleRuntimeModules(t, mux)
	mux.HandleFunc("/__gofastr/del", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"success":true}`)
	})
	mux.HandleFunc("/__gofastr/undo", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			undone.Add(1)
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"success":true}`)
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, `<!doctype html><html><head><title>rpc action</title></head><body>
<main role="main" tabindex="-1">
<button id="del" type="button" data-cui-rpc="/__gofastr/del" data-cui-rpc-method="DELETE"
  data-cui-rpc-success-toast="Invoice deleted"
  data-cui-rpc-success-action='{"label":"Undo","attrs":{"data-cui-rpc":"/__gofastr/undo","data-cui-rpc-method":"POST","data-cui-rpc-success-toast":"Invoice restored"}}'>Delete</button>
</main>
`+toastStackHTML()+`
<script src="/__gofastr/runtime.js"></script>
</body></html>`)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	ctx := chromedptest.Context(t, chromedptest.Timeout(90*time.Second))
	var label string
	var ttl int
	if err := chromedp.Run(ctx,
		chromedp.Navigate(srv.URL+"/"),
		chromedp.WaitVisible(`#del`, chromedp.ByID),
		chromedp.Click(`#del`, chromedp.ByID),
		chromedp.WaitVisible(`[data-hui-toast-action]`, chromedp.ByQuery),
		chromedp.Evaluate(`document.querySelector('[data-hui-toast-action]').textContent`, &label),
		chromedp.Evaluate(`+document.querySelector('[data-hui-toast-id]').getAttribute('data-hui-toast-ttl-ms')`, &ttl),
		chromedp.Click(`[data-hui-toast-action]`, chromedp.ByQuery),
	); err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	if label != "Undo" {
		t.Errorf("action label = %q, want Undo", label)
	}
	if ttl < 10000 {
		t.Errorf("an action toast lives %dms, want at least 10000", ttl)
	}
	var restored bool
	if err := chromedp.Run(ctx, chromedp.Poll(`[...document.querySelectorAll('[data-hui-toast-title]')].some(e => e.textContent === 'Invoice restored')`,
		&restored, chromedp.WithPollingTimeout(10*time.Second))); err != nil || !restored {
		t.Fatalf("the undo's own toast never showed: %v", err)
	}
	if undone.Load() != 1 {
		t.Errorf("undo endpoint posted %d times, want 1", undone.Load())
	}
}
