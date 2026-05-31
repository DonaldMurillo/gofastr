// Package render provides a type-safe HTML template engine built from scratch.
// Templates are Go code producing HTML with compile-time type checking and
// auto-escaping. Inspired by Templ.
//
// All text content is HTML-escaped by default. Use [Raw] for trusted markup.
package render

import (
	"sort"
	"strings"
)

// HTML is a type-safe wrapper around an HTML string fragment.
// Values of type HTML are assumed to contain safe, well-formed markup.
// Construct HTML values using [Tag], [Text], [Raw], [VoidTag], and [Join].
type HTML string

// String returns the underlying HTML string.
func (h HTML) String() string {
	return string(h)
}

// Escape replaces the five special HTML characters with their entity
// equivalents: &, <, >, ", '. This prevents XSS when inserting untrusted
// data into HTML documents.
func Escape(s string) string {
	s = strings.ReplaceAll(s, "&", "&amp;")
	s = strings.ReplaceAll(s, "<", "&lt;")
	s = strings.ReplaceAll(s, ">", "&gt;")
	s = strings.ReplaceAll(s, `"`, "&quot;")
	s = strings.ReplaceAll(s, "'", "&#39;")
	return s
}

// Text wraps the given string as auto-escaped HTML text content.
// The input is HTML-escaped to prevent injection of raw markup.
func Text(s string) HTML {
	return HTML(Escape(s))
}

// Raw wraps the given string as HTML without any escaping.
// Only use Raw when the content is known to be safe, e.g. markup
// produced by trusted builder calls.
func Raw(s string) HTML {
	return HTML(s)
}

// Attr formats a single HTML attribute as key="value" with the value
// HTML-escaped.
//
// The key is validated against a strict allow-list (ASCII letters,
// digits, `-`, `_`, `:`). Keys containing whitespace would let a
// caller smuggle a second attribute — e.g. `src onerror` becomes a
// fresh `onerror=` attribute when concatenated into a tag. Keys
// beginning with `on` (case-insensitive) are inline event handlers
// and are never legitimately constructed via this builder — a host
// that needs one should be writing handlers in runtime.js, not
// injecting JS into the HTML. Both cases return the empty string so
// the attribute is dropped from any tag that includes it.
func Attr(key, value string) string {
	if !isSafeAttrKey(key) {
		return ""
	}
	return key + `="` + Escape(value) + `"`
}

// isSafeAttrKey reports whether key is a syntactically valid HTML
// attribute name AND is not an inline event-handler (`on*`). The
// allow-list mirrors the HTML5 attribute-name grammar restricted to
// the characters that practically appear in this framework's UI
// code (ASCII letters, digits, `-`, `_`, `:`). Anything else —
// whitespace, quotes, slashes, control bytes — is rejected because
// the only reason it would appear is an attempted breakout.
func isSafeAttrKey(key string) bool {
	if key == "" {
		return false
	}
	if len(key) >= 2 {
		a, b := key[0], key[1]
		if (a == 'o' || a == 'O') && (b == 'n' || b == 'N') {
			return false
		}
	}
	for i := 0; i < len(key); i++ {
		c := key[i]
		switch {
		case c >= 'a' && c <= 'z':
		case c >= 'A' && c <= 'Z':
		case c >= '0' && c <= '9':
		case c == '-' || c == '_' || c == ':':
		default:
			return false
		}
	}
	return true
}

// Tag builds an HTML element from a tag name, optional attributes, and
// zero or more child HTML fragments.
//
//	Tag("div", nil, Text("hello"))  →  <div>hello</div>
//	Tag("a", map[string]string{"href": "/"}, Text("home"))  →  <a href="/">home</a>
//
// The tag name is validated against a strict allow-list (ASCII letters,
// digits and `-`). A name containing whitespace or other characters
// would otherwise let a caller smuggle attributes — e.g. a name of
// `div onclick="alert(1)"` would render as `<div onclick="alert(1)">`
// — so any invalid name is replaced with a neutral `span`.
func Tag(name string, attrs map[string]string, children ...HTML) HTML {
	safeName := safeTagName(name)
	var b strings.Builder
	b.Grow(estimateTagSize(safeName, attrs, children))
	b.WriteByte('<')
	b.WriteString(safeName)
	writeAttrs(&b, attrs)
	b.WriteByte('>')

	for _, child := range children {
		b.WriteString(string(child))
	}

	b.WriteString("</")
	b.WriteString(safeName)
	b.WriteByte('>')
	return HTML(b.String())
}

