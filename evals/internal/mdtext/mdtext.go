// Package mdtext neutralises eval-output text on its way into committed
// markdown reports.
package mdtext

import "strings"

// Escape turns markdown-significant bytes into HTML entities (renderers
// show the literal character, never structure) and flattens embedded
// newlines so text produced by the thing being evaluated cannot plant
// image beacons or add lines to the report.
var escapes = strings.NewReplacer(
	"&", "&amp;",
	"<", "&lt;",
	">", "&gt;",
	"!", "&#33;",
	"[", "&#91;",
	"]", "&#93;",
	"#", "&#35;",
	"*", "&#42;",
	"`", "&#96;",
	"\n", " ",
	"\r", " ",
)

// Escape applies escapes to one report field.
func Escape(s string) string { return escapes.Replace(s) }
