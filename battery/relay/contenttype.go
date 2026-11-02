package relay

import (
	"mime"
	"strings"
)

// The relay answers on the app's own origin, so a relayed response with
// a type the browser renders as a document runs as the app. These are
// the types an analytics, error-tracking or asset vendor serves; a
// route opts more in with Route.ContentTypes.
var defaultContentTypes = map[string]bool{
	"text/javascript":               true,
	"application/javascript":        true,
	"application/x-javascript":      true,
	"application/ecmascript":        true,
	"text/ecmascript":               true,
	"text/css":                      true,
	"application/json":              true,
	"text/plain":                    true,
	"application/font-woff":         true,
	"application/font-woff2":        true,
	"application/x-font-woff":       true,
	"application/x-font-ttf":        true,
	"application/x-font-otf":        true,
	"application/vnd.ms-fontobject": true,
}

// svgType is an image type that is also a document able to run script.
// It passes, served under Content-Security-Policy: sandbox.
const svgType = "image/svg+xml"

// relayableType reports whether media type mt (lowercased, no
// parameters) may be relayed on rt.
func relayableType(rt *routeRuntime, mt string) bool {
	if rt.extraCT[mt] || defaultContentTypes[mt] {
		return true
	}
	major, minor, _ := strings.Cut(mt, "/")
	switch major {
	case "image", "font":
		return minor != ""
	case "application":
		return strings.HasSuffix(minor, "+json")
	}
	return false
}

// validMediaType reports whether s is a lowercase type/subtype with no
// parameters and no wildcard, the spelling Route.ContentTypes takes.
func validMediaType(s string) bool {
	if s == "" || s != strings.ToLower(s) || strings.Contains(s, "*") {
		return false
	}
	mt, params, err := mime.ParseMediaType(s)
	if err != nil || len(params) != 0 || mt != s {
		return false
	}
	major, minor, ok := strings.Cut(mt, "/")
	return ok && major != "" && minor != ""
}
