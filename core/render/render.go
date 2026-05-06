package render

import (
	"net/http"
)

// RespondHTML writes the given HTML to the response with the correct
// Content-Type header and a 200 OK status.
func RespondHTML(w http.ResponseWriter, h HTML) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(h.String()))
}

// HTMLHandler adapts a function that returns HTML into an http.HandlerFunc.
// This is the primary way to connect GoFastr templates to the Router.
func HTMLHandler(fn func(r *http.Request) HTML) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		h := fn(r)
		RespondHTML(w, h)
	}
}
