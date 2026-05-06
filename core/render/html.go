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
func Attr(key, value string) string {
	return Escape(key) + `="` + Escape(value) + `"`
}

// Tag builds an HTML element from a tag name, optional attributes, and
// zero or more child HTML fragments.
//
//	Tag("div", nil, Text("hello"))  →  <div>hello</div>
//	Tag("a", map[string]string{"href": "/"}, Text("home"))  →  <a href="/">home</a>
func Tag(name string, attrs map[string]string, children ...HTML) HTML {
	var b strings.Builder
	b.WriteByte('<')
	b.WriteString(name)
	writeAttrs(&b, attrs)
	b.WriteByte('>')

	for _, child := range children {
		b.WriteString(string(child))
	}

	b.WriteString("</")
	b.WriteString(name)
	b.WriteByte('>')
	return HTML(b.String())
}

// VoidTag builds a self-closing HTML element (e.g. <img>, <br>, <hr>,
// <input>, <meta>, <link>). The tag is rendered without a closing tag.
func VoidTag(name string, attrs map[string]string) HTML {
	var b strings.Builder
	b.WriteByte('<')
	b.WriteString(name)
	writeAttrs(&b, attrs)
	b.WriteString(">")
	return HTML(b.String())
}

// Join concatenates zero or more HTML fragments into a single HTML value.
func Join(children ...HTML) HTML {
	var b strings.Builder
	for _, child := range children {
		b.WriteString(string(child))
	}
	return HTML(b.String())
}

// writeAttrs writes sorted attributes into the builder.
func writeAttrs(b *strings.Builder, attrs map[string]string) {
	if len(attrs) == 0 {
		return
	}
	keys := make([]string, 0, len(attrs))
	for k := range attrs {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		b.WriteByte(' ')
		b.WriteString(Attr(k, attrs[k]))
	}
}
