package registry

import (
	"maps"
	"slices"
	"sort"
	"strings"

	"github.com/DonaldMurillo/gofastr/core-ui/component"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/internal/attrscan"
)

// Render renders the component and injects data-cui-comp="<name>"
// onto its outermost tag. Panics on malformed component output with
// a message that tells the author how to fix it.
//
// The injected marker is the source of truth for both:
//
//   - the SSR host (which scans the final rendered HTML and emits
//     <link>s in <head> before first paint via registry.Scan), and
//
//   - the client runtime (which scans newly inserted DOM and calls
//     loadComponentCSS for each marker).
//
// Both paths dedup on the link's data-cui-style attribute, so a
// component never re-fetches across the SSR + hydration handoff or
// across page-to-page navigations.
func (s *Style) Render(c component.Component) render.HTML {
	if c == nil {
		panic("registry: Style(" + s.e.Name + ").Render(nil). Pass a Component, or use Style.WrapHTML(html) for function-style components")
	}
	return s.WrapHTML(component.RenderComponent(c))
}

// WrapHTML is the function-level form of Render. Use it when the
// component renders via a helper function returning render.HTML
// (most of framework/ui), so you can adopt the registry without
// restructuring into a Component type.
//
//	func PageHeader(cfg PageHeaderConfig) render.HTML {
//	    return Style.WrapHTML(/* existing render */)
//	}
func (s *Style) WrapHTML(html render.HTML) render.HTML {
	out, err := injectMarker(string(html), s.e.Name)
	if err != nil {
		panic(err)
	}
	return out
}

// markerAttrs are the two loader markers in rendered HTML:
// data-cui-comp="<name>" on a kit component root and
// data-cui-scope="<name>" on an owned style's root. scanNames captures
// the name. Only attribute positions inside an open tag count (preceded
// by whitespace or '/'), so stray mentions inside <pre>/<code>/text
// content don't trigger false-positive SSR links. A name outside
// markerNameByte's charset, or one with no closing quote, never
// matches, so it never reaches a /__gofastr/comp/<name>.css URL.
var markerAttrs = []string{"comp", "scope"}

// markerNameByte accepts [a-zA-Z0-9_:.-].
func markerNameByte(c byte) bool {
	switch {
	case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9':
		return true
	}
	return c == '_' || c == ':' || c == '.' || c == '-'
}

// Scan returns the sorted, deduped list of sheet names referenced by
// data-cui-comp and data-cui-scope attributes in html. Used by the SSR
// host to decide which <link> tags to emit in <head>.
//
// Markers inside a <template> do not count: the browser never renders
// template content, and the runtime's boot scan does not reach into it
// either. A confirm dialog or toast kept in a template loads its sheet
// when the runtime clones it, not render-blocking on every page that
// carries it. A declarative shadow root (<template shadowrootmode>)
// renders, so its markers count.
func Scan(html string) []string {
	live, _ := ScanSplit(html)
	return live
}

// ScanSplit is Scan plus the names only template content references:
// held lists, sorted and deduped, every name a marker inside an inert
// <template> carries and the rendered page does not. The SSR host
// prefetches those sheets so the clone the runtime makes later finds
// its sheet in the HTTP cache.
func ScanSplit(html string) (live, held []string) {
	var tpl strings.Builder
	live = scanNames(splitInertTemplates(html, &tpl), nil)
	if tpl.Len() > 0 {
		held = scanNames(tpl.String(), live)
	}
	return live, held
}

// scanNames returns the sorted marker names in html, minus skip.
func scanNames(html string, skip []string) []string {
	seen := map[string]struct{}{}
	for n := range attrscan.Values(html, markerAttrs, markerNameByte, true) {
		seen[n] = struct{}{}
	}
	for _, n := range skip {
		delete(seen, n)
	}
	if len(seen) == 0 {
		return nil
	}
	return slices.Sorted(maps.Keys(seen))
}

// EagerNames returns the sorted list of names of every LoadAlways
// entry. Used by the SSR host to seed <head> links on every page
// regardless of what the page actually rendered.
func EagerNames() []string {
	all := All()
	out := make([]string, 0, len(all))
	for _, e := range all {
		if e.Load == LoadAlways {
			out = append(out, e.Name)
		}
	}
	sort.Strings(out)
	return out
}

// splitInertTemplates returns html with every inert <template> element
// cut out, start tag through matching end tag (to the end of the input
// when unterminated, as the parser treats it), and writes each cut
// element to held. Templates nest; a declarative shadow root's start
// and end tags stay, with its content.
func splitInertTemplates(html string, held *strings.Builder) string {
	i := indexTemplateTag(html, 0)
	if i < 0 {
		return html
	}
	var b strings.Builder
	b.Grow(len(html))
	last := 0
	for i >= 0 {
		if html[i+1] == '/' { // a shadow root's end tag
			i = indexTemplateTag(html, i+1)
			continue
		}
		end := startTagEnd(html, i)
		if hasAttrName(html[i:end], "shadowrootmode") {
			i = indexTemplateTag(html, end)
			continue
		}
		b.WriteString(html[last:i])
		depth, j := 1, end
		for depth > 0 {
			k := indexTemplateTag(html, j)
			if k < 0 {
				j = len(html)
				break
			}
			if html[k+1] == '/' {
				depth--
			} else {
				depth++
			}
			j = startTagEnd(html, k)
		}
		held.WriteString(html[i:j])
		held.WriteByte('\n')
		last = j
		i = indexTemplateTag(html, j)
	}
	b.WriteString(html[last:])
	return b.String()
}

