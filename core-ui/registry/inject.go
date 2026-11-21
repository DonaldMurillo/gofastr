package registry

import (
	"fmt"
	"html"
	"maps"
	"slices"
	"strings"

	"github.com/DonaldMurillo/gofastr/core/render"
)

func injectMarker(html, name string) (render.HTML, error) {
	if name == "" {
		return render.HTML(html), fmt.Errorf("injectMarker: empty name")
	}
	start, end, err := rootTag(html)
	if err != nil {
		return render.HTML(html), fmt.Errorf("registry: component %q must render a single rooted element: %w", name, err)
	}
	if attrIndex(html[start:end], "data-cui-comp") >= 0 {
		return render.HTML(html), nil
	}
	return render.HTML(spliceAttr(html, end, "data-cui-comp", name)), nil
}

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

// InjectAttribute splices name="value" into the first opening tag of src.
func InjectAttribute(src render.HTML, name, value string) (render.HTML, error) {
	if !attrNameOK(name) {
		return src, fmt.Errorf("registry: InjectAttribute: %q is not a valid attribute name", name)
	}
	html := string(src)
	start, end, err := rootTag(html)
	if err != nil {
		return src, fmt.Errorf("registry: InjectAttribute: %w", err)
	}
	if attrIndex(html[start:end], name) >= 0 {
		return src, nil
	}
	return render.HTML(spliceAttr(html, end, name, value)), nil
}

// AppendAttributes appends sorted render.Attr values to the root tag, keeping existing attributes.
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

func attrIndex(openTag, name string) int {
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
		if attrAt(openTag, at, name) {
			return at
		}
	}
	return -1
}

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
		return true
	}
	next := rest[len(name)]
	return next == '=' || next == ' ' || next == '\t' || next == '\n' || next == '\r' || next == '/'
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

func htmlEscape(v string) string { return html.EscapeString(v) }

// Attribute reads name's value from the first opening tag of src.
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
	at := attrIndex(openTag, name)
	if at < 0 {
		return "", false, nil
	}
	rest := openTag[at+len(name):]
	if !strings.HasPrefix(rest, "=") {
		return "", true, nil
	}
	v := strings.TrimPrefix(rest, "=")
	if len(v) > 1 && (v[0] == '"' || v[0] == '\'') {
		if closing := strings.IndexByte(v[1:], v[0]); closing >= 0 {
			return v[1 : 1+closing], true, nil
		}
		return "", false, fmt.Errorf("registry: Attribute: unterminated attribute value")
	}
	if cut := strings.IndexAny(v, " \t\n\r"); cut >= 0 {
		v = v[:cut]
	}
	return v, true, nil
}
