package helper

import (
	"io"
	"net/http"
)

func capped(w http.ResponseWriter, r *http.Request) {
	limitBody(w, r)
	_, _ = io.ReadAll(r.Body) // capped by limitBody, no diagnostic
}
