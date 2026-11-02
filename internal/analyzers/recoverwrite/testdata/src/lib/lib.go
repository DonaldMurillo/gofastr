// Package lib stands in for core/i18n: HTTPError writes its writer
// parameter through a second helper, so it carries a writesResponse
// fact into its importers.
package lib

import (
	"fmt"
	"net/http"
)

func HTTPError(w http.ResponseWriter, r *http.Request, code int, msg string) { // want HTTPError:`writesResponse\[0\]`
	send(r, w, code, msg)
}

func send(r *http.Request, w http.ResponseWriter, code int, msg string) { // want send:`writesResponse\[1\]`
	w.Header().Set("Content-Type", "text/plain")
	w.WriteHeader(code)
	fmt.Fprint(w, msg)
}

// Reads the writer's header only: not a writer.
func ContentType(w http.ResponseWriter) string { return w.Header().Get("Content-Type") }
