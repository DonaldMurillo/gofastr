package relay

import (
	"mime"
	"strings"
)

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

const svgType = "image/svg+xml"

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
