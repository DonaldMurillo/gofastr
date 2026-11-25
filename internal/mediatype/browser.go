package mediatype

import _ "embed"

//go:embed browser.js
var jsonResponseCheck string

// WithJSONResponseCheck makes the shared Content-Type check available locally
// to a browser script, without requiring the page to load another asset.
func WithJSONResponseCheck(script string) string {
	return "(() => {\n" + jsonResponseCheck + script + "\n})();\n"
}
