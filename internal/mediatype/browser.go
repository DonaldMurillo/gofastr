package mediatype

import (
	_ "embed"
	"strings"
)

//go:embed browser.js
var jsonResponseCheck string

// WithJSONResponseCheck replaces the browser script's check marker with the shared predicate.
func WithJSONResponseCheck(script string) string {
	return strings.Replace(script, "/* gofastr:json-response-check */", jsonResponseCheck, 1)
}
