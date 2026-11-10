package setup

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/internal/browserpath"
	"github.com/DonaldMurillo/gofastr/internal/chromedptest"
	"github.com/chromedp/chromedp"
)

// A real browser follows the setup link from another site's page and
// lands on the wizard. The other site is 127.0.0.1 and the app is
// localhost: two sites to the browser, so the click is the cross-site
// navigation a hosted log viewer's link makes. Before the hop this ended
// on the 403 "setup token required" text with the token spent.
func TestSetupLinkClickedOnAnotherSite(t *testing.T) {
	if testing.Short() {
		t.Skip("browser E2E disabled in short mode")
	}
	browser, ok := browserpath.Find()
	if !ok {
		t.Skip("browser E2E requires Chrome, Chromium, or Edge")
	}
	done := false
	r := buildTestRunner(t, false, &done)
	app := httptest.NewServer(r.Handler(func() {}, nil, nil))
	defer app.Close()
	_, port, err := net.SplitHostPort(strings.TrimPrefix(app.URL, "http://"))
	if err != nil {
		t.Fatal(err)
	}
	link := "http://localhost:" + port + "/setup?token=" + r.token

	viewer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprintf(w, `<!doctype html><title>logs</title><a id="setup-link" href="%s">setup</a>`, link)
	}))
	defer viewer.Close()

	ctx := chromedptest.Context(t, chromedptest.AllocatorOptions(chromedp.ExecPath(browser)))
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	var body, location string
	if err := chromedp.Run(ctx,
		chromedp.Navigate(viewer.URL),
		chromedp.Click(`#setup-link`, chromedp.ByID),
		chromedp.WaitVisible(`#setup-field-FOO`, chromedp.ByID),
		chromedp.Location(&location),
		chromedp.Text(`body`, &body, chromedp.ByQuery),
	); err != nil {
		var text string
		_ = chromedp.Run(ctx, chromedp.Text(`body`, &text, chromedp.ByQuery))
		t.Fatalf("the wizard never appeared after the click: %v\npage: %q", err, text)
	}
	if location != "http://localhost:"+port+"/setup" {
		t.Fatalf("landed on %q, want the wizard at /setup with the token out of the URL", location)
	}
}
