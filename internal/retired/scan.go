// Package retired reports retired class names and data-fui-* attributes
// in rendered HTML. `gofastr upgrade` finds these names in an app's
// source: constant strings and CSS. A name built at run time —
// fmt.Sprintf("ui-%s", kind), a class read from the database, a template
// — is invisible to that scan but plain to see in the rendered page, so
// in a Go test binary and under `gofastr dev` the framework scans each
// finite HTML response it serves and reports what it finds. Production
// never scans and never loads the registry.
//
// The retired names come from internal/upgrade's migration registry: the
// union of every breaking note's strings.classes, css.classes and
// strings.attrs. That package has no dependency on go/packages, and
// neither does this one — the serving path must never link a type
// checker (pinned by TestServingPackagesSkipXTools).
package retired

import "bytes"

// Scan walks html's start tags and calls visit once per tag with the tag
// name, its attribute names (in document order), and the
// whitespace-split tokens of every class attribute. All arguments are
// sub-slices of html and are only valid until visit returns. Comments,
// doctypes, end tags, and the raw-text bodies of script, style,
// textarea and title are skipped; markup-looking bytes inside them are
// not markup. Malformed and truncated input yields whatever parsed; it
// never panics and never loops.
func Scan(html []byte, visit func(tag []byte, attrs [][]byte, classes [][]byte)) {
	var attrs, classes [][]byte
	i := 0
	for i < len(html) {
		lt := bytes.IndexByte(html[i:], '<')
		if lt < 0 {
			return
		}
		i += lt
		if i+1 >= len(html) {
			return
		}
		rest := html[i+1:]
		switch c := rest[0]; {
		case c == '!' || c == '?':
			// Comment, doctype, CDATA, or processing instruction: none
			// of it is markup. Find the closing '>' (a comment's may be
			// far away; an unterminated one simply ends the document).
			if bytes.HasPrefix(rest, []byte("!--")) {
				end := bytes.Index(rest[3:], []byte("-->"))
				if end < 0 {
					return
				}
				i += 1 + 3 + end + 3
				continue
			}
			gt := bytes.IndexByte(rest, '>')
			if gt < 0 {
				return
			}
			i += 1 + gt + 1
		case c == '/':
			// End tag: its own attributes (a parse error anyway) are
			// not part of any start tag.
			gt := bytes.IndexByte(rest, '>')
			if gt < 0 {
				return
			}
			i += 1 + gt + 1
		case isAlpha(c):
			nameEnd := tagNameEnd(rest)
			name := rest[:nameEnd]
			attrs, classes = attrs[:0], classes[:0]
			pos := scanAttrs(rest[nameEnd:], &attrs, &classes)
			visit(name, attrs, classes)
			// script/style/textarea/title have raw-text bodies: nothing
			// inside is markup. Skip to the matching close tag.
			if isRawText(name) {
				i = skipRawText(html, i+1+nameEnd+pos, name)
				continue
			}
			i += 1 + nameEnd + pos
		default:
			// '<' before anything that could start a tag is text
			// ("3 < 5"): the '<' itself is data.
			i++
		}
	}
}

