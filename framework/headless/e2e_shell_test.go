package headless

// Shared page shells and helpers for the e2e tests that need a second
// route (a client navigation lands on it) or chrome outside <main>
// (the part a client navigation keeps).

import (
	"context"
	"fmt"
	"net/http"
	"testing"

	"github.com/chromedp/chromedp"

	"github.com/DonaldMurillo/gofastr/core-ui/runtime"
)

// shellPage writes a page in the harness's shell, for a second route a
// client navigation lands on.
func shellPage(w http.ResponseWriter, body string) {
	chromeShellPage(w, "", body)
}

// chromeShellPage serves a page whose chrome sits OUTSIDE <main>, the
// part a client navigation keeps (a layout's header), with body inside
// main. An empty chrome renders no header.
func chromeShellPage(w http.ResponseWriter, chrome, body string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	header := ""
	if chrome != "" {
		header = "<header>" + chrome + "</header>"
	}
	fmt.Fprintf(w, `<!doctype html><html><head><title>headless</title>`+
		`<script type="application/json" id="gofastr-behaviors">%s</script></head><body>`+
		`%s<main role="main"><span id="ready">ready</span>%s</main>`+
		`<script src="/__gofastr/runtime.js"></script></body></html>`, runtime.BehaviorsJSON(), header, body)
}

// awayRoutes is the route manifest the client router needs to take a
// same-origin link to /away as a client navigation.
const awayRoutes = `<script type="application/json" id="gofastr-routes">[{"path":"/"},{"path":"/away"}]</script>`

// evalString runs js and returns its string result.
func evalString(t *testing.T, ctx context.Context, js string) string {
	t.Helper()
	var s string
	if err := chromedp.Run(ctx, chromedp.Evaluate(js, &s)); err != nil {
		t.Fatalf("eval %s: %v", js, err)
	}
	return s
}
