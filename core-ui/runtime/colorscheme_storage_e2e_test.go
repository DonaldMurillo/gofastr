package runtime

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/internal/chromedptest"
	"github.com/chromedp/cdproto/emulation"
	"github.com/chromedp/chromedp"
)

func TestColorSchemeAndThemeFollowOtherTabs(t *testing.T) {
	js, err := ColorSchemeJS()
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "<!doctype html><html><head><script>"+js+"</script></head><body>Theme</body></html>")
	}))
	defer srv.Close()
	ctx := chromedptest.Context(t, chromedptest.Timeout(45*time.Second))
	if err := chromedp.Run(ctx, chromedp.Navigate(srv.URL), chromedp.Evaluate(`__gofastr_colorScheme.set('light')`, nil)); err != nil {
		t.Fatal(err)
	}
	other, cancel := chromedp.NewContext(ctx)
	defer cancel()
	if err := chromedp.Run(other,
		chromedp.Navigate(srv.URL),
		chromedp.Evaluate(`__gofastr_colorScheme.set('dark'); __gofastr_theme.set('cui-theme-abc')`, nil),
	); err != nil {
		t.Fatal(err)
	}
	if err := chromedp.Run(ctx,
		chromedp.Poll(`document.documentElement.dataset.colorScheme === 'dark' && document.documentElement.classList.contains('cui-theme-abc')`, nil),
	); err != nil {
		t.Fatalf("other tab's preference did not arrive: %v", err)
	}
	if err := chromedp.Run(other, chromedp.Evaluate(`localStorage.clear()`, nil)); err != nil {
		t.Fatal(err)
	}
	if err := chromedp.Run(ctx,
		chromedp.Poll(`!document.documentElement.classList.contains('cui-theme-abc') && __gofastr_colorScheme.get() === 'auto'`, nil),
		emulation.SetEmulatedMedia().WithFeatures([]*emulation.MediaFeature{{Name: "prefers-color-scheme", Value: "dark"}}),
		chromedp.Poll(`document.documentElement.dataset.colorScheme === 'dark'`, nil),
		emulation.SetEmulatedMedia().WithFeatures([]*emulation.MediaFeature{{Name: "prefers-color-scheme", Value: "light"}}),
		chromedp.Poll(`document.documentElement.dataset.colorScheme === 'light'`, nil),
	); err != nil {
		t.Fatalf("returning to automatic mode lost OS updates: %v", err)
	}
}