// scanAttrs parses the attribute list of one start tag starting at b
// (just past the tag name) and returns the offset of the byte that ends
// the tag: the '>' or len(b) when the input ran out.
func scanAttrs(b []byte, attrs *[][]byte, classes *[][]byte) int {
	i := 0
	for i < len(b) {
		i += skipWS(b[i:])
		if i >= len(b) {
			return i
		}
		if b[i] == '>' {
			return i + 1
		}
		if b[i] == '/' {
			// Self-closing marker or a stray slash; either way the next
			// iteration re-examines what follows.
			i++
			continue
		}
		// Attribute name: a run of bytes that are not whitespace, '=',
		// '>' or '/'. A name that would start with '=' keeps it (HTML's
		// unexpected-equals-sign parse error), so the loop always
		// advances.
		start := i
		if b[i] == '=' {
			i++
		}
		for i < len(b) && !isWS(b[i]) && b[i] != '=' && b[i] != '>' && b[i] != '/' {
			i++
		}
		name := b[start:i]
		if len(name) == 0 {
			// Only reachable for input like "<div =" with nothing after:
			// consume the byte to guarantee progress.
			i++
			continue
		}
		*attrs = append(*attrs, name)
		// Optional value: whitespace, '=', whitespace, then a quoted or
		// unquoted value.
		j := i + skipWS(b[i:])
		if j >= len(b) || b[j] != '=' {
			// Boolean attribute. Its value is the empty string, which
			// contributes no class tokens.
			continue
		}
		j++
		j += skipWS(b[j:])
		if j >= len(b) {
			i = j
			continue
		}
		valueStart, valueEnd := -1, -1
		switch q := b[j]; q {
		case '"', '\'':
			vStart := j + 1
			end := bytes.IndexByte(b[vStart:], q)
			if end < 0 {
				// Unterminated quoted value runs to the end of input.
				valueStart, valueEnd = vStart, len(b)
				recordClassValue(name, b[valueStart:valueEnd], classes)
				return len(b)
			}
			valueStart, valueEnd = vStart, vStart+end
			i = valueEnd + 1
		default:
			valueStart = j
			for j < len(b) && !isWS(b[j]) && b[j] != '>' {
				j++
			}
			valueEnd = j
			i = j
		}
		recordClassValue(name, b[valueStart:valueEnd], classes)
	}
	return i
}

// recordClassValue appends the whitespace-split tokens of a class
// attribute's value. Other attributes' values carry no classes.
func recordClassValue(name, value []byte, classes *[][]byte) {
	if len(value) == 0 || !bytes.EqualFold(name, []byte("class")) {
		return
	}
	for len(value) > 0 {
		value = value[skipWS(value):]
		if len(value) == 0 {
			return
		}
		end := 0
		for end < len(value) && !isWS(value[end]) {
			end++
		}
		*classes = append(*classes, value[:end])
		value = value[end:]
	}
}

// skipRawText advances past the raw-text body of a script/style/
// textarea/title element: everything up to the case-insensitive close
// tag. Returns the offset just past the close tag's '>', or len(html)
// when the document ends first.
func skipRawText(html []byte, from int, name []byte) int {
	closeTag := make([]byte, 0, len(name)+2)
	closeTag = append(closeTag, '<', '/')
	closeTag = append(closeTag, name...)
	i := from
	for {
		idx := indexFold(html[i:], closeTag)
		if idx < 0 {
			return len(html)
		}
		after := i + idx + len(closeTag)
		if after >= len(html) || isWS(html[after]) || html[after] == '>' || html[after] == '/' {
			gt := bytes.IndexByte(html[after:], '>')
			if gt < 0 {
				return len(html)
			}
			return after + gt + 1
		}
		// A longer name (</scripting): keep looking.
		i = after
	}
}

func isAlpha(c byte) bool {
	return ('a' <= c && c <= 'z') || ('A' <= c && c <= 'Z')
}

func isWS(c byte) bool {
	return c == ' ' || c == '\t' || c == '\n' || c == '\r' || c == '\f'
}

func skipWS(b []byte) int {
	n := 0
	for n < len(b) && isWS(b[n]) {
		n++
	}
	return n
}

func tagNameEnd(b []byte) int {
	i := 0
	for i < len(b) && !isWS(b[i]) && b[i] != '>' && b[i] != '/' {
		i++
	}
	return i
}

// isRawText reports whether the element's content is raw text (script,
// style) or escapable raw text (textarea, title): the parser reads no
// markup inside until the matching close tag.
func isRawText(name []byte) bool {
	return bytes.EqualFold(name, []byte("script")) || bytes.EqualFold(name, []byte("style")) ||
		bytes.EqualFold(name, []byte("textarea")) || bytes.EqualFold(name, []byte("title"))
}

func indexFold(b, sub []byte) int {
	for i := 0; i+len(sub) <= len(b); i++ {
		if bytes.EqualFold(b[i:i+len(sub)], sub) {
			return i
		}
	}
	return -1
}
