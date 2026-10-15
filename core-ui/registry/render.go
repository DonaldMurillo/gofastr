package registry

import (
	"maps"
	"regexp"
	"slices"
	"sort"
	"strings"

	"github.com/DonaldMurillo/gofastr/core-ui/component"
	"github.com/DonaldMurillo/gofastr/core/render"
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

// markerRe matches the two loader markers in rendered HTML:
// data-cui-comp="<name>" on a kit component root and
// data-cui-scope="<name>" on an owned style's root. The name is
// captured. The leading boundary class restricts matches to attribute
// positions inside an open tag (preceded by whitespace), so stray
// mentions inside <pre>/<code>/text content don't trigger
// false-positive SSR links. A name outside the charset never matches,
// so it never reaches a /__gofastr/comp/<name>.css URL.
var markerRe = regexp.MustCompile(`[\s/]data-cui-(?:comp|scope)="([a-zA-Z0-9_:.-]+)"`)

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
	matches := markerRe.FindAllStringSubmatch(html, -1)
	if len(matches) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(matches))
	for _, m := range matches {
		seen[m[1]] = struct{}{}
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
		if strings.Contains(strings.ToLower(html[i:end]), "shadowrootmode") {
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
// after from, case-insensitively, where the name ends the tag name
// (<template-x is a custom element). -1 when there is none.
func indexTemplateTag(html string, from int) int {
	for i := from; i < len(html); i++ {
		j := strings.IndexByte(html[i:], '<')
		if j < 0 {
			return -1
		}
		i += j
		n := i + 1
		if n < len(html) && html[n] == '/' {
			n++
		}
		if n+8 <= len(html) && strings.EqualFold(html[n:n+8], "template") &&
			(n+8 == len(html) || strings.IndexByte(" \t\n\r\f/>", html[n+8]) >= 0) {
			return i
		}
	}
	return -1
}

// startTagEnd returns the index just past the '>' closing the tag at i,
// skipping quoted attribute values; len(html) when the tag never ends.
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
