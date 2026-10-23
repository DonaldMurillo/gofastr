package uihost

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/chromedp/chromedp"

	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/internal/chromedptest"
)

// A client navigation between an English and an Arabic page flips
// <html dir> with the language: the core copies lang, and the doc-dir
// module, fetched only once a right-to-left marker appears, copies the
// direction. A left-to-right page never fetches it.
func TestE2E_DocDirFollowsNavigation(t *testing.T) {
	if testing.Short() {
		t.Skip("browser E2E disabled in short mode")
	}
	_ = browserExecutable(t)

	application := app.NewApp("DirSite").WithLangFunc(func(path string) string {
		if strings.HasPrefix(path, "/ar") {
			return "ar"
		}
		return ""
	})
	application.RegisterScreen(app.NewScreen("/", &plainSiteComp{marker: "EN", links: [][2]string{{"goAr", "/ar"}}}).WithTitle("Home"), nil)
	application.RegisterScreen(app.NewScreen("/ar", &plainSiteComp{marker: "AR", links: [][2]string{{"goEn", "/"}}}).WithTitle("Ar"), nil)
	host := New(application)

	var mu sync.Mutex
	fetched := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/__gofastr/runtime/"+app.DocDirBehaviorName+".js" {
			mu.Lock()
			fetched++
			mu.Unlock()
		}
		host.ServeHTTP(w, r)
	}))
	t.Cleanup(srv.Close)

	ctx := chromedptest.Context(t, chromedptest.Timeout(60*time.Second))
	dirIs := func(want string) bool {
		var ok bool
		_ = chromedp.Run(ctx, chromedp.Poll(`document.documentElement.dir === '`+want+`'`, &ok,
			chromedp.WithPollingTimeout(10*time.Second)))
		return ok
	}
	if err := chromedp.Run(ctx,
		chromedp.Navigate(srv.URL+"/"),
		chromedp.WaitVisible(`#goAr`, chromedp.ByID),
	); err != nil {
		t.Fatal(err)
	}
	if !dirIs("ltr") {
		t.Fatal("the English page is not ltr")
	}
	mu.Lock()
	early := fetched
	mu.Unlock()
	if early != 0 {
		t.Errorf("a left-to-right page fetched %s", app.DocDirBehaviorName)
	}
	for _, step := range []struct{ link, marker, dir, lang string }{
		{"#goAr", "AR", "rtl", "ar"},
		{"#goEn", "EN", "ltr", "en"},
	} {
		var ok bool
		if err := chromedp.Run(ctx,
			chromedp.Click(step.link, chromedp.ByID),
			chromedp.Poll(`document.getElementById('mk-`+step.marker+`') !== null && document.documentElement.lang === '`+step.lang+`'`, &ok,
				chromedp.WithPollingTimeout(15*time.Second)),
		); err != nil {
			t.Fatalf("click %s: %v", step.link, err)
		}
		if !dirIs(step.dir) {
			var got string
			_ = chromedp.Run(ctx, chromedp.Evaluate(`document.documentElement.dir + ' ' + document.documentElement.lang + ' ' + JSON.stringify([...document.querySelectorAll('[data-cui-lang]')].map(e => e.tagName + ':' + e.getAttribute('data-cui-lang') + ':' + e.getAttribute('data-cui-dir'))) + ' ' + JSON.stringify(window.__gofastr.loadedModules)`, &got))
			t.Fatalf("after %s the document is dir=%q, want %s", step.link, got, step.dir)
		}
	}
}
