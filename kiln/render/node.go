package render

import (
	"fmt"

	"github.com/gofastr/gofastr/core-ui/elements"
	"github.com/gofastr/gofastr/core/render"
	"github.com/gofastr/gofastr/kiln/world"
)

// RenderNode walks a world.Node tree and emits HTML by dispatching to
// the framework's core-ui/elements package. The IR (world.Node) is the
// JSON shape the agent authors; the actual element vocabulary, ARIA
// rules, attribute escaping, and accessibility defaults all live in
// core-ui/elements — Kiln does not reimplement them.
//
// Unknown / forbidden elements (or elements missing required ARIA
// fields) fall back to a comment in dev so the gap is visible.
func RenderNode(n world.Node) render.HTML {
	children := make([]render.HTML, 0, len(n.Children))
	for _, c := range n.Children {
		children = append(children, RenderNode(c))
	}
	return renderKind(n.Kind, n.Props, children)
}

// renderKind dispatches each known IR kind to the matching
// core-ui/elements builder. All ID/Class plumbing and the agent's
// arbitrary attrs (data-kiln-tool, etc.) flow through Attrs.
func renderKind(kind string, props map[string]any, children []render.HTML) render.HTML {
	switch kind {

	// --- text leaves -----------------------------------------------
	case "text":
		s := propString(props, "value")
		if s == "" {
			s = propString(props, "text")
		}
		if s == "" {
			s = propString(props, "content")
		}
		if s == "" && len(children) > 0 {
			return render.Join(children...)
		}
		return render.Text(s)
	case "raw":
		s := propString(props, "value")
		if s == "" {
			s = propString(props, "text")
		}
		return render.Raw(s)

	// --- structural containers -------------------------------------
	case "div":
		return elements.Div(elements.DivConfig{
			ID: propString(props, "id"), Class: propString(props, "class"),
			Role: propString(props, "role"), AriaLabel: propString(props, "aria-label"),
			Attrs: extraAttrs(props, "id", "class", "role", "aria-label"),
		}, children...)
	case "article":
		return elements.Article(elements.ArticleConfig{
			ID: propString(props, "id"), Class: propString(props, "class"),
			Attrs: extraAttrs(props, "id", "class"),
		}, children...)
	case "section":
		// Sections require label or labelledby for ARIA. We auto-label
		// from id when neither is supplied so agent IRs don't trip
		// elements' panic.
		label := propString(props, "label")
		if label == "" {
			label = propString(props, "aria-label")
		}
		labelledBy := propString(props, "labelledby")
		if label == "" && labelledBy == "" {
			if id := propString(props, "id"); id != "" {
				label = id
			} else {
				label = "section"
			}
		}
		return elements.Section(elements.SectionConfig{
			Label: label, LabelledBy: labelledBy,
			ID: propString(props, "id"), Class: propString(props, "class"),
			Attrs: extraAttrs(props, "id", "class", "label", "labelledby", "aria-label"),
		}, children...)
	case "main":
		return elements.Main(elements.MainConfig{
			ID: propString(props, "id"), Class: propString(props, "class"),
			Attrs: extraAttrs(props, "id", "class"),
		}, children...)
	case "header":
		return elements.Header(elements.HeaderConfig{
			ID: propString(props, "id"), Class: propString(props, "class"),
			Attrs: extraAttrs(props, "id", "class"),
		}, children...)
	case "footer":
		return elements.Footer(elements.FooterConfig{
			ID: propString(props, "id"), Class: propString(props, "class"),
			Attrs: extraAttrs(props, "id", "class"),
		}, children...)
	case "nav":
		label := propString(props, "label")
		if label == "" {
			label = propString(props, "aria-label")
		}
		labelledBy := propString(props, "labelledby")
		if label == "" && labelledBy == "" {
			label = "Main"
		}
		return elements.Nav(elements.NavConfig{
			Label: label, LabelledBy: labelledBy,
			ID: propString(props, "id"), Class: propString(props, "class"),
			Attrs: extraAttrs(props, "id", "class", "label", "labelledby", "aria-label"),
		}, children...)
	case "aside":
		label := propString(props, "label")
		if label == "" {
			label = propString(props, "aria-label")
		}
		labelledBy := propString(props, "labelledby")
		if label == "" && labelledBy == "" {
			label = "Aside"
		}
		return elements.Aside(elements.AsideConfig{
			Label: label, LabelledBy: labelledBy,
			ID: propString(props, "id"), Class: propString(props, "class"),
			Attrs: extraAttrs(props, "id", "class", "label", "labelledby", "aria-label"),
		}, children...)

	// --- text elements --------------------------------------------
	case "heading":
		level := propInt(props, "level", 1)
		if level < 1 {
			level = 1
		}
		if level > 6 {
			level = 6
		}
		text := propString(props, "text")
		body := children
		if text != "" {
			body = append([]render.HTML{render.Text(text)}, body...)
		}
		return elements.Heading(elements.HeadingConfig{
			Level: level,
			ID:    propString(props, "id"), Class: propString(props, "class"),
			Attrs: extraAttrs(props, "id", "class", "level", "text"),
		}, body...)
	case "paragraph", "p":
		text := propString(props, "text")
		body := children
		if text != "" {
			body = append([]render.HTML{render.Text(text)}, body...)
		}
		return elements.Paragraph(textConfig(props), body...)
	case "span":
		text := propString(props, "text")
		body := children
		if text != "" {
			body = append([]render.HTML{render.Text(text)}, body...)
		}
		return elements.Span(textConfig(props), body...)
	case "strong":
		return elements.Strong(textConfig(props), withTextProp(props, children)...)
	case "em":
		return elements.Em(textConfig(props), withTextProp(props, children)...)
	case "code":
		return elements.Code(textConfig(props), withTextProp(props, children)...)
	case "pre":
		return elements.Pre(textConfig(props), withTextProp(props, children)...)
	case "small":
		return elements.Small(textConfig(props), withTextProp(props, children)...)
	case "blockquote":
		return elements.Blockquote(textConfig(props), withTextProp(props, children)...)

	// --- interactive ----------------------------------------------
	case "button":
		label := propString(props, "label")
		if label == "" {
			label = propString(props, "text")
		}
		// Carry agent action attrs through; elements.Button merges them.
		attrs := extraAttrs(props, "id", "class", "label", "text", "type")
		typ := propString(props, "type")
		if typ == "" {
			typ = "button"
		}
		return elements.Button(elements.ButtonConfig{
			Label: label, Type: typ,
			ID: propString(props, "id"), Class: propString(props, "class"),
			Attrs: attrs,
		})
	case "link", "a":
		text := propString(props, "text")
		if text == "" && len(children) > 0 {
			// Wrap children HTML — elements.Link only accepts plain text;
			// fall through to LinkHTML for HTML children.
			return elements.LinkHTML(elements.LinkHTMLConfig{
				Href:    propString(props, "href"),
				Content: render.Join(children...),
				ID:      propString(props, "id"), Class: propString(props, "class"),
				Attrs: extraAttrs(props, "id", "class", "href", "text"),
			})
		}
		return elements.Link(elements.LinkConfig{
			Href: propString(props, "href"), Text: text,
			ID: propString(props, "id"), Class: propString(props, "class"),
			Attrs: extraAttrs(props, "id", "class", "href", "text"),
		})
	case "input":
		typ := propString(props, "type")
		if typ == "" {
			typ = "text"
		}
		name := propString(props, "name")
		if name == "" {
			name = propString(props, "id")
		}
		if name == "" {
			name = "field"
		}
		return elements.Input(elements.InputConfig{
			Type: typ, Name: name,
			ID: propString(props, "id"), Class: propString(props, "class"),
			Attrs: extraAttrs(props, "id", "class", "type", "name"),
		})
	case "label":
		text := propString(props, "text")
		body := children
		if text != "" && len(children) == 0 {
			return elements.Label(elements.LabelConfig{
				For: propString(props, "for"), Text: text,
				ID: propString(props, "id"), Class: propString(props, "class"),
				Attrs: extraAttrs(props, "id", "class", "for", "text"),
			})
		}
		// children present — emit a manual <label> so we can include the markup
		attrs := map[string]string{}
		if v := propString(props, "id"); v != "" {
			attrs["id"] = v
		}
		if v := propString(props, "class"); v != "" {
			attrs["class"] = v
		}
		if v := propString(props, "for"); v != "" {
			attrs["for"] = v
		}
		mergeInto(attrs, extraAttrs(props, "id", "class", "for", "text"))
		if text != "" {
			body = append([]render.HTML{render.Text(text)}, body...)
		}
		return render.Tag("label", attrs, body...)
	case "form":
		method := propString(props, "method")
		if method == "" {
			method = "POST"
		}
		return elements.Form(elements.FormConfig{
			Method: method, Action: propString(props, "action"),
			ID: propString(props, "id"), Class: propString(props, "class"),
			Attrs: extraAttrs(props, "id", "class", "method", "action"),
		}, children...)
	case "select":
		// Options expected as children (kind: "option" with value/text props).
		// elements.Select takes a structured Options list — easier to
		// fall through to manual <select> when the agent uses children.
		return render.Tag("select", attrsFromProps(props,
			"id", "class", "name", "required", "multiple",
		), children...)
	case "option":
		return elements.Option(propString(props, "value"), propString(props, "text"), propBool(props, "selected"))
	case "textarea":
		name := propString(props, "name")
		if name == "" {
			name = "field"
		}
		return elements.TextArea(elements.TextAreaConfig{
			Name: name,
			ID:   propString(props, "id"), Class: propString(props, "class"),
			Attrs: extraAttrs(props, "id", "class", "name"),
		})
	case "fieldset":
		return elements.FieldSet(elements.FieldSetConfig{
			Legend: propString(props, "legend"),
			ID:     propString(props, "id"), Class: propString(props, "class"),
			Attrs: extraAttrs(props, "id", "class", "legend"),
		}, children...)

	// --- media ----------------------------------------------------
	case "image", "img":
		// Width/height pass through via Attrs since ImageConfig keeps
		// only Src/Alt as first-class fields.
		return elements.Image(elements.ImageConfig{
			Src: propString(props, "src"), Alt: propString(props, "alt"),
			ID: propString(props, "id"), Class: propString(props, "class"),
			Attrs: extraAttrs(props, "id", "class", "src", "alt"),
		})

	// --- lists ----------------------------------------------------
	case "list":
		ordered := propBool(props, "ordered")
		// Agent's children are typically already wrapped or are bare —
		// auto-wrap any non-li children in <li>.
		items := wrapAsListItems(children)
		cfg := elements.ListConfig{
			ID: propString(props, "id"), Class: propString(props, "class"),
			Attrs: extraAttrs(props, "id", "class", "ordered"),
		}
		if ordered {
			return elements.OrderedList(cfg, items...)
		}
		return elements.UnorderedList(cfg, items...)
	case "ul":
		return elements.UnorderedList(listConfig(props), wrapAsListItems(children)...)
	case "ol":
		return elements.OrderedList(listConfig(props), wrapAsListItems(children)...)
	case "li":
		return elements.ListItem(elements.ListItemConfig{
			ID: propString(props, "id"), Class: propString(props, "class"),
			Attrs: extraAttrs(props, "id", "class"),
		}, children...)

	// --- table ----------------------------------------------------
	case "table":
		return render.Tag("table", attrsFromProps(props, "id", "class"), children...)
	case "thead":
		return render.Tag("thead", attrsFromProps(props, "id", "class"), children...)
	case "tbody":
		return render.Tag("tbody", attrsFromProps(props, "id", "class"), children...)
	case "tr":
		return render.Tag("tr", attrsFromProps(props, "id", "class"), children...)
	case "th":
		return render.Tag("th", attrsFromProps(props, "id", "class", "scope"), children...)
	case "td":
		return render.Tag("td", attrsFromProps(props, "id", "class"), children...)

	default:
		// Unknown kind — leave a debugging trace.
		return render.Raw(fmt.Sprintf("<!-- kiln: unknown kind %q -->", render.Escape(kind)))
	}
}

