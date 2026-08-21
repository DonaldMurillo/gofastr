package noderender

import (
	"fmt"
	"maps"
	"strings"

	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core-ui/node"
	"github.com/DonaldMurillo/gofastr/core/render"
)

// RenderNode walks a node.Node tree and emits HTML by dispatching to
// the core-ui/html package. The IR (node.Node) is the JSON shape an
// author (blueprint codegen or Kiln) declares; the actual element
// vocabulary, ARIA rules, attribute escaping, and accessibility defaults
// all live in core-ui/html, this renderer does not reimplement them.
//
// Unknown / forbidden elements (or elements missing required ARIA
// fields) fall back to a comment in dev so the gap is visible.
// RenderNode treats the IR as UNTRUSTED. See RenderTrustedNode for the
// first-party counterpart, and actionAttrs for what that distinction
// costs an untrusted tree.
func RenderNode(n node.Node) render.HTML {
	return renderNode(n, false)
}

// RenderTrustedNode is RenderNode for a FIRST-PARTY IR, one whose props
// were authored by the developer, not by an agent or by anything derived
// from user input.
//
// The only difference is that a trusted tree may name the action
// attributes actionAttrs strips: data-action, data-action-* and
// data-param-*. cmd/gofastr's blueprint generator is the caller this
// exists for, it compiles a developer's own YAML into IR and wires
// button actions through exactly those attributes.
//
// Do NOT reach for this to silence a dropped attribute. If the IR came
// from Kiln, a process module, an LLM, or any user-supplied document, it
// is untrusted no matter how convenient the attribute would be.
func RenderTrustedNode(n node.Node) render.HTML {
	return renderNode(n, true)
}

func renderNode(n node.Node, trusted bool) render.HTML {
	children := make([]render.HTML, 0, len(n.Children))
	for _, c := range n.Children {
		// Trust flows down the whole tree: a trusted root's children
		// came from the same first-party document.
		children = append(children, renderNode(c, trusted))
	}
	props := n.Props
	if !trusted {
		props = withoutActionAttrs(props)
	}
	return renderKind(n.Kind, props, children)
}

// RenderKind renders a single node's own element from pre-rendered children,
// without walking the subtree. Callers with a wider vocabulary (kiln/render's
// typed design-system kinds) dispatch each child themselves and use this for
// the one-to-one semantic HTML shell, so a typed kind nested inside a leaf
// container still renders through its component.
func RenderKind(kind string, props map[string]any, children []render.HTML) render.HTML {
	return renderKind(kind, withoutActionAttrs(props), children)
}

// RenderTrustedKind is RenderKind for a first-party IR. See
// RenderTrustedNode for when that is and is not appropriate.
func RenderTrustedKind(kind string, props map[string]any, children []render.HTML) render.HTML {
	return renderKind(kind, props, children)
}

// actionAttrs are the runtime-privileged attributes that only a
// first-party IR may name. They are stripped at the untrusted entry
// point rather than inside attrAllowed because the blueprint generator
// legitimately emits all three through the SAME renderer that Kiln uses
// for agent-authored IR, the attribute name alone cannot tell the two
// apart, so the caller has to.
//
// Why they matter: core-ui/runtime/frag/boot.js resolves the nearest
// [data-component] ancestor and calls window.__gofastr.trigger() with
// the action name and the data-param-* map. data-action-mount fires at
// hydration and again on every gofastr:navigate, with no user
// interaction. So an untrusted IR naming these picks a compiled server
// action AND its arguments, and runs it on the host island's behalf.
func withoutActionAttrs(props map[string]any) map[string]any {
	if len(props) == 0 {
		return props
	}
	var out map[string]any
	for k := range props {
		lower := strings.ToLower(k)
		if lower != "data-action" &&
			!strings.HasPrefix(lower, "data-action-") &&
			!strings.HasPrefix(lower, "data-param-") {
			continue
		}
		// Copy lazily: the overwhelming majority of nodes name none of
		// these, and rewriting every props map would be pure garbage.
		if out == nil {
			out = make(map[string]any, len(props))
			maps.Copy(out, props)
		}
		delete(out, k)
	}
	if out == nil {
		return props
	}
	return out
}

