package registry

import (
	"fmt"
	"html"
	"maps"
	"slices"
	"strings"

	"github.com/DonaldMurillo/gofastr/core/render"
)

// injectMarker adds the component marker unless the root already has one.
func injectMarker(html, name string) (render.HTML, error) {
	if name == "" {
		return render.HTML(html), fmt.Errorf("injectMarker: empty name")
	}
	start, end, err := rootTag(html)
	if err != nil {
		return render.HTML(html), fmt.Errorf("registry: component %q must render a single rooted element: %w", name, err)
	}
	if hasAttribute(html[start:end], "data-cui-comp") {
		return render.HTML(html), nil
	}
	return render.HTML(spliceAttr(html, end, "data-cui-comp", name)), nil
}

// attrNameOK validates an attribute name: a letter or underscore (or
// the xml: style prefixes) then name characters. Anything else could
// splice structurally into the tag.
func attrNameOK(name string) bool {
	if name == "" {
		return false
	}
	for i := 0; i < len(name); i++ {
		c := name[i]
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c == '_', c == ':':
		case i > 0 && (c >= '0' && c <= '9' || c == '-' || c == '_' || c == '.' || c == ':'):
		default:
			return false
		}
	}
	return true
}

// InjectAttribute splices ` name="value"` into the first opening tag
// of src. It shares the marker injection's tag scanning: no HTML
// parser, a quote-aware scan to the tag's `>`, splicing before a
// self-closing `/` while keeping `<br />` spacing. value is
// HTML-escaped, so quotes and angle characters cannot break out of the
// attribute.
//
// Idempotent: when the first tag already carries the attribute, src is
// returned unchanged — the same posture injectMarker takes for
// data-cui-comp, so a caller composing wrappers keeps the first stamp.
//
// Errors when src does not begin with an element open tag (after
// leading whitespace and comments) or the tag is unterminated.
func InjectAttribute(src render.HTML, name, value string) (render.HTML, error) {
	if !attrNameOK(name) {
		return src, fmt.Errorf("registry: InjectAttribute: %q is not a valid attribute name", name)
	}
	html := string(src)
	start, end, err := rootTag(html)
	if err != nil {
		return src, fmt.Errorf("registry: InjectAttribute: %w", err)
	}
	if hasAttribute(html[start:end], name) {
		return src, nil
	}
	return render.HTML(spliceAttr(html, end, name, value)), nil
}

// AppendAttributes appends sorted render.Attr values to the root tag, keeping existing attributes.
// A fragment with no usable root or no valid new attributes is returned unchanged.
func AppendAttributes(src render.HTML, attrs map[string]string) render.HTML {
	if len(attrs) == 0 {
		return src
	}
	html := string(src)
	_, end, err := rootTag(html)
	if err != nil {
		return src
	}
	var out strings.Builder
	for _, name := range slices.Sorted(maps.Keys(attrs)) {
		if attr := render.Attr(name, attrs[name]); attr != "" {
			if out.Len() > 0 {
				out.WriteByte(' ')
			}
			out.WriteString(attr)
		}
	}
	if out.Len() == 0 {
		return src
	}
	return render.HTML(spliceAttrs(html, end, out.String()))
}

func rootTag(html string) (start, end int, err error) {
	start = skipWhitespace(html, 0)
	for strings.HasPrefix(html[start:], "<!--") {
		close := strings.Index(html[start+4:], "-->")
		if close < 0 {
			return 0, 0, fmt.Errorf("unterminated <!-- comment in html")
		}
		start = skipWhitespace(html, start+4+close+3)
	}
	if start+1 >= len(html) || html[start] != '<' || html[start+1] == '/' || html[start+1] == '!' || html[start+1] == '?' {
		return 0, 0, fmt.Errorf("html must begin with an element open tag, got %q", preview(html))
	}
	end = findOpenTagEnd(html, start+1)
	if end < 0 {
		return 0, 0, fmt.Errorf("unterminated open tag")
	}
	return start, end, nil
}

func spliceAttrs(html string, end int, attrs string) string {
	insertAt := end
	selfClose := html[end-1] == '/'
	if selfClose {
		insertAt--
	}
	sep := " "
	if insertAt > 0 && strings.ContainsRune(" \t\n", rune(html[insertAt-1])) {
		sep = ""
	}
	if selfClose && insertAt > 0 && html[insertAt-1] == ' ' {
		attrs += " "
	}
	return html[:insertAt] + sep + attrs + html[insertAt:]
}

func spliceAttr(html string, end int, name, value string) string {
	return spliceAttrs(html, end, name+`="`+htmlEscape(value)+`"`)
}

