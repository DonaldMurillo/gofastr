package registry

import (
	"fmt"
	"strings"

	"github.com/gofastr/gofastr/core/render"
)

// injectMarker splices ` data-fui-comp="<name>"` into the first
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

	// If the outermost tag already carries data-fui-comp (e.g. the
	// caller is composing a component wrapped by another Style, or
	// WrapHTML was already applied), leave the html alone. Double-
	// marker would inflate Scan output and emit a stray <link>.
	openTag := html[i:end]
	if hasAttribute(openTag, "data-fui-comp") {
		return render.HTML(html), nil
	}

	// If the tag is self-closing (`/>`), splice before the `/`.
	insertAt := end
	selfClose := end > 0 && html[end-1] == '/'
	if selfClose {
		insertAt = end - 1
	}

	// If there is no space before insertAt, add one.
	sep := " "
	if insertAt > 0 && (html[insertAt-1] == ' ' || html[insertAt-1] == '\t' || html[insertAt-1] == '\n') {
		sep = ""
	}

	attr := sep + `data-fui-comp="` + name + `"`
	// Preserve a trailing space before `/>` if the source had one
	// — `<br />` should stay `<br data-fui-comp="…" />`, not collapse
	// to `<br data-fui-comp="…"/>`. We detect this by checking the
	// character we're about to splice before.
	if selfClose && insertAt > 0 && html[insertAt-1] == ' ' {
		attr += " "
	}
	out := html[:insertAt] + attr + html[insertAt:]
	return render.HTML(out), nil
}

// hasAttribute reports whether the given opening-tag slice already
// contains the named attribute. Quote-aware: matches inside quoted
// attribute values (e.g. `class="x data-fui-comp x"`) don't count.
// Boundary before the attr name must be whitespace; boundary after
// must be `=`, whitespace, `/`, or `>` so we don't match prefix
// collisions like `data-fui-comp-extra`.
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
