package registry

import (
	"fmt"
	"html"
	"maps"
	"slices"
	"strings"

	xhtml "golang.org/x/net/html"

	"github.com/DonaldMurillo/gofastr/core/render"
)

func injectMarker(html, name string) (render.HTML, error) {
	if name == "" {
		return render.HTML(html), fmt.Errorf("injectMarker: empty name")
	}
	start, _, err := rootTag(html)
	if err != nil {
		return render.HTML(html), fmt.Errorf("registry: component %q must render a single rooted element: %w", name, err)
	}
	if _, ok := findAttr(html, start, "data-cui-comp"); ok {
		return render.HTML(html), nil
	}
	return render.HTML(spliceAttr(html, start, "data-cui-comp", name)), nil
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
	start, _, err := rootTag(html)
	if err != nil {
		return src, fmt.Errorf("registry: InjectAttribute: %w", err)
	}
	if _, ok := findAttr(html, start, name); ok {
		return src, nil
	}
	return render.HTML(spliceAttr(html, start, name, value)), nil
}

// AppendAttributes appends sorted render.Attr values to the root tag, keeping existing attributes.
func AppendAttributes(src render.HTML, attrs map[string]string) render.HTML {
	if len(attrs) == 0 {
		return src
	}
	html := string(src)
	start, _, err := rootTag(html)
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
	return render.HTML(spliceAttrs(html, start, out.String()))
}

// rootTag finds the first element open tag of html, past leading
// whitespace and comments: start is its '<' and end its closing '>'.
func rootTag(html string) (start, end int, err error) {
	start = skipWhitespace(html, 0)
	for strings.HasPrefix(html[start:], "<!--") {
		after := commentEnd(html, start)
		if after < 0 {
			return 0, 0, fmt.Errorf("unterminated <!-- comment in html")
		}
		start = skipWhitespace(html, after)
	}
	if start+1 >= len(html) || html[start] != '<' || html[start+1] == '/' || html[start+1] == '!' || html[start+1] == '?' {
		return 0, 0, fmt.Errorf("html must begin with an element open tag, got %q", preview(html))
	}
	end, _ = walkTag(html, start, nil)
	if end < 0 {
		return 0, 0, fmt.Errorf("unterminated open tag")
	}
	return start, end, nil
}

// commentEnd returns the offset just past the comment that opens with
// "<!--" at html[at], or -1: as in HTML, "<!-->" and "<!--->" are whole
// comments, and "--!>" closes one as "-->" does.
func commentEnd(html string, at int) int {
	body := at + 4
	switch {
	case strings.HasPrefix(html[body:], ">"):
		return body + 1
	case strings.HasPrefix(html[body:], "->"):
		return body + 2
	}
	end := -1
	for _, closer := range []string{"-->", "--!>"} {
		if i := strings.Index(html[body:], closer); i >= 0 && (end < 0 || body+i+len(closer) < end) {
			end = body + i + len(closer)
		}
	}
	return end
}

// attrSpan is one attribute of a start tag, as offsets into the html it
// was read from.
type attrSpan struct {
	name     string
	nameEnd  int  // just past the name
	eq       bool // an = follows the name
	valStart int  // the raw value is html[valStart:valEnd], inside its quotes
	valEnd   int
	end      int  // just past the attribute, its closing quote included
	quote    byte // the value's quote; 0 when unquoted or bare
}

// walkTag reads the start tag whose '<' is html[start] as the HTML
// tokenizer does: the tag name runs to whitespace, '/' or '>'; an
// attribute name runs to whitespace, '/', '>' or '=' (a leading '=' is
// part of it); a value after '=' and optional whitespace is quoted to its
// own closing quote or unquoted to whitespace or '>'. It calls fn for
// each attribute in order (not after fn returns false) and returns the
// index of the tag's '>' (-1 when the tag never ends) and the index of a
// self-closing '/' just before it (-1 when there is none).
func walkTag(html string, start int, fn func(attrSpan) bool) (end, slash int) {
	j := start + 1
	for j < len(html) && html[j] != '>' && html[j] != '/' && !isHTMLSpace(html[j]) {
		j++
	}
	slash = -1
	for j < len(html) {
		switch c := html[j]; {
		case c == '>':
			return j, slash
		case c == '/':
			slash = j
			j++
			continue
		case isHTMLSpace(c):
			slash = -1
			j++
			continue
		}
		slash = -1
		k := j + 1
		for k < len(html) && html[k] != '=' && html[k] != '>' && html[k] != '/' && !isHTMLSpace(html[k]) {
			k++
		}
		a := attrSpan{name: html[j:k], nameEnd: k, valStart: k, valEnd: k, end: k}
		v := skipWhitespace(html, k)
		if v < len(html) && html[v] == '=' {
			a.eq = true
			v = skipWhitespace(html, v+1)
			if v < len(html) && (html[v] == '"' || html[v] == '\'') {
				closing := strings.IndexByte(html[v+1:], html[v])
				if closing < 0 {
					return -1, -1
				}
				a.quote, a.valStart, a.valEnd, a.end = html[v], v+1, v+1+closing, v+1+closing+1
			} else {
				e := v
				for e < len(html) && html[e] != '>' && !isHTMLSpace(html[e]) {
					e++
				}
				a.valStart, a.valEnd, a.end = v, e, e
			}
		}
		j = a.end
		if fn != nil && !fn(a) {
			fn = nil
		}
	}
	return -1, -1
}

