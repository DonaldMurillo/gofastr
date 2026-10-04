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

func TestDesktopStartDragUsesWebView2Channel(t *testing.T) {
	module, ok := Module("desktop")
	if !ok {
		t.Fatal("desktop module not embedded")
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = fmt.Fprintf(w, `<!doctype html><html><head><meta charset="utf-8"></head><body>
<script>window.__dragMessages = []; window.chrome = window.chrome || {}; window.chrome.webview = {postMessage: function (message) { window.__dragMessages.push(message); }};</script>
<script>%s</script>
<script>window.__gofastr.desktop.window.startDrag();</script>
</body></html>`, module)
	}))
	t.Cleanup(server.Close)

	ctx := chromedptest.Context(t, chromedptest.Timeout(60*time.Second))
	if err := chromedp.Run(ctx, chromedp.Navigate(server.URL)); err != nil {
		t.Fatalf("chromedp navigate: %v", err)
	}
	var got string
	if err := chromedp.Run(ctx, chromedp.Evaluate(`window.__dragMessages[0]`, &got)); err != nil {
		t.Fatalf("chromedp evaluate: %v", err)
	}
	if got != `{"type":"drag"}` {
		t.Fatalf("WebView2 drag message = %s, want {\"type\":\"drag\"}", got)
	}
}