// renderKind dispatches each known IR kind to the matching
// core-ui/html builder. All ID/Class plumbing and the agent's
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
	// NOTE: there is intentionally no "raw" kind. The IR (world.Node) is
	// agent-authored and untrusted, Kind is free-form with no whitelist,
	// so a raw-HTML passthrough would be an XSS sink. A "raw" node falls
	// through to the default branch below, which emits an escaped debug
	// comment instead of live markup. (finding k-raw-1)

	// --- structural containers -------------------------------------
	case "div":
		return html.Div(html.DivConfig{
			ID: propString(props, "id"), Class: propString(props, "class"),
			Role: propString(props, "role"), AriaLabel: propString(props, "aria-label"),
			ExtraAttrs: extraAttrs(props, "id", "class", "role", "aria-label"),
		}, children...)
	case "article":
		return html.Article(html.ArticleConfig{
			ID: propString(props, "id"), Class: propString(props, "class"),
			ExtraAttrs: extraAttrs(props, "id", "class"),
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
		return html.Section(html.SectionConfig{
			Label: label, LabelledBy: labelledBy,
			ID: propString(props, "id"), Class: propString(props, "class"),
			ExtraAttrs: extraAttrs(props, "id", "class", "label", "labelledby", "aria-label"),
		}, children...)
	case "main":
		return html.Main(html.MainConfig{
			ID: propString(props, "id"), Class: propString(props, "class"),
			ExtraAttrs: extraAttrs(props, "id", "class"),
		}, children...)
	case "header":
		return html.Header(html.HeaderConfig{
			ID: propString(props, "id"), Class: propString(props, "class"),
			ExtraAttrs: extraAttrs(props, "id", "class"),
		}, children...)
	case "footer":
		return html.Footer(html.FooterConfig{
			ID: propString(props, "id"), Class: propString(props, "class"),
			ExtraAttrs: extraAttrs(props, "id", "class"),
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
		return html.Nav(html.NavConfig{
			Label: label, LabelledBy: labelledBy,
			ID: propString(props, "id"), Class: propString(props, "class"),
			ExtraAttrs: extraAttrs(props, "id", "class", "label", "labelledby", "aria-label"),
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
		return html.Aside(html.AsideConfig{
			Label: label, LabelledBy: labelledBy,
			ID: propString(props, "id"), Class: propString(props, "class"),
			ExtraAttrs: extraAttrs(props, "id", "class", "label", "labelledby", "aria-label"),
		}, children...)

	// --- text elements --------------------------------------------
	case "heading":
		level := min(max(propInt(props, "level", 1), 1), 6)
		text := propString(props, "text")
		body := children
		if text != "" {
			body = append([]render.HTML{render.Text(text)}, body...)
		}
		return html.Heading(html.HeadingConfig{
			Level: level,
			ID:    propString(props, "id"), Class: propString(props, "class"),
			ExtraAttrs: extraAttrs(props, "id", "class", "level", "text"),
		}, body...)
	case "paragraph", "p":
		text := propString(props, "text")
		body := children
		if text != "" {
			body = append([]render.HTML{render.Text(text)}, body...)
		}
		return html.Paragraph(textConfig(props), body...)
	case "span":
		text := propString(props, "text")
		body := children
		if text != "" {
			body = append([]render.HTML{render.Text(text)}, body...)
		}
		return html.Span(textConfig(props), body...)
	case "strong":
		return html.Strong(textConfig(props), withTextProp(props, children)...)
	case "em":
		return html.Em(textConfig(props), withTextProp(props, children)...)
	case "code":
		return html.Code(textConfig(props), withTextProp(props, children)...)
	case "pre":
		return html.Pre(textConfig(props), withTextProp(props, children)...)
	case "small":
		return html.Small(textConfig(props), withTextProp(props, children)...)
	case "blockquote":
		return html.Blockquote(textConfig(props), withTextProp(props, children)...)

	// --- interactive ----------------------------------------------
	case "button":
		label := propString(props, "label")
		if label == "" {
			label = propString(props, "text")
		}
		// Carry agent action attrs through; html.Button merges them.
		attrs := extraAttrs(props, "id", "class", "label", "text", "type")
		if label == "" && attrs["aria-label"] == "" {
			// html.Button panics on a labelless button, the right
			// contract for hand-written Go, but IR props reach this
			// path at request time (Kiln renders on every page load,
			// recover middleware is opt-in). Degrade with a placeholder
			// aria-label instead of crashing the render.
			if attrs == nil {
				attrs = html.Attrs{}
			}
			attrs["aria-label"] = "button"
		}
		typ := propString(props, "type")
		if typ == "" {
			typ = "button"
		}
		return html.Button(html.ButtonConfig{
			Label: label, Type: typ,
			ID: propString(props, "id"), Class: propString(props, "class"),
			ExtraAttrs: attrs,
		})
	case "link", "a":
		text := propString(props, "text")
		if text == "" && len(children) > 0 {
			// Wrap children HTML, html.Link only accepts plain text;
			// fall through to LinkHTML for HTML children.
			return html.LinkHTML(html.LinkHTMLConfig{
				Href:    propString(props, "href"),
				Content: render.Join(children...),
				ID:      propString(props, "id"), Class: propString(props, "class"),
				ExtraAttrs: extraAttrs(props, "id", "class", "href", "text"),
			})
		}
		return html.Link(html.LinkConfig{
			Href: propString(props, "href"), Text: text,
			ID: propString(props, "id"), Class: propString(props, "class"),
			ExtraAttrs: extraAttrs(props, "id", "class", "href", "text"),
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
		return html.Input(html.InputConfig{
			Type: typ, Name: name,
			ID: propString(props, "id"), Class: propString(props, "class"),
			ExtraAttrs: extraAttrs(props, "id", "class", "type", "name"),
		})
	case "label":
		text := propString(props, "text")
		body := children
		if text != "" && len(children) == 0 {
			return html.Label(html.LabelConfig{
				For: propString(props, "for"), Text: text,
				ID: propString(props, "id"), Class: propString(props, "class"),
				ExtraAttrs: extraAttrs(props, "id", "class", "for", "text"),
			})
		}
		// children present, emit a manual <label> so we can include the markup
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
		// Kiln-rendered forms target the world's CRUD endpoints, which
		// accept JSON. Default enctype to application/json so the
		// runtime's safe-by-default form interceptor knows to JSON-wrap
		// the body instead of letting the browser submit it as
		// urlencoded (which the CRUD handlers don't decode).
		attrs := extraAttrs(props, "id", "class", "method", "action")
		if _, set := attrs["enctype"]; !set {
			if propString(props, "enctype") == "" {
				if attrs == nil {
					attrs = map[string]string{}
				}
				attrs["enctype"] = "application/json"
			}
		}
		return html.Form(html.FormConfig{
			Method: method, Action: propString(props, "action"),
			ID: propString(props, "id"), Class: propString(props, "class"),
			ExtraAttrs: attrs,
		}, children...)
	case "select":
		// Options expected as children (kind: "option" with value/text props).
		// html.Select takes a structured Options list, easier to
		// fall through to manual <select> when the agent uses children.
		return render.Tag("select", attrsFromProps(props,
			"id", "class", "name", "required", "multiple",
		), children...)
	case "option":
		return html.Option(propString(props, "value"), propString(props, "text"), propBool(props, "selected"))
	case "textarea":
		name := propString(props, "name")
		if name == "" {
			name = "field"
		}
		return html.TextArea(html.TextAreaConfig{
			Name: name,
			ID:   propString(props, "id"), Class: propString(props, "class"),
			ExtraAttrs: extraAttrs(props, "id", "class", "name"),
		})
	case "fieldset":
		return html.FieldSet(html.FieldSetConfig{
			Legend: propString(props, "legend"),
			ID:     propString(props, "id"), Class: propString(props, "class"),
			ExtraAttrs: extraAttrs(props, "id", "class", "legend"),
		}, children...)

	// --- media ----------------------------------------------------
	case "image", "img":
		// Width/height pass through via Attrs since ImageConfig keeps
		// only Src/Alt as first-class fields.
		return html.Image(html.ImageConfig{
			Src: propString(props, "src"), Alt: propString(props, "alt"),
			ID: propString(props, "id"), Class: propString(props, "class"),
			ExtraAttrs: extraAttrs(props, "id", "class", "src", "alt"),
		})

	// --- lists ----------------------------------------------------
	case "list":
		ordered := propBool(props, "ordered")
		// Agent's children are typically already wrapped or are bare,
		// auto-wrap any non-li children in <li>.
		items := wrapAsListItems(children)
		cfg := html.ListConfig{
			ID: propString(props, "id"), Class: propString(props, "class"),
			ExtraAttrs: extraAttrs(props, "id", "class", "ordered"),
		}
		if ordered {
			return html.OrderedList(cfg, items...)
		}
		return html.UnorderedList(cfg, items...)
	case "ul":
		return html.UnorderedList(listConfig(props), wrapAsListItems(children)...)
	case "ol":
		return html.OrderedList(listConfig(props), wrapAsListItems(children)...)
	case "li":
		return html.ListItem(html.ListItemConfig{
			ID: propString(props, "id"), Class: propString(props, "class"),
			ExtraAttrs: extraAttrs(props, "id", "class"),
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
		// Unknown kind, leave a debugging trace.
		return render.Raw(fmt.Sprintf("<!-- noderender: unknown kind %q -->", render.Escape(kind)))
	}
}

// --- helpers ----------------------------------------------------------

func textConfig(props map[string]any) html.TextConfig {
	return html.TextConfig{
		ID: propString(props, "id"), Class: propString(props, "class"),
		ExtraAttrs: extraAttrs(props, "id", "class", "text"),
	}
}

func listConfig(props map[string]any) html.ListConfig {
	return html.ListConfig{
		ID: propString(props, "id"), Class: propString(props, "class"),
		ExtraAttrs: extraAttrs(props, "id", "class"),
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
		out = append(out, html.ListItem(html.ListItemConfig{}, c))
	}
	return out
}

// extraAttrs collects any prop keys NOT in the well-known list. These
// flow into html.X via Attrs so agent-supplied data-kiln-tool,
// data-kiln-args, aria-*, role, target, rel, etc. all reach the DOM.
// allowedAttrs is the set of NON-data attribute names an untrusted IR may
// emit. See attrAllowed for the full rule.
var allowedAttrs = map[string]bool{
	// identity + presentation
	"id": true, "class": true, "title": true, "dir": true, "lang": true,
	"role": true, "slot": true, "hidden": true, "tabindex": true,
	// media / link semantics (the URL values themselves are scheme-checked
	// by core-ui/html; these are the non-URL knobs beside them)
	"alt": true, "target": true, "rel": true, "type": true, "loading": true,
	"decoding": true, "width": true, "height": true, "download": true,
	// form semantics
	"name": true, "value": true, "placeholder": true, "for": true,
	"method": true, "enctype": true, "autocomplete": true, "inputmode": true,
	"required": true, "disabled": true, "readonly": true, "checked": true,
	"selected": true, "multiple": true, "min": true, "max": true, "step": true,
	"minlength": true, "maxlength": true, "pattern": true, "rows": true, "cols": true,
	// table semantics
	"colspan": true, "rowspan": true, "scope": true, "headers": true,
	// details/dialog
	"open": true,
}

// privilegedDataAttrs are the data-* attributes the RUNTIME itself acts
// on, and therefore the ones an untrusted IR must never name:
//
//   - data-behavior becomes a <script src>, arbitrary code
//   - data-widget / data-component / data-island are hydration identity:
//     naming one makes the element impersonate a registered island, and
//     data-island is what core-ui/runtime/src/sse.js targets when it
//     swaps server-pushed content into a region
//   - data-bind writes into the client state store
//   - the whole data-fui-* family drives signals, RPC, polling and
//     navigation
//
// Everything else in the data- namespace is an inert marker read only by
// the host's own code (the blueprint's data-field / data-entity-list-*,
// kiln's data-kiln-tool, test hooks), so it passes. That split is the
// point: the IR may describe UI, it may not reach the runtime.
//
// Keep this in sync with what the runtime actually reads. The list is a
// claim about core-ui/runtime, and it was wrong once: data-action* and
// data-param-* were treated as inert host markers while
// core-ui/runtime/frag/boot.js was dispatching them into
// window.__gofastr.trigger(), at hydration and again on every
// gofastr:navigate, with the IR choosing both the compiled action and,
// via data-param-*, its arguments. To re-derive the set:
//
//	grep -ohE "(getAttribute|hasAttribute|closest|querySelector[All]*|matches)\(['\"][^'\"]*data-[a-z-]+" \
//	  core-ui/runtime/frag/*.js core-ui/runtime/src/*.js | grep -oE 'data-[a-z-]+' | sort -u
var privilegedDataAttrs = map[string]bool{
	"data-behavior": true, "data-widget": true,
	"data-component": true, "data-bind": true,
	"data-island": true,
}

// privilegedDataPrefixes are runtime-privileged data-* FAMILIES. An
// untrusted IR must not name any attribute starting with one of these.
//
// data-action-* and data-param-* are deliberately NOT here: they are
// stripped a layer earlier, by withoutActionAttrs at the untrusted entry
// point. They cannot be handled by name alone, because this renderer
// serves two callers with different trust, cmd/gofastr's blueprint
// compiles developer-authored YAML and legitimately emits them, while
// kiln renders agent-authored IR through the same code. The trust split
// (RenderNode vs RenderTrustedNode) is what tells the two apart. See
// actionAttrs for why they matter and TestIRCannotFireActions for the pin.
var privilegedDataPrefixes = []string{
	"data-fui-",
}

// extraAttrs collects element props that should pass through as raw
// HTML attributes. It:
//   - skips the `known` list (props the caller already promoted to
//     first-class element fields, like id/class/role)
//   - keeps only allowedAttrs plus `aria-*`
//   - passes the rest through with fmt.Sprint
//
// Silently dropping (rather than erroring) is deliberate: the IR is
// generated a turn at a time, and one bad turn should degrade the page,
// not fail the render.
func extraAttrs(props map[string]any, known ...string) html.Attrs {
	if len(props) == 0 {
		return nil
	}
	skip := make(map[string]struct{}, len(known))
	for _, k := range known {
		skip[k] = struct{}{}
	}
	out := html.Attrs{}
	for k, v := range props {
		if _, ok := skip[k]; ok {
			continue
		}
		if v == nil {
			continue
		}
		if !attrAllowed(k) {
			continue
		}
		out[k] = fmt.Sprint(v)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// attrAllowed reports whether an IR-supplied attribute name may be
// emitted:
//
//	aria-*                       → yes
//	data-*                       → yes, unless runtime-privileged
//	a known presentational attr  → yes
//	anything else                → no
//
// The last line is what closes the original hole: the previous rule was
// a deny-list of three names, so `style`, every `on*` handler, `srcdoc`,
// and every attribute nobody had thought of yet all passed.
//
// Matching is case-insensitive, HTML attribute names are
// case-insensitive to the parser, so an `OnClick` prop is an event
// handler no matter how the author cased it.
func attrAllowed(name string) bool {
	lower := strings.ToLower(name)
	if strings.HasPrefix(lower, "aria-") {
		return true
	}
	if strings.HasPrefix(lower, "data-") {
		if privilegedDataAttrs[lower] {
			return false
		}
		for _, p := range privilegedDataPrefixes {
			if strings.HasPrefix(lower, p) {
				return false
			}
		}
		return true
	}
	return allowedAttrs[lower]
}

func mergeInto(dst, src map[string]string) {
	maps.Copy(dst, src)
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
