package runtime

import (
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/internal/chromedptest"
	"github.com/chromedp/chromedp"
)

// TestRPCErrorToastShowsServerMessage: a non-form data-cui-rpc button
// carrying data-cui-rpc-error-toast turns a refused answer (a 409 for a
// record other rows still reference) into a toast titled by the
// attribute, with the server's JSON "error" as its body. Before it the
// runtime returned on !r.ok and the click ended in silence.
func TestRPCErrorToastShowsServerMessage(t *testing.T) {
	page := `<!doctype html><html><head><title>rpc error</title></head><body>
<button id="del" data-cui-rpc="/api/things/1" data-cui-rpc-method="DELETE"
  data-cui-rpc-error-toast="Could not delete this thing.">Delete</button>
` + toastStackHTML() + `
<script src="/__gofastr/runtime.js"></script>
</body></html>`
	url := startPollServer(t, page, map[string]http.HandlerFunc{
		"/api/things/1": func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusConflict)
			fmt.Fprint(w, `{"code":409,"error":"conflict: the record is referenced by, or references, another record","success":false}`)
		},
	})

	ctx := chromedptest.Context(t, chromedptest.Timeout(90*time.Second))
	var text string
	if err := chromedp.Run(ctx,
		chromedp.Navigate(url+"/"),
		chromedp.WaitVisible(`#del`, chromedp.ByID),
		chromedp.Click(`#del`, chromedp.ByID),
		chromedp.WaitVisible(`[data-hui-toast-id]`, chromedp.ByQuery),
		chromedp.Evaluate(`document.querySelector('[data-hui-toast-id]').textContent`, &text),
	); err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	if !strings.Contains(text, "Could not delete this thing.") {
		t.Errorf("toast text %q lacks the attribute's title", text)
	}
	if !strings.Contains(text, "referenced by, or references, another record") {
		t.Errorf("toast text %q lacks the server's error message", text)
	}
}
