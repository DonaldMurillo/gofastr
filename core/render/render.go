package render

import (
	"net/http"
)

// RespondHTML writes h with status 200 and Content-Type text/html; charset=utf-8.
func RespondHTML(w http.ResponseWriter, h HTML) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(h.String()))
}

// HTMLHandler adapts fn into an http.HandlerFunc that writes its result with [RespondHTML].
func HTMLHandler(fn func(r *http.Request) HTML) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		h := fn(r)
		RespondHTML(w, h)
	}
}
