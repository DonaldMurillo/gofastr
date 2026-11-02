package lib

import (
	"net/http"
	"strings"
)

// IsForm is a cross-package content-type predicate that accepts
// multipart.
func IsForm(r *http.Request) bool { // want IsForm:"acceptsMultipart"
	ct := strings.ToLower(r.Header.Get("Content-Type"))
	return strings.HasPrefix(ct, "application/x-www-form-urlencoded") || strings.HasPrefix(ct, "multipart/form-data")
}

// ParseAny parses either encoding: a cross-package reader.
func ParseAny(r *http.Request) error { // want ParseAny:"readsMultipart"
	if strings.HasPrefix(r.Header.Get("Content-Type"), "multipart/form-data") {
		return r.ParseMultipartForm(1 << 20)
	}
	return r.ParseForm()
}
