package render

import (
	"sort"
	"strings"
)

// HTML is a markup fragment trusted as safe; build it with [Tag], [Text], [Raw], [VoidTag] and [Join].
type HTML string

// String returns the underlying HTML string.
func (h HTML) String() string {
	return string(h)
}

// Escape replaces &, <, >, " and ' with their HTML entities.
func Escape(s string) string {
	s = strings.ReplaceAll(s, "&", "&amp;")
	s = strings.ReplaceAll(s, "<", "&lt;")
	s = strings.ReplaceAll(s, ">", "&gt;")
	s = strings.ReplaceAll(s, `"`, "&quot;")
	s = strings.ReplaceAll(s, "'", "&#39;")
	return s
}

// Text returns s HTML-escaped.
func Text(s string) HTML {
	return HTML(Escape(s))
}

// Raw returns s unescaped; use it only for trusted markup.
func Raw(s string) HTML {
	return HTML(s)
}

// Attr returns key="value" with value escaped, or "" when key is not ASCII letters, digits, -, _ and : or starts with on.
func Attr(key, value string) string {
	if !isSafeAttrKey(key) {
		return ""
	}
	return key + `="` + Escape(value) + `"`
}

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

// Tag builds <name attrs>children</name>, attributes sorted; an invalid name renders as span.
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

// VoidTag builds an element with no closing tag (img, br, input), validating name like [Tag].
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

// If returns html when cond is true, otherwise "". html is built either way; use [When] for an expensive branch.
func If(cond bool, html HTML) HTML {
	if cond {
		return html
	}
	return ""
}

// When returns fn() when cond is true, otherwise "", calling fn only when cond is true.
func When(cond bool, fn func() HTML) HTML {
	if cond {
		return fn()
	}
	return ""
}

// Classes joins the non-empty parts with spaces; pair it with [ClassIf].
func Classes(parts ...string) string {
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p != "" {
			out = append(out, p)
		}
	}
	return strings.Join(out, " ")
}

// ClassIf returns name when cond is true, otherwise "".
func ClassIf(cond bool, name string) string {
	if cond {
		return name
	}
	return ""
}

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
			continue
		}
		b.WriteByte(' ')
		b.WriteString(rendered)
	}
}

func estimateTagSize(name string, attrs map[string]string, children []HTML) int {
	total := 2*len(name) + 5
	total += estimateAttrsSize(attrs)
	for _, child := range children {
		total += len(child)
	}
	return total
}

func estimateVoidTagSize(name string, attrs map[string]string) int {
	return len(name) + 2 + estimateAttrsSize(attrs)
}

func estimateAttrsSize(attrs map[string]string) int {
	total := 0
	for k, v := range attrs {
		total += len(k) + len(v) + 4
	}
	return total
}