// findAttr returns the first attribute named name, without regard to
// ASCII case, of the start tag at html[start].
func findAttr(html string, start int, name string) (attrSpan, bool) {
	var found attrSpan
	ok := false
	walkTag(html, start, func(a attrSpan) bool {
		if strings.EqualFold(a.name, name) {
			found, ok = a, true
			return false
		}
		return true
	})
	return found, ok
}

// spliceAttrs adds attrs, already escaped, as the last attributes of the
// start tag at html[start], before a self-closing '/' and keeping a space
// before it. A last attribute left as a bare '=' is given "" first, so
// the browser does not read attrs as its value.
func spliceAttrs(html string, start int, attrs string) string {
	var last attrSpan
	has := false
	end, slash := walkTag(html, start, func(a attrSpan) bool {
		last, has = a, true
		return true
	})
	insertAt := end
	if slash >= 0 {
		insertAt = slash
	}
	head, mid := html[:insertAt], ""
	if has && last.eq && last.quote == 0 && last.valStart == last.valEnd {
		head, mid = html[:last.valStart], `""`+html[last.valStart:insertAt]
	}
	sep := " "
	if mid == "" && insertAt > 0 && isHTMLSpace(html[insertAt-1]) || mid != "" && isHTMLSpace(mid[len(mid)-1]) {
		sep = ""
	}
	if slash >= 0 && insertAt > 0 && html[insertAt-1] == ' ' {
		attrs += " "
	}
	return head + mid + sep + attrs + html[insertAt:]
}

func spliceAttr(html string, start int, name, value string) string {
	return spliceAttrs(html, start, name+`="`+htmlEscape(value)+`"`)
}

func skipWhitespace(s string, i int) int {
	for i < len(s) && isHTMLSpace(s[i]) {
		i++
	}
	return i
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
	start, _, err := rootTag(html)
	if err != nil {
		return "", false, fmt.Errorf("registry: Attribute: %w", err)
	}
	a, ok := findAttr(html, start, name)
	if !ok {
		return "", false, nil
	}
	return html[a.valStart:a.valEnd], true, nil
}

// AddAttributeToken adds token to the space-separated list name holds on
// the first opening tag of src, adding the attribute when the tag has
// none; a list that already holds token comes back unchanged.
func AddAttributeToken(src render.HTML, name, token string) (render.HTML, error) {
	if !attrNameOK(name) {
		return src, fmt.Errorf("registry: AddAttributeToken: %q is not a valid attribute name", name)
	}
	if token == "" || strings.ContainsAny(token, " \t\n\r\f\x00") {
		return src, fmt.Errorf("registry: AddAttributeToken: %q is not a single token", token)
	}
	html := string(src)
	start, _, err := rootTag(html)
	if err != nil {
		return src, fmt.Errorf("registry: AddAttributeToken: %w", err)
	}
	a, ok := findAttr(html, start, name)
	if !ok {
		return render.HTML(spliceAttr(html, start, name, token)), nil
	}
	raw := html[a.valStart:a.valEnd]
	if slices.Contains(strings.FieldsFunc(attrDecode(raw), isHTMLSpaceRune), token) {
		return src, nil
	}
	sep := " "
	if raw == "" || isHTMLSpace(raw[len(raw)-1]) {
		sep = ""
	}
	if a.quote != 0 {
		return render.HTML(insertBeforeQuote(html, a.valEnd, sep, token)), nil
	}
	return render.HTML(requoteValue(html, a.nameEnd, a.end, raw, sep, token)), nil
}

// insertBeforeQuote adds sep and the HTML-escaped token just before a
// quoted value's closing quote at html[at], leaving the old bytes as they
// were.
func insertBeforeQuote(html string, at int, sep, token string) string {
	return html[:at] + sep + htmlEscape(token) + html[at:]
}

// requoteValue writes a bare or unquoted value back double-quoted over
// html[from:to], from the end of its name to the end of its value, then
// sep and the HTML-escaped token. An unquoted value may hold a quote; its
// " becomes &quot;, which decodes to the same character, since an & cannot
// lengthen the reference before it.
func requoteValue(html string, from, to int, raw, sep, token string) string {
	return html[:from] + `="` + strings.ReplaceAll(raw, `"`, "&quot;") + sep + htmlEscape(token) + `"` + html[to:]
}

func isHTMLSpaceRune(r rune) bool { return r < 0x80 && isHTMLSpace(byte(r)) }

// attrDecode decodes a raw attribute value as a browser does in an
// attribute, where a semicolonless reference followed by = or an
// alphanumeric stays literal, unlike html.UnescapeString's text rules.
func attrDecode(raw string) string {
	z := xhtml.NewTokenizer(strings.NewReader(`<x a="` + strings.ReplaceAll(raw, `"`, "&quot;") + `">`))
	if z.Next() != xhtml.StartTagToken {
		return raw
	}
	if _, val, _ := z.TagAttr(); val != nil {
		return string(val)
	}
	return raw
}
