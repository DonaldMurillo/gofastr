package headless

import (
	"strings"

	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core/render"
)

// Own marks every top-level element of h data-cui-internal. It is for
// markup a component builds from its own config and hands to another
// component as slot content: framework/ui's default banner glyph, a
// form's submit row, a field's control. The receiving component cannot
// tell that markup from a caller's, so the composer says it here, and
// a slot whose content is wholly Own'd counts as the component's own
// (see ownedSlot): the element holding it is marked too.
//
// Top-level text cannot carry an attribute and is left as it is, which
// also leaves the slot reachable: a composer that wants its text
// internal wraps it in an element first. Elements already marked are
// left alone.
func Own(h render.HTML) render.HTML {
	s := string(h)
	tags, _ := scanTop(s)
	var b strings.Builder
	last := 0
	for _, t := range tags {
		if t.marked {
			continue
		}
		b.WriteString(s[last:t.end])
		b.WriteString(` data-cui-internal=""`)
		last = t.end
	}
	if last == 0 {
		return h
	}
	b.WriteString(s[last:])
	return render.HTML(b.String())
}

// ownedSlot reports whether slot content is wholly the composing
// component's: at least one top-level element, every one of them
// marked data-cui-internal, and no top-level text. A component marks
// the element holding such a slot, because nothing in it is the
// caller's.
func ownedSlot(h render.HTML) bool {
	tags, text := scanTop(string(h))
	if len(tags) == 0 || text {
		return false
	}
	for _, t := range tags {
		if !t.marked {
			return false
		}
	}
	return true
}

// internalIf returns own marked when on, own unchanged otherwise.
func internalIf(on bool, own html.Attrs) html.Attrs {
	if on {
		return Internal(own)
	}
	return own
}

// topTag is one top-level start tag: end is the index of the '>' (or
// the '/' of a self-closing "/>") where an attribute may be inserted.
type topTag struct {
	end    int
	marked bool
}

// scanTop scans a rendered fragment and returns its top-level start
// tags and whether any non-space text sits outside every element. It
// reads the markup this repo's renderer writes: quoted attribute
// values, void and self-closed elements, comments, and the raw-text
// elements whose bodies are not markup.
func scanTop(s string) (tags []topTag, text bool) {
	depth := 0
	for i := 0; i < len(s); {
		switch {
		case s[i] != '<' || i+1 >= len(s):
			if depth == 0 && !isSpaceByte(s[i]) {
				text = true
			}
			i++
		case strings.HasPrefix(s[i:], "<!--"):
			end := strings.Index(s[i+4:], "-->")
			if end < 0 {
				return tags, text
			}
			i += 4 + end + 3
		case s[i+1] == '/':
			end := strings.IndexByte(s[i:], '>')
			if end < 0 {
				return tags, text
			}
			depth--
			i += end + 1
		case isTagStart(s[i+1]):
			name, attrs, end, selfClose := scanStartTag(s, i)
			if end < 0 {
				return tags, text
			}
			if depth == 0 {
				at := end
				if selfClose {
					at = end - 1
				}
				tags = append(tags, topTag{end: at, marked: attrs["data-cui-internal"]})
			}
			i = end + 1
			if selfClose || isVoid(name) {
				continue
			}
			if rawText(name) {
				// Skip the body; the end tag's own case closes it.
				close := strings.Index(strings.ToLower(s[i:]), "</"+name)
				if close < 0 {
					return tags, text
				}
				i += close
			}
			depth++
		default:
			if depth == 0 {
				text = true
			}
			i++
		}
	}
	return tags, text
}

// scanStartTag reads the start tag at s[i] ('<' then a letter). It
// returns the lowercased name, the attribute names present, the index
// of the closing '>' (-1 when unterminated), and whether it ends "/>".
func scanStartTag(s string, i int) (name string, attrs map[string]bool, end int, selfClose bool) {
	j := i + 1
	for j < len(s) && !isSpaceByte(s[j]) && s[j] != '>' && s[j] != '/' {
		j++
	}
	name = strings.ToLower(s[i+1 : j])
	attrs = map[string]bool{}
	for j < len(s) {
		for j < len(s) && isSpaceByte(s[j]) {
			j++
		}
		if j >= len(s) {
			return name, attrs, -1, false
		}
		switch s[j] {
		case '>':
			return name, attrs, j, false
		case '/':
			if j+1 < len(s) && s[j+1] == '>' {
				return name, attrs, j + 1, true
			}
			j++
			continue
		}
		k := j
		for k < len(s) && !isSpaceByte(s[k]) && s[k] != '=' && s[k] != '>' && s[k] != '/' {
			k++
		}
		attrs[strings.ToLower(s[j:k])] = true
		j = k
		if j < len(s) && s[j] == '=' {
			j++
			if j < len(s) && (s[j] == '"' || s[j] == '\'') {
				close := strings.IndexByte(s[j+1:], s[j])
				if close < 0 {
					return name, attrs, -1, false
				}
				j += close + 2
			} else {
				for j < len(s) && !isSpaceByte(s[j]) && s[j] != '>' {
					j++
				}
			}
		}
	}
	return name, attrs, -1, false
}

func isTagStart(c byte) bool { return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' }

func isSpaceByte(c byte) bool {
	return c == ' ' || c == '\t' || c == '\n' || c == '\r' || c == '\f'
}

func rawText(name string) bool {
	switch name {
	case "script", "style", "textarea", "title":
		return true
	}
	return false
}