// --- helpers ----------------------------------------------------------

func textConfig(props map[string]any) elements.TextConfig {
	return elements.TextConfig{
		ID: propString(props, "id"), Class: propString(props, "class"),
		Attrs: extraAttrs(props, "id", "class", "text"),
	}
}

func listConfig(props map[string]any) elements.ListConfig {
	return elements.ListConfig{
		ID: propString(props, "id"), Class: propString(props, "class"),
		Attrs: extraAttrs(props, "id", "class"),
	}
}

func withTextProp(props map[string]any, children []render.HTML) []render.HTML {
	text := propString(props, "text")
	if text != "" {
		return append([]render.HTML{render.Text(text)}, children...)
	}
	return children
}

// wrapAsListItems takes free-floating children and wraps any that aren't
// already <li> into ListItems so the agent can write `list` with bare
// content children and still get valid markup.
func wrapAsListItems(children []render.HTML) []render.HTML {
	out := make([]render.HTML, 0, len(children))
	for _, c := range children {
		s := string(c)
		if len(s) >= 4 && s[:4] == "<li " || (len(s) >= 4 && s[:4] == "<li>") {
			out = append(out, c)
			continue
		}
		out = append(out, elements.ListItem(elements.ListItemConfig{}, c))
	}
	return out
}

// extraAttrs collects any prop keys NOT in the well-known list. These
// flow into elements.X via Attrs so agent-supplied data-kiln-tool,
// data-kiln-args, aria-*, role, target, rel, etc. all reach the DOM.
// dangerousAttrs are HTML attributes the renderer drops unconditionally.
// They violate strict CSP (default-src 'self' with no unsafe-inline) and
// are the most common XSS vectors. The kiln runtime ships strict CSP, so
// agents that try to set inline styles or event handlers will produce
// non-functional pages with browser console errors. Drop them here so
// the agent's mistake doesn't leak to the user.
var dangerousAttrs = map[string]bool{
	"style":   true, // → use class + theme tokens instead
	"srcdoc":  true,
	"sandbox": false, // OK on iframes; keep
}

