package registry

import (
	"fmt"
	"html"
	"strings"

	"github.com/DonaldMurillo/gofastr/core/render"
)

// injectMarker splices ` data-cui-comp="<name>"` into the first
// opening tag of html. No HTML parser: a small state machine finds
// the end of the opening tag (the `>` that is not inside an attribute
// quote) and inserts the attribute just before it.
//
// Errors when html does not begin with an opening tag after leading
// whitespace, or when the first tag is self-closing in a way we
// cannot edit safely (we still inject before `/>`). The error
// message tells the caller how to fix their component.
func injectMarker(html, name string) (render.HTML, error) {
	if name == "" {
		return render.HTML(html), fmt.Errorf("injectMarker: empty name")
	}
	// Skip leading whitespace and HTML comments.
	i := 0
	for {
		j := skipWhitespace(html, i)
		if j+4 <= len(html) && html[j:j+4] == "<!--" {
			end := strings.Index(html[j+4:], "-->")
			if end < 0 {
				return render.HTML(html), fmt.Errorf("injectMarker: unterminated <!-- comment in component output")
			}
			i = j + 4 + end + 3
			continue
		}
		i = j
		break
	}
	if i >= len(html) || html[i] != '<' {
		return render.HTML(html), fmt.Errorf(
			"registry: component %q must render a single rooted element; "+
				"got fragment starting with %q. Wrap your output in <div> or a semantic tag.",
			name, preview(html))
	}
	// Tag name must be a letter (HTML element), not '/', '!', '?'.
	if i+1 >= len(html) {
		return render.HTML(html), fmt.Errorf("registry: component %q produced an incomplete tag", name)
	}
	c := html[i+1]
	if c == '/' || c == '!' || c == '?' {
		return render.HTML(html), fmt.Errorf(
			"registry: component %q must start with an element open tag, got %q",
			name, preview(html))
	}

	// Find the end of the opening tag: the `>` that closes it,
	// respecting attribute quotes.
	end := findOpenTagEnd(html, i+1)
	if end < 0 {
		return render.HTML(html), fmt.Errorf("registry: component %q produced an unterminated open tag", name)
	}

	// If the outermost tag already carries data-cui-comp (e.g. the
	// caller is composing a component wrapped by another Style, or
	// WrapHTML was already applied), leave the html alone. Double-
	// marker would inflate Scan output and emit a stray <link>.
	openTag := html[i:end]
	if hasAttribute(openTag, "data-cui-comp") {
		return render.HTML(html), nil
	}

	return render.HTML(spliceAttr(html, i, end, "data-cui-comp", name)), nil
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
	// Skip leading whitespace and HTML comments (as injectMarker does).
	i := 0
	for {
		j := skipWhitespace(html, i)
		if j+4 <= len(html) && html[j:j+4] == "<!--" {
			end := strings.Index(html[j+4:], "-->")
			if end < 0 {
				return src, fmt.Errorf("registry: InjectAttribute: unterminated <!-- comment in html")
			}
			i = j + 4 + end + 3
			continue
		}
		i = j
		break
	}
	if i+1 >= len(html) || html[i] != '<' || html[i+1] == '/' || html[i+1] == '!' || html[i+1] == '?' {
		return src, fmt.Errorf("registry: InjectAttribute: html must begin with an element open tag, got %q", preview(html))
	}
	end := findOpenTagEnd(html, i+1)
	if end < 0 {
		return src, fmt.Errorf("registry: InjectAttribute: unterminated open tag")
	}
	if hasAttribute(html[i:end], name) {
		return src, nil
	}
	return render.HTML(spliceAttr(html, i, end, name, htmlEscape(value))), nil
}

// spliceAttr builds the attribute text and inserts it before the
// tag's `>` (before the `/` of a self-closing tag), adding a separator
// space only when the source does not already have whitespace there.
func spliceAttr(html string, i, end int, name, value string) string {
	insertAt := end
	selfClose := end > 0 && html[end-1] == '/'
	if selfClose {
		insertAt = end - 1
	}
	sep := " "
	if insertAt > 0 && (html[insertAt-1] == ' ' || html[insertAt-1] == '\t' || html[insertAt-1] == '\n') {
		sep = ""
	}
	attr := sep + name + `="` + value + `"`
	// Preserve `<br />` spacing: a space before `/` stays a space.
	if selfClose && insertAt > 0 && html[insertAt-1] == ' ' {
		attr += " "
	}
	return html[:insertAt] + attr + html[insertAt:]
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
	i := 0
	for {
		j := skipWhitespace(html, i)
		if j+4 <= len(html) && html[j:j+4] == "<!--" {
			end := strings.Index(html[j+4:], "-->")
			if end < 0 {
				return "", false, fmt.Errorf("registry: Attribute: unterminated <!-- comment in html")
			}
			i = j + 4 + end + 3
			continue
		}
		i = j
		break
	}
	if i+1 >= len(html) || html[i] != '<' || html[i+1] == '/' || html[i+1] == '!' || html[i+1] == '?' {
		return "", false, fmt.Errorf("registry: Attribute: html must begin with an element open tag, got %q", preview(html))
	}
	end := findOpenTagEnd(html, i+1)
	if end < 0 {
		return "", false, fmt.Errorf("registry: Attribute: unterminated open tag")
	}
	openTag := html[i:end]
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
