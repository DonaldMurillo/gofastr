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

// Render renders c with data-cui-comp="<name>" on its outermost tag.
func (s *Style) Render(c component.Component) render.HTML {
	if c == nil {
		panic("registry: Style(" + s.e.Name + ").Render(nil). Pass a Component, or use Style.WrapHTML(html) for function-style components")
	}
	return s.WrapHTML(component.RenderComponent(c))
}

// WrapHTML is Render for a component that returns render.HTML.
func (s *Style) WrapHTML(html render.HTML) render.HTML {
	out, err := injectMarker(string(html), s.e.Name)
	if err != nil {
		panic(err)
	}
	return out
}

var markerAttrs = []string{"comp", "scope"}

// Scan returns the sorted sheet names the rendered html's markers reference, template content excluded.
func Scan(html string) []string {
	live, _ := ScanSplit(html)
	return live
}

// ScanSplit is Scan plus held, the names only inert template content references.
func ScanSplit(html string) (live, held []string) {
	var tpl strings.Builder
	live = scanNames(splitInertTemplates(html, &tpl), nil)
	if tpl.Len() > 0 {
		held = scanNames(tpl.String(), live)
	}
	return live, held
}

func scanNames(html string, skip []string) []string {
	seen := map[string]struct{}{}
	for n := range attrscan.Values(html, markerAttrs, isTagNameByte, true) {
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

// EagerNames returns the sorted names of every LoadAlways entry.
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

func splitInertTemplates(html string, held *strings.Builder) string {
	i := indexTemplateTag(html, 0)
	if i < 0 {
		return html
	}
	var b strings.Builder
	b.Grow(len(html))
	last := 0
	for i >= 0 {
		if html[i+1] == '/' {
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
			i++
			continue
		case asciiEqualFold(name, "template"):
			return i
		}
		i = startTagEnd(html, i)
		if !closing && isRawTextElement(name) {
			end := indexEndTag(html, i, name)
			if end < 0 {
				return -1
			}
			i = end
		}
	}
	return -1
}

var rawTextElements = []string{"script", "style", "textarea", "title", "xmp", "iframe", "noembed", "noframes", "noscript"}

func isRawTextElement(name string) bool {
	for _, r := range rawTextElements {
		if asciiEqualFold(name, r) {
			return true
		}
	}
	return false
}

func indexEndTag(html string, from int, name string) int {
	for i := from; i < len(html); i++ {
		j := strings.Index(html[i:], "</")
		if j < 0 {
			return -1
		}
		i += j
		n := i + 2
		if n+len(name) <= len(html) && asciiEqualFold(html[n:n+len(name)], name) &&
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

func hasAttrName(tag, name string) bool {
	_, ok := findAttr(tag, 0, name)
	return ok
}

func isHTMLSpace(c byte) bool {
	return c == ' ' || c == '\t' || c == '\n' || c == '\r' || c == '\f'
}

func startTagEnd(html string, i int) int {
	end, _ := walkTag(html, i, nil)
	if end < 0 {
		return len(html)
	}
	return end + 1
}
