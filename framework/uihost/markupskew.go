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

func writeStaleMarkupReload(w http.ResponseWriter, r *http.Request, path, swapLayer string) {
	h := w.Header()
	h.Set("Content-Type", "text/html; charset=utf-8")
	h.Set("X-Gofastr-Partial", "true")
	if swapLayer != "" {
		h.Set("X-Gofastr-Swap", escapeLayerKey(swapLayer))
	}
	h.Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusConflict)
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
	esc := html.EscapeString(strings.ReplaceAll(target, "'", "%27"))
	fmt.Fprintf(w, `<meta http-equiv="refresh" content="0; url='%s'"><p role="alert">%s <a href="%s">%s</a></p>`, esc, updated, esc, reload)
}
