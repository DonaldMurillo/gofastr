package uihost

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/chromedp/chromedp"

	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core-ui/app/decide"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/internal/chromedptest"
)

// TestPartialRedirect_RuntimeFollowsXGofastrLocation pins the full
// chain end-to-end in a real browser:
//
//  1. SPA-nav (window.__gofastr.navigate("/dash")) issues a partial
//     fetch with X-Gofastr-Navigate: 1.
//  2. handlePartialPage sees the SessionPolicy redirect, returns
//     200 + X-Gofastr-Location: /login + empty body.
//  3. runtime.js loadPage reads the header and re-loadPage()s /login.
//  4. URL bar shows /login (not /dash).
//  5. Login page content is rendered.
//
// This is the gap the prior httptest only half-covered (server side).
func TestPartialRedirect_RuntimeFollowsXGofastrLocation(t *testing.T) {
	application := app.NewApp("e2e")

	// Public landing: minimal HTML with a marker so we can wait on it.
	application.RegisterScreen(
		app.NewScreen("/", &textComp{tag: "landing"}).WithTitle("Landing"),
		nil,
	)
	// Public login: what we expect to land on after the redirect.
	application.RegisterScreen(
		app.NewScreen("/login", &textComp{tag: "login-page"}).WithTitle("Login"),
		nil,
	)
	// Gated screen: SessionPolicy with default redirect to /login.
	gated := app.NewScreen("/dash", &textComp{tag: "dashboard"}).
		WithTitle("Dashboard").
		WithPolicy(app.PolicyFunc(func(ctx context.Context) app.Decision {
			return decide.Redirect("/login")
		}))
	application.RegisterScreen(gated, nil)

	ds := New(application)
	srv := httptest.NewServer(ds)
	t.Cleanup(srv.Close)

	ctx := chromedptest.Context(t, chromedptest.AllocatorOptions(chromedp.ExecPath(browserExecutable(t))))

	var finalURL, body string
	var beforeShot, afterShot []byte
	err := chromedp.Run(ctx,
		chromedp.Navigate(srv.URL+"/"),
		chromedp.WaitVisible(`[data-text="landing"]`, chromedp.ByQuery),
		chromedp.FullScreenshot(&beforeShot, 90),
		// Trigger SPA nav to the gated path. The runtime must:
		//  - fetch /dash with X-Gofastr-Navigate: 1
		//  - read X-Gofastr-Location: /login from the 200 response
		//  - call loadPage("/login")
		//  - push /login into the URL bar
		chromedp.Evaluate(`window.__gofastr.navigate("/dash")`, nil),
		chromedp.WaitVisible(`[data-text="login-page"]`, chromedp.ByQuery),
		chromedp.Evaluate(`window.location.pathname`, &finalURL),
		chromedp.Evaluate(`document.body.innerText`, &body),
		chromedp.FullScreenshot(&afterShot, 90),
	)
	if err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	if finalURL != "/login" {
		t.Errorf("URL bar should reflect the redirect destination, got %q (want /login)", finalURL)
	}
	if !strings.Contains(body, "login-page") {
		t.Errorf("login page body not rendered after partial-redirect, got %q", body)
	}
	if strings.Contains(body, "dashboard") {
		t.Errorf("dashboard body leaked through despite policy redirect, got %q", body)
	}
	// Visual evidence: saved to /tmp so a human / next-session agent
	// can inspect. PNG bytes; non-fatal if write fails.
	_ = os.WriteFile("/tmp/gofastr-vis-partial-redirect-before.png", beforeShot, 0o644)
	_ = os.WriteFile("/tmp/gofastr-vis-partial-redirect-after.png", afterShot, 0o644)
}

// A policy sending a soft navigation to another site, an identity
// provider that answers no CORS: fetch could not follow the 303 there
// and the reader got a "could not load" toast. The host names the URL
// and the runtime loads it as a page.
func TestPartialRedirect_ExternalLoadsPage(t *testing.T) {
	idp := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, `<!doctype html><title>idp</title><p id="idp">idp login</p>`)
	}))
	t.Cleanup(idp.Close)
	application := app.NewApp("e2e")
	application.RegisterScreen(app.NewScreen("/", &textComp{tag: "landing"}).WithTitle("Landing"), nil)
	application.RegisterScreen(app.NewScreen("/away", &textComp{tag: "away"}).WithTitle("Away").
		WithPolicy(app.PolicyFunc(func(ctx context.Context) app.Decision {
			return decide.Redirect(idp.URL + "/login")
		})), nil)
	srv := httptest.NewServer(New(application))
	t.Cleanup(srv.Close)

	ctx := chromedptest.Context(t, chromedptest.AllocatorOptions(chromedp.ExecPath(browserExecutable(t))))
	if err := chromedp.Run(ctx,
		chromedp.Navigate(srv.URL+"/"),
		chromedp.WaitVisible(`[data-text="landing"]`, chromedp.ByQuery),
		chromedp.Evaluate(`window.__gofastr.navigate("/away")`, nil),
	); err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	var href string
	for deadline := time.Now().Add(10 * time.Second); ; time.Sleep(50 * time.Millisecond) {
		var text string
		err := chromedp.Run(ctx,
			chromedp.Evaluate(`location.href`, &href),
			chromedp.Evaluate(`document.getElementById('idp')?.textContent ?? ''`, &text))
		if err == nil && text == "idp login" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("the identity provider never loaded: at %q (%v)", href, err)
		}
	}
	if href != idp.URL+"/login" {
		t.Errorf("landed on %q, want %s/login", href, idp.URL)
	}
}

// textComp is a minimal component for the e2e harness: renders a div
// with a data-text marker so chromedp can wait on it.
type textComp struct{ tag string }

func (t *textComp) Render() render.HTML {
	return render.HTML(`<div data-text="` + t.tag + `">` + t.tag + `</div>`)
}
