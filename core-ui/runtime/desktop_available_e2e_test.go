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

// desktop.available is the boolean the d.ts and the docs promise: false
// in a plain browser, so a truthiness guard skips the bridge there, and
// true once the host marker is present. A function here passed every
// guard everywhere.
func TestDesktopAvailableIsABoolean(t *testing.T) {
	module, ok := Module("desktop")
	if !ok {
		t.Fatal("desktop module not embedded")
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = fmt.Fprintf(w, `<!doctype html><html><head><meta charset="utf-8"></head><body>
<script>%s</script>
</body></html>`, module)
	}))
	t.Cleanup(server.Close)

	ctx := chromedptest.Context(t, chromedptest.Timeout(60*time.Second))
	if err := chromedp.Run(ctx, chromedp.Navigate(server.URL)); err != nil {
		t.Fatalf("chromedp navigate: %v", err)
	}
	var plain, marked string
	if err := chromedp.Run(ctx,
		chromedp.Evaluate(`typeof __gofastr.desktop.available + ':' + __gofastr.desktop.available + ':' + (__gofastr.desktop.available ? 'guarded-in' : 'guarded-out')`, &plain),
		chromedp.Evaluate(`window.__gofastr_desktop = {}; typeof __gofastr.desktop.available + ':' + __gofastr.desktop.available`, &marked),
	); err != nil {
		t.Fatalf("chromedp evaluate: %v", err)
	}
	if plain != "boolean:false:guarded-out" {
		t.Errorf("plain browser available = %s, want boolean:false:guarded-out", plain)
	}
	if marked != "boolean:true" {
		t.Errorf("with the host marker available = %s, want boolean:true", marked)
	}
}