// indexTemplateTag finds the next <template or </template tag at or
// after from, case-insensitively (<template-x is a custom element), or
// -1. It walks markup the way the parser does, so template text the
// parser reads as something else does not count: a comment, a quoted
// attribute value, and the body of a raw-text element (script, style,
// textarea, title and the like) are skipped whole.
func indexTemplateTag(html string, from int) int {
	for i := from; i < len(html); {
		j := strings.IndexByte(html[i:], '<')
		if j < 0 {
			return -1
		}
		i += j
		if strings.HasPrefix(html[i:], "<!--") {
			end := strings.Index(html[i+4:], "-->")
			if end < 0 {
				return -1
			}
			i += 4 + end + 3
			continue
		}
		n := i + 1
		closing := n < len(html) && html[n] == '/'
		if closing {
			n++
		}
		k := n
		for k < len(html) && isTagNameByte(html[k]) {
			k++
		}
		name := html[n:k]
		switch {
		case name == "" || n == i+1 && !isASCIILetter(html[n]):
			i++ // not a tag: a bare '<', a doctype, a processing instruction
			continue
		case strings.EqualFold(name, "template"):
			return i
		}
		i = startTagEnd(html, i)
		if !closing && isRawTextElement(name) {
			// The body is text up to the matching end tag.
			end := indexEndTag(html, i, name)
			if end < 0 {
				return -1
			}
			i = end
		}
	}
	return -1
}

// rawTextElements hold text, not markup, up to their own end tag.
var rawTextElements = []string{"script", "style", "textarea", "title", "xmp", "iframe", "noembed", "noframes", "noscript"}

func isRawTextElement(name string) bool {
	for _, r := range rawTextElements {
		if strings.EqualFold(name, r) {
			return true
		}
	}
	return false
}

// indexEndTag finds the next </name at or after from, case-insensitively
// and ending the tag name, or -1.
func indexEndTag(html string, from int, name string) int {
	for i := from; i < len(html); i++ {
		j := strings.Index(html[i:], "</")
		if j < 0 {
			return -1
		}
		i += j
		n := i + 2
		if n+len(name) <= len(html) && strings.EqualFold(html[n:n+len(name)], name) &&
			(n+len(name) == len(html) || !isTagNameByte(html[n+len(name)])) {
			return i
		}
	}
	return -1
}

func isTagNameByte(c byte) bool {
	return isASCIILetter(c) || c >= '0' && c <= '9' || c == '-' || c == '_' || c == ':' || c == '.'
}

func isASCIILetter(c byte) bool { return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' }

// startTagEnd returns the index just past the '>' closing the tag at i,
// skipping quoted attribute values; len(html) when the tag never ends.
// hasAttrName reports whether the start tag tag (from its `<` to past
// its `>`) carries an attribute called name, ASCII case-insensitively.
// Only names count: `title="shadowrootmode"` is a value.
func hasAttrName(tag, name string) bool {
	j := 1
	for j < len(tag) && isTagNameByte(tag[j]) {
		j++
	}
	for j < len(tag) {
		c := tag[j]
		if c == '>' {
			return false
		}
		if c == '/' || isHTMLSpace(c) {
			j++
			continue
		}
		k := j
		for k < len(tag) && tag[k] != '=' && tag[k] != '>' && tag[k] != '/' && !isHTMLSpace(tag[k]) {
			k++
		}
		if strings.EqualFold(tag[j:k], name) {
			return true
		}
		j = k
		for j < len(tag) && isHTMLSpace(tag[j]) {
			j++
		}
		if j >= len(tag) || tag[j] != '=' {
			continue
		}
		j++
		for j < len(tag) && isHTMLSpace(tag[j]) {
			j++
		}
		if j < len(tag) && (tag[j] == '"' || tag[j] == '\'') {
			q := tag[j]
			j++
			for j < len(tag) && tag[j] != q {
				j++
			}
			j++
			continue
		}
		for j < len(tag) && tag[j] != '>' && !isHTMLSpace(tag[j]) {
			j++
		}
	}
	return false
}

func isHTMLSpace(c byte) bool {
	return c == ' ' || c == '\t' || c == '\n' || c == '\r' || c == '\f'
}

func startTagEnd(html string, i int) int {
	var q byte
	for j := i + 1; j < len(html); j++ {
		switch c := html[j]; {
		case q != 0:
			if c == q {
				q = 0
			}
		case c == '"' || c == '\'':
			q = c
		case c == '>':
			return j + 1
		}
	}
	return len(html)
}