// VoidTag builds a self-closing HTML element (e.g. <img>, <br>, <hr>,
// <input>, <meta>, <link>). The tag is rendered without a closing tag.
// The tag name is validated like Tag — see that doc for rationale.
func VoidTag(name string, attrs map[string]string) HTML {
	safeName := safeTagName(name)
	var b strings.Builder
	b.Grow(estimateVoidTagSize(safeName, attrs))
	b.WriteByte('<')
	b.WriteString(safeName)
	writeAttrs(&b, attrs)
	b.WriteString(">")
	return HTML(b.String())
}

// safeTagName returns name if it is a syntactically valid HTML tag name
// (first char ASCII letter, remaining chars ASCII letters/digits/-).
// Any other input — empty, whitespace-bearing, attribute-smuggling —
// is collapsed to "span", which is a neutral inline tag that can't
// execute scripts or open a layout hole.
func safeTagName(name string) string {
	if name == "" {
		return "span"
	}
	for i := 0; i < len(name); i++ {
		c := name[i]
		switch {
		case c >= 'a' && c <= 'z':
		case c >= 'A' && c <= 'Z':
		case i > 0 && c >= '0' && c <= '9':
		case i > 0 && c == '-':
		default:
			return "span"
		}
	}
	return name
}

// Join concatenates zero or more HTML fragments into a single HTML value.
func Join(children ...HTML) HTML {
	var b strings.Builder
	total := 0
	for _, child := range children {
		total += len(child)
	}
	b.Grow(total)
	for _, child := range children {
		b.WriteString(string(child))
	}
	return HTML(b.String())
}

// If returns html when cond is true, otherwise an empty fragment.
// Useful for inline conditional rendering:
//
//	Tag("div", nil,
//	    Text("hello"),
//	    If(user.Admin, adminBadge()),
//	)
//
// Both arguments evaluate eagerly — Go has no lazy semantics here. When
// the truthy branch is expensive (database query, heavy allocation),
// use [When] instead so the function only runs when cond is true.
func If(cond bool, html HTML) HTML {
	if cond {
		return html
	}
	return ""
}

// When returns fn() when cond is true, otherwise an empty fragment.
// Lazy variant of [If] that avoids constructing html when cond is false —
// preferred when the truthy branch is expensive.
func When(cond bool, fn func() HTML) HTML {
	if cond {
		return fn()
	}
	return ""
}

// Classes joins non-empty string args with spaces. Pair with [ClassIf]
// for conditional class lists:
//
//	class := render.Classes(
//	    "p-cond-row",
//	    render.ClassIf(active, "active"),
//	    render.ClassIf(hasError, "error"),
//	)
//
// The returned string is plain text and is HTML-escaped automatically
// when assigned to a tag attribute via [Tag] or any html.* config.
//
// For wide predicate-driven sets, see html.Classes(map[string]bool).
func Classes(parts ...string) string {
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p != "" {
			out = append(out, p)
		}
	}
	return strings.Join(out, " ")
}

// ClassIf returns name when cond is true, otherwise the empty string.
// Argument order matches Go's `if cond { name }` reading order.
// Designed for use inside [Classes]:
//
//	render.Classes("base", render.ClassIf(isActive, "active"))
func ClassIf(cond bool, name string) string {
	if cond {
		return name
	}
	return ""
}

// writeAttrs writes sorted attributes into the builder.
func writeAttrs(b *strings.Builder, attrs map[string]string) {
	if len(attrs) == 0 {
		return
	}
	if len(attrs) == 1 {
		for k, v := range attrs {
			rendered := Attr(k, v)
			if rendered == "" {
				return
			}
			b.WriteByte(' ')
			b.WriteString(rendered)
		}
		return
	}
	keys := make([]string, 0, len(attrs))
	for k := range attrs {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		rendered := Attr(k, attrs[k])
		if rendered == "" {
			// Attr drops unsafe keys (whitespace, event handlers,
			// non-allow-list chars). Skip the leading space too so we
			// don't leave a dangling separator inside the tag.
			continue
		}
		b.WriteByte(' ')
		b.WriteString(rendered)
	}
}

func estimateTagSize(name string, attrs map[string]string, children []HTML) int {
	total := 2*len(name) + 5 // <name></name>
	total += estimateAttrsSize(attrs)
	for _, child := range children {
		total += len(child)
	}
	return total
}

func estimateVoidTagSize(name string, attrs map[string]string) int {
	return len(name) + 2 + estimateAttrsSize(attrs) // <name>
}

func estimateAttrsSize(attrs map[string]string) int {
	total := 0
	for k, v := range attrs {
		total += len(k) + len(v) + 4 // space, =, quotes
	}
	return total
}
