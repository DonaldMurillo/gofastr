package registry

import (
	"regexp"
	"sort"

	"github.com/gofastr/gofastr/core-ui/component"
	"github.com/gofastr/gofastr/core/render"
)

// Render renders the component and injects data-fui-comp="<name>"
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
// Both paths dedup on the link's data-fui-style attribute, so a
// component never re-fetches across the SSR + hydration handoff or
// across page-to-page navigations.
func (s *Style) Render(c component.Component) render.HTML {
	if c == nil {
		panic("registry: Style(" + s.e.Name + ").Render(nil) — pass a Component, or use Style.WrapHTML(html) for function-style components")
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

// markerRe matches data-fui-comp="<name>" in rendered HTML. The name
// is captured. The leading boundary class restricts matches to
// attribute positions inside an open tag (preceded by whitespace),
// so stray mentions inside <pre>/<code>/text content don't trigger
// false-positive SSR links.
var markerRe = regexp.MustCompile(`[\s/]data-fui-comp="([a-zA-Z0-9_:.-]+)"`)

// Scan returns the sorted, deduped list of component names referenced
// by data-fui-comp attributes in html. Used by the SSR host to decide
// which <link> tags to emit in <head>.
func Scan(html string) []string {
	matches := markerRe.FindAllStringSubmatch(html, -1)
	if len(matches) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(matches))
	for _, m := range matches {
		seen[m[1]] = struct{}{}
	}
	out := make([]string, 0, len(seen))
	for n := range seen {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
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
