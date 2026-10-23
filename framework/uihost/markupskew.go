package uihost

import (
	"fmt"
	"html"
	"net/http"
	"strings"

	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core-ui/runtime"
	"github.com/DonaldMurillo/gofastr/core/handler"
	"github.com/DonaldMurillo/gofastr/core/i18n"
)

// staleMarkupClient reports whether a navigation fetch comes from a
// browser runtime that reads a different markup generation than this
// host renders (core-ui/runtime.MarkupVersion): a tab opened before a
// deploy. Only a browser kernel reads the markup, so only a script
// fetch counts, and Sec-Fetch-Mode is how the request says so (a
// document load is "navigate", a runtime fetch "cors" or
// "same-origin"). A request without it (curl, a Go test, a browser
// that predates Fetch Metadata) gets the ordinary partial.
func staleMarkupClient(r *http.Request) bool {
	if r.Header.Get(runtime.MarkupHeader) == runtime.MarkupVersion {
		return false
	}
	switch r.Header.Get("Sec-Fetch-Mode") {
	case "cors", "same-origin", "no-cors":
		return true
	}
	return false
}

// writeStaleMarkupReload answers a stale runtime's navigation with a
// body that reloads the document at the destination instead of markup
// its kernel cannot read. Every runtime generation applies an HTML
// partial the same way, so the answer needs no client support: a 409
// partial (never cached as a screen) whose swap key is the layer the
// live DOM holds, carrying a meta refresh. Inserting it starts the
// browser's own navigation, which loads the destination whole with
// today's runtime. The link is the fallback where refresh is disabled.
func writeStaleMarkupReload(w http.ResponseWriter, r *http.Request, path, swapLayer string) {
	h := w.Header()
	h.Set("Content-Type", "text/html; charset=utf-8")
	h.Set("X-Gofastr-Partial", "true")
	if swapLayer != "" {
		h.Set("X-Gofastr-Swap", swapLayer)
	}
	h.Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusConflict)
	// The refresh is no link the page rewriter reads, so the locale
	// prefix the router stripped goes back on here.
	// path is in routing form, so a key segment holding "/" goes back
	// out as %2F.
	target := i18n.PathPrefix(r.Context()) + app.EscapeRoutePath(path)
	if r.URL.RawQuery != "" {
		target += "?" + r.URL.RawQuery
	}
	updated := html.EscapeString(pageAppUpdated.Text(r.Context(), nil))
	reload := html.EscapeString(pageReloadPage.Text(r.Context(), nil))
	if !handler.IsSafeRelativePath(target) {
		fmt.Fprintf(w, `<p role="alert">%s %s</p>`, updated, reload)
		return
	}
	// The refresh URL is single-quoted inside the attribute, so a quote
	// in the query must not end it early.
	esc := html.EscapeString(strings.ReplaceAll(target, "'", "%27"))
	fmt.Fprintf(w, `<meta http-equiv="refresh" content="0; url='%s'"><p role="alert">%s <a href="%s">%s</a></p>`, esc, updated, esc, reload)
}
