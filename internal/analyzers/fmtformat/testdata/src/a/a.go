package a

import (
	"fmt"
	"net/url"
	"strings"
)

// helper shape: patternWith encodes carry internally and returns it
// joined with its tail argument.
func patternWith(carry url.Values, tail string) string {
	enc := carry.Encode()
	if enc == "" {
		return "?" + tail
	}
	return "?" + enc + "&" + tail
}

// helper taint reaching a sink through a local variable.
func sinkViaHelper(v url.Values, key string) string {
	pattern := patternWith(v, "sort=%s&dir=%s") // want `fmtformat: patternWith returns URL-encoded output and this call joins it with fmt verbs`
	return fmt.Sprintf(pattern, key, "asc")     // want `fmtformat: URL-encoded value in the fmt.Sprintf format string`
}

func sinkDirect(v url.Values) string {
	return fmt.Sprintf(v.Encode(), 1) // want `fmtformat: URL-encoded value in the fmt.Sprintf format string`
}

func sinkViaConcat(v url.Values) string {
	href := "?" + v.Encode() + "&p=%d"
	return fmt.Sprintf(href, 2) // want `fmtformat: URL-encoded value in the fmt.Sprintf format string`
}

func sinkQueryEscape(q string) string {
	p := "q=" + url.QueryEscape(q) + "&x=%s"
	return fmt.Sprintf(p, "y") // want `fmtformat: URL-encoded value in the fmt.Sprintf format string`
}

// builder called with verb literals, sink far away (other package).
func buildOnly(v url.Values) string {
	return patternWith(v, "p=%d") // want `fmtformat: patternWith returns URL-encoded output and this call joins it with fmt verbs`
}

type cfg struct{ Href string }

// builder-object shape (resource.go:514): encoded bytes accumulated
// via strings.Builder.WriteString, rendered with .String(),
// concatenated with verbs into a composite-literal field.
func buildField(q string) cfg {
	var carry strings.Builder
	carry.WriteString(url.QueryEscape(q))
	return cfg{Href: "?" + carry.String() + "&p=%d"} // want `fmtformat: URL-encoded value concatenated with fmt verbs into a handed-onward pattern`
}
