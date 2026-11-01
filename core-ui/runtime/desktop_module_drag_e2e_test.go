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

// A drag region may be a whole card holding controls. Only a primary
// mousedown on the region itself starts a native window drag: a button,
// a field, a link or a data-cui-window-no-drag subtree inside it keeps
// its click, and so does a right or middle press, because the native
// host takes the mouse the moment a drag starts.
func TestWindowDragSkipsControlsAndRightClick(t *testing.T) {
	module, ok := Module("desktop")
	if !ok {
		t.Fatal("desktop module not embedded")
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = fmt.Fprintf(w, `<!doctype html><html><head><meta charset="utf-8"></head><body>
<div id="card" data-cui-window-drag style="padding:20px">
  <span id="title">Quick note</span>
  <button id="btn" type="button"><span id="btnlabel">Save</span></button>
  <input id="field">
  <a id="link" href="#x">More</a>
  <div id="nodrag" data-cui-window-no-drag><span id="nodraginner">menu</span></div>
</div>
<script>window.__dragMessages = []; window.chrome = window.chrome || {}; window.chrome.webview = {postMessage: function (message) { window.__dragMessages.push(message); }};</script>
<script>%s</script>
</body></html>`, module)
	}))
	t.Cleanup(server.Close)

	ctx := chromedptest.Context(t, chromedptest.Timeout(60*time.Second))
	if err := chromedp.Run(ctx, chromedp.Navigate(server.URL)); err != nil {
		t.Fatalf("chromedp navigate: %v", err)
	}
	press := func(id string, button int) int {
		t.Helper()
		var n int
		js := fmt.Sprintf(`(() => {
  window.__dragMessages.length = 0;
  document.getElementById(%q).dispatchEvent(new MouseEvent('mousedown', { bubbles: true, button: %d }));
  return window.__dragMessages.length;
})()`, id, button)
		if err := chromedp.Run(ctx, chromedp.Evaluate(js, &n)); err != nil {
			t.Fatalf("chromedp evaluate: %v", err)
		}
		return n
	}
	if n := press("title", 0); n != 1 {
		t.Fatalf("primary press on the drag region posted %d drag messages, want 1", n)
	}
	for _, id := range []string{"btnlabel", "field", "link", "nodraginner"} {
		if n := press(id, 0); n != 0 {
			t.Errorf("primary press on #%s inside the drag region started a window drag; a control keeps its click", id)
		}
	}
	for _, b := range []int{1, 2} {
		if n := press("title", b); n != 0 {
			t.Errorf("mouse button %d on the drag region started a window drag; only the primary button drags", b)
		}
	}
}
