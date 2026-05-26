package uihost

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/chromedp/chromedp"

	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core/render"
)

// TestNativeForm_UnadornedFormSubmitsNatively pins the canonical
// "host writes a plain form, the framework gets out of the way" case.
// A form with NO enctype attribute and NO data-fui-spa MUST submit
// browser-native (urlencoded), the server's Set-Cookie MUST stick,
// and the server's 303 redirect MUST be followed by the browser.
//
// This is the regression a host author worried about: "if I forget to
// add enctype, does my plain login form still work?" Answer: yes.
func TestNativeForm_UnadornedFormSubmitsNatively(t *testing.T) {
	type submitRecord struct {
		contentType string
		formValues  map[string]string
	}
	var got atomic.Pointer[submitRecord]

	mux := http.NewServeMux()

	// /submit: standard server-side handler. Reads urlencoded form,
	// sets a cookie, 303 to /landed. Mirrors what auth/login does.
	mux.HandleFunc("/submit", func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		rec := &submitRecord{
			contentType: r.Header.Get("Content-Type"),
			formValues:  map[string]string{},
		}
		for k, v := range r.PostForm {
			if len(v) > 0 {
				rec.formValues[k] = v[0]
			}
		}
		got.Store(rec)
		http.SetCookie(w, &http.Cookie{
			Name:  "session",
			Value: "real-session-token",
			Path:  "/",
		})
		http.Redirect(w, r, "/landed", http.StatusSeeOther)
	})

	// The two pages get served via the UIHost so the runtime + chrome
	// pipeline is exercised end-to-end.
	application := app.NewApp("nf")
	application.RegisterScreen(
		app.NewScreen("/", &rawHTMLComp{html: `
		<form id="f" action="/submit" method="POST">
		  <input id="email" name="email" type="text">
		  <input id="password" name="password" type="password">
		  <button id="go" type="submit">Sign in</button>
		</form>`}).WithTitle("login"),
		nil,
	)
	application.RegisterScreen(
		app.NewScreen("/landed", &rawHTMLComp{html: `<h1 id="landed">landed</h1>`}).WithTitle("landed"),
		nil,
	)

	ds := New(application)
	mux.Handle("/", ds)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	chCtx := newE2EChromeForUIHost(t)
	var url string
	var cookies string
	var beforeShot, afterShot []byte
	err := chromedp.Run(chCtx,
		chromedp.Navigate(srv.URL+"/"),
		chromedp.WaitVisible(`#go`, chromedp.ByID),
		chromedp.SendKeys(`#email`, "alice@example.com", chromedp.ByID),
		chromedp.SendKeys(`#password`, "hunter22", chromedp.ByID),
		chromedp.FullScreenshot(&beforeShot, 90),
		chromedp.Click(`#go`, chromedp.ByID),
		chromedp.WaitVisible(`#landed`, chromedp.ByID),
		chromedp.Evaluate(`window.location.pathname`, &url),
		chromedp.Evaluate(`document.cookie`, &cookies),
		chromedp.FullScreenshot(&afterShot, 90),
	)
	if err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	rec := got.Load()
	if rec == nil {
		t.Fatal("/submit was never reached")
	}
	if !strings.HasPrefix(strings.ToLower(rec.contentType), "application/x-www-form-urlencoded") {
		t.Errorf("Content-Type = %q; want urlencoded (browser-native submit, NOT intercepted as JSON)", rec.contentType)
	}
	if rec.formValues["email"] != "alice@example.com" {
		t.Errorf("server didn't parse email field: %+v", rec.formValues)
	}
	if url != "/landed" {
		t.Errorf("browser didn't follow 303 to /landed; URL = %q", url)
	}
	if !strings.Contains(cookies, "session=real-session-token") {
		t.Errorf("Set-Cookie didn't stick across the native redirect; cookies=%q", cookies)
	}
	_ = os.WriteFile("/tmp/gofastr-vis-native-form-before.png", beforeShot, 0o644)
	_ = os.WriteFile("/tmp/gofastr-vis-native-form-after.png", afterShot, 0o644)
}

// rawHTMLComp emits a literal HTML fragment for tests that want to
// control the exact DOM (e.g. precise form shape, no extra wrapping).
type rawHTMLComp struct{ html string }

func (r *rawHTMLComp) Render() render.HTML { return render.HTML(r.html) }

// suppress unused-warning on fmt import that build tools sometimes add.
var _ = fmt.Sprintf
