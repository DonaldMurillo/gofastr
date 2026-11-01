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

// A browser that refuses Web Storage (blocked site data, some private
// modes, a sandboxed frame) makes every localStorage access throw. The
// theme toggle must still switch the page for the rest of the visit, and
// get() must report what set() chose, or a toggle that cycles from get()
// is stuck on its first value.
func TestColorSchemeToggleWorksWithStorageBlocked(t *testing.T) {
	js, err := ColorSchemeJS()
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `<!doctype html><html><head><script>
Object.defineProperty(window, 'localStorage', { configurable: true, get() { throw new DOMException('blocked', 'SecurityError'); } });
</script><script>`+js+`</script></head><body>Theme</body></html>`)
	}))
	defer srv.Close()
	ctx := chromedptest.Context(t, chromedptest.Timeout(45*time.Second))
	var scheme, got, theme, themeGot string
	if err := chromedp.Run(ctx,
		chromedp.Navigate(srv.URL),
		chromedp.Evaluate(`__gofastr_colorScheme.set(document.documentElement.dataset.colorScheme === 'dark' ? 'light' : 'dark'); 0`, nil),
		chromedp.Evaluate(`document.documentElement.dataset.colorScheme`, &scheme),
		chromedp.Evaluate(`__gofastr_colorScheme.get()`, &got),
		chromedp.Evaluate(`__gofastr_theme.set('cui-theme-abc'); [...document.documentElement.classList].join(' ')`, &theme),
		chromedp.Evaluate(`__gofastr_theme.get()`, &themeGot),
	); err != nil {
		t.Fatal(err)
	}
	if got != scheme {
		t.Errorf("colorScheme.get() = %q after set() applied %q: a toggle reading get() is stuck", got, scheme)
	}
	if theme != "cui-theme-abc" || themeGot != "cui-theme-abc" {
		t.Errorf("theme.set with storage blocked: classes %q, get() %q; want cui-theme-abc for both", theme, themeGot)
	}
	var light string
	if err := chromedp.Run(ctx,
		chromedp.Evaluate(`__gofastr_colorScheme.set('light'); __gofastr_colorScheme.set('dark'); document.documentElement.dataset.colorScheme`, &light),
	); err != nil {
		t.Fatal(err)
	}
	if light != "dark" {
		t.Errorf("set('dark') with storage blocked left data-color-scheme %q", light)
	}
}
