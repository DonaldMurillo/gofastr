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
var browserCheckSource string

var (
	browserChecks     = parseBrowserChecks(browserCheckSource)
	jsonResponseCheck = browserCheck(jsonResponseCheckMarker)
	htmlResponseCheck = strings.TrimSuffix(browserCheck(htmlResponseCheckMarker), "\n")
)

func parseBrowserChecks(source string) map[string]string {
	checks := make(map[string]string)
	var marker string
	for line := range strings.Lines(source) {
		key := strings.TrimRight(line, "\r\n")
		if strings.HasPrefix(key, "/* gofastr:") && strings.HasSuffix(key, "-response-check */") {
			if _, exists := checks[key]; exists {
				panic("mediatype: duplicate " + key + " in browser.js")
			}
			marker = key
			checks[marker] = ""
		} else {
			if marker == "" {
				panic("mediatype: expected a response check marker at the start of browser.js")
			}
			checks[marker] += line
		}
	}
	return checks
}

func browserCheck(marker string) string {
	check := browserChecks[marker]
	if check == "" {
		panic("mediatype: missing check for " + marker + " in browser.js")
	}
	return check
}

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
