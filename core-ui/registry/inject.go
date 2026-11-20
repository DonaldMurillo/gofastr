package registry

import (
	"fmt"
	"html"
	"strings"

	"github.com/DonaldMurillo/gofastr/core/render"
)

type tagFault int

const (
	tagOK tagFault = iota
	tagOpenComment
	tagNotElement
	tagIncomplete
	tagNotOpen
	tagUnterminated
)

func firstOpenTag(html string) (i, end int, fault tagFault) {
	for {
		j := skipWhitespace(html, i)
		if j+4 <= len(html) && html[j:j+4] == "<!--" {
			e := strings.Index(html[j+4:], "-->")
			if e < 0 {
				return 0, 0, tagOpenComment
			}
			i = j + 4 + e + 3
			continue
		}
		i = j
		break
	}
	if i >= len(html) || html[i] != '<' {
		return 0, 0, tagNotElement
	}
	if i+1 >= len(html) {
		return 0, 0, tagIncomplete
	}
	if c := html[i+1]; c == '/' || c == '!' || c == '?' {
		return 0, 0, tagNotOpen
	}
	end = findOpenTagEnd(html, i+1)
	if end < 0 {
		return 0, 0, tagUnterminated
	}
	return i, end, tagOK
}

func injectMarker(html, name string) (render.HTML, error) {
	if name == "" {
		return render.HTML(html), fmt.Errorf("injectMarker: empty name")
	}
	i, end, fault := firstOpenTag(html)
	switch fault {
	case tagOpenComment:
		return render.HTML(html), fmt.Errorf("injectMarker: unterminated <!-- comment in component output")
	case tagNotElement:
		return render.HTML(html), fmt.Errorf(
			"registry: component %q must render a single rooted element; "+
				"got fragment starting with %q. Wrap your output in <div> or a semantic tag.",
			name, preview(html))
	case tagIncomplete:
		return render.HTML(html), fmt.Errorf("registry: component %q produced an incomplete tag", name)
	case tagNotOpen:
		return render.HTML(html), fmt.Errorf(
			"registry: component %q must start with an element open tag, got %q",
			name, preview(html))
	case tagUnterminated:
		return render.HTML(html), fmt.Errorf("registry: component %q produced an unterminated open tag", name)
	}
	if attrIndex(html[i:end], "data-cui-comp") >= 0 {
		return render.HTML(html), nil
	}
	return render.HTML(spliceAttr(html, i, end, "data-cui-comp", name)), nil
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
	i, end, err := openTagFor(html, "InjectAttribute")
	if err != nil {
		return src, err
	}
	if attrIndex(html[i:end], name) >= 0 {
		return src, nil
	}
	return render.HTML(spliceAttr(html, i, end, name, htmlEscape(value))), nil
}

func openTagFor(html, who string) (i, end int, err error) {
	i, end, fault := firstOpenTag(html)
	switch fault {
	case tagOpenComment:
		return 0, 0, fmt.Errorf("registry: %s: unterminated <!-- comment in html", who)
	case tagNotElement, tagIncomplete, tagNotOpen:
		return 0, 0, fmt.Errorf("registry: %s: html must begin with an element open tag, got %q", who, preview(html))
	case tagUnterminated:
		return 0, 0, fmt.Errorf("registry: %s: unterminated open tag", who)
	}
	return i, end, nil
}

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
	if selfClose && insertAt > 0 && html[insertAt-1] == ' ' {
		attr += " "
	}
	return html[:insertAt] + attr + html[insertAt:]
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
	i, end, err := openTagFor(html, "Attribute")
	if err != nil {
		return "", false, err
	}
	openTag := html[i:end]
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