// hasAttribute reports whether the given opening-tag slice already
// contains the named attribute. Quote-aware: matches inside quoted
// attribute values (e.g. `class="x data-cui-comp x"`) don't count.
// Boundary before the attr name must be whitespace; boundary after
// must be `=`, whitespace, `/`, or `>` so we don't match prefix
// collisions like `data-cui-comp-extra`.
func hasAttribute(openTag, name string) bool {
	var quote byte
	for at := 0; at+len(name) <= len(openTag); at++ {
		c := openTag[at]
		if quote != 0 {
			if c == quote {
				quote = 0
			}
			continue
		}
		if c == '"' || c == '\'' {
			quote = c
			continue
		}
		// Only treat positions outside a quoted value as candidates.
		if at == 0 {
			continue // index 0 is the tag-name char, not an attribute
		}
		if openTag[at:at+len(name)] != name {
			continue
		}
		prev := openTag[at-1]
		if prev != ' ' && prev != '\t' && prev != '\n' && prev != '\r' {
			continue
		}
		var next byte
		if at+len(name) < len(openTag) {
			next = openTag[at+len(name)]
		}
		if next == '=' || next == ' ' || next == '\t' || next == '\n' || next == '\r' || next == '/' || next == '>' || next == 0 {
			return true
		}
	}
	return false
}

func skipWhitespace(s string, i int) int {
	for i < len(s) {
		c := s[i]
		if c == ' ' || c == '\t' || c == '\n' || c == '\r' {
			i++
			continue
		}
		break
	}
	return i
}

// findOpenTagEnd returns the index of the `>` that closes the open
// tag beginning at start-1, or -1 if not found. Skips over single-
// and double-quoted attribute values.
func findOpenTagEnd(s string, start int) int {
	var quote byte
	for i := start; i < len(s); i++ {
		c := s[i]
		if quote != 0 {
			if c == quote {
				quote = 0
			}
			continue
		}
		switch c {
		case '"', '\'':
			quote = c
		case '>':
			return i
		}
	}
	return -1
}

func preview(s string) string {
	if len(s) > 32 {
		return s[:32] + "…"
	}
	return s
}

// htmlEscape escapes an attribute value: quotes, angle brackets and
// ampersands cannot then terminate the attribute or the tag.
func htmlEscape(v string) string { return html.EscapeString(v) }

// Attribute reads an attribute's value from the first opening tag of
// src. ok is false when the tag carries no such attribute. It shares
// InjectAttribute's scanning and the same malformed-HTML errors, so a
// caller can read-then-decide-then-stamp without parsing twice
// differently.
func Attribute(src render.HTML, name string) (string, bool, error) {
	if !attrNameOK(name) {
		return "", false, fmt.Errorf("registry: Attribute: %q is not a valid attribute name", name)
	}
	html := string(src)
	start, end, err := rootTag(html)
	if err != nil {
		return "", false, fmt.Errorf("registry: Attribute: %w", err)
	}
	openTag := html[start:end]
	// Walk the tag outside quoted values looking for `name = value`.
	var quote byte
	for at := 1; at < len(openTag); at++ {
		c := openTag[at]
		if quote != 0 {
			if c == quote {
				quote = 0
			}
			continue
		}
		if c == '"' || c == '\'' {
			quote = c
			continue
		}
		if !attrAt(openTag, at, name) {
			continue
		}
		rest := openTag[at+len(name):]
		if !strings.HasPrefix(rest, "=") {
			return "", true, nil // present, valueless
		}
		v := strings.TrimPrefix(rest, "=")
		// A quoted value runs to ITS closing quote, not to the last
		// quote on the line ("a" class="x" must not bleed).
		if len(v) > 1 && (v[0] == '"' || v[0] == '\'') {
			if closing := strings.IndexByte(v[1:], v[0]); closing >= 0 {
				return v[1 : 1+closing], true, nil
			}
			return "", false, fmt.Errorf("registry: Attribute: unterminated attribute value")
		}
		// Unquoted value ends at whitespace or the tag end.
		if cut := strings.IndexAny(v, " \t\n\r"); cut >= 0 {
			v = v[:cut]
		}
		return v, true, nil
	}
	return "", false, nil
}

// attrAt reports whether openTag[at:] starts the attribute name at a
// name boundary on BOTH sides: whitespace before, and '=', whitespace,
// '/' or the tag's end after (so data-cui-scopey is not
// data-cui-scope).
func attrAt(openTag string, at int, name string) bool {
	rest := openTag[at:]
	if !strings.HasPrefix(rest, name) {
		return false
	}
	prev := openTag[at-1]
	if prev != ' ' && prev != '\t' && prev != '\n' && prev != '\r' {
		return false
	}
	if len(rest) == len(name) {
		return true // runs to the tag slice's end, i.e. just before '>'
	}
	next := rest[len(name)]
	return next == '=' || next == ' ' || next == '\t' || next == '\n' || next == '\r' || next == '/'
}
