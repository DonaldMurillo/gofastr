package mediatype

import (
	_ "embed"
	"fmt"
	"strings"
)

const (
	jsonResponseCheckMarker = "/* gofastr:json-response-check */"
	htmlResponseCheckMarker = "/* gofastr:html-response-check */"
)

//go:embed browser.js
var browserChecks string

var jsonResponseCheck, htmlResponseCheck = func() (string, string) {
	json, html, ok := strings.Cut(browserChecks, "\n"+htmlResponseCheckMarker+"\n")
	if !ok {
		panic("mediatype: missing HTML response check in browser.js")
	}
	return json, strings.TrimSuffix(html, "\n")
}()

// WithJSONResponseCheck replaces the browser script's check marker with the shared predicate.
// It panics unless the script has exactly one marker.
func WithJSONResponseCheck(script string) string {
	return withResponseCheck(script, jsonResponseCheckMarker, jsonResponseCheck)
}

// WithHTMLResponseCheck replaces the browser script's HTML check marker with the shared check.
// It panics unless the script has exactly one marker.
func WithHTMLResponseCheck(script string) string {
	return withResponseCheck(script, htmlResponseCheckMarker, htmlResponseCheck)
}

func withResponseCheck(script, marker, check string) string {
	if count := strings.Count(script, marker); count != 1 {
		panic(fmt.Sprintf("mediatype: expected exactly one %s marker, found %d", marker, count))
	}
	return strings.Replace(script, marker, check, 1)
}
