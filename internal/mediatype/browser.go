package mediatype

import (
	_ "embed"
	"strings"
)

//go:embed browser.js
var jsonResponseCheck string

// WithJSONResponseCheck replaces the browser script's check marker with the shared predicate.
// It panics unless the script has exactly one marker.
func WithJSONResponseCheck(script string) string {
	const marker = "/* gofastr:json-response-check */"
	if strings.Count(script, marker) != 1 {
		panic("mediatype.WithJSONResponseCheck: expected exactly one " + marker + " marker")
	}
	return strings.Replace(script, marker, jsonResponseCheck, 1)
}