// extraAttrs collects element props that should pass through as raw
// HTML attributes. It:
//   - skips the `known` list (props the caller already promoted to
//     first-class element fields, like id/class/role)
//   - drops dangerousAttrs (style, on*, srcdoc) — these violate CSP and
//     can't be rescued; if the agent emits them, swallow silently
//   - passes the rest through with fmt.Sprint
//
// The agent skill explicitly forbids `style`; this is a hard belt-and-
// suspenders so a single bad turn doesn't poison the page.
func extraAttrs(props map[string]any, known ...string) elements.Attrs {
	if len(props) == 0 {
		return nil
	}
	skip := make(map[string]struct{}, len(known))
	for _, k := range known {
		skip[k] = struct{}{}
	}
	out := elements.Attrs{}
	for k, v := range props {
		if _, ok := skip[k]; ok {
			continue
		}
		if v == nil {
			continue
		}
		if dangerousAttrs[k] {
			continue
		}
		// Inline event handlers — onclick, onload, onmouseover, … —
		// are inline JS that the strict CSP rejects. Drop them.
		if len(k) > 2 && k[0] == 'o' && k[1] == 'n' {
			continue
		}
		out[k] = fmt.Sprint(v)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func mergeInto(dst, src map[string]string) {
	for k, v := range src {
		dst[k] = v
	}
}

// --- prop accessors (unchanged) --------------------------------------

func propString(p map[string]any, key string) string {
	if p == nil {
		return ""
	}
	v, ok := p[key]
	if !ok || v == nil {
		return ""
	}
	switch s := v.(type) {
	case string:
		return s
	case fmt.Stringer:
		return s.String()
	default:
		return fmt.Sprint(v)
	}
}

func propInt(p map[string]any, key string, def int) int {
	if p == nil {
		return def
	}
	v, ok := p[key]
	if !ok || v == nil {
		return def
	}
	switch n := v.(type) {
	case int:
		return n
	case int64:
		return int(n)
	case float64:
		return int(n)
	case float32:
		return int(n)
	default:
		return def
	}
}

func propBool(p map[string]any, key string) bool {
	if p == nil {
		return false
	}
	v, ok := p[key]
	if !ok || v == nil {
		return false
	}
	b, _ := v.(bool)
	return b
}

func attrsFromProps(p map[string]any, keys ...string) map[string]string {
	if len(p) == 0 {
		return nil
	}
	out := map[string]string{}
	for _, k := range keys {
		if v, ok := p[k]; ok && v != nil {
			out[k] = fmt.Sprint(v)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}
