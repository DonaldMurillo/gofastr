package noderender

import (
	"fmt"
	"maps"
	"strings"

	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core-ui/node"
	"github.com/DonaldMurillo/gofastr/core-ui/registry"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/internal/asciicase"
)

// RenderNode renders an untrusted tree, dropping the attributes the runtime acts on.
func RenderNode(n node.Node) render.HTML {
	return renderNode(n, false)
}

// RenderTrustedNode renders a first-party tree, keeping its action attributes.
func RenderTrustedNode(n node.Node) render.HTML {
	return renderNode(n, true)
}

func renderNode(n node.Node, trusted bool) render.HTML {
	children := make([]render.HTML, 0, len(n.Children))
	for _, c := range n.Children {
		children = append(children, renderNode(c, trusted))
	}
	props := n.Props
	if !trusted {
		props = withoutActionAttrs(props)
	}
	return renderKind(n.Kind, props, children)
}

// RenderKind renders one untrusted node's element around pre-rendered children.
func RenderKind(kind string, props map[string]any, children []render.HTML) render.HTML {
	return renderKind(kind, withoutActionAttrs(props), children)
}

// RenderTrustedKind is RenderKind for a first-party node.
func RenderTrustedKind(kind string, props map[string]any, children []render.HTML) render.HTML {
	return renderKind(kind, props, children)
}

func withoutActionAttrs(props map[string]any) map[string]any {
	if len(props) == 0 {
		return props
	}
	var out map[string]any
	for k := range props {
		lower := strings.ToLower(k)
		if lower != "data-action" &&
			!strings.HasPrefix(lower, "data-action-") &&
			!strings.HasPrefix(lower, "data-param-") &&
			!registry.IsMarkerAttr(lower) {
			continue
		}
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

func renderKind(kind string, props map[string]any, children []render.HTML) render.HTML {
	switch kind {

	case "text":
		s := textContentProp(props)
		if s == "" && len(children) > 0 {
			return render.Join(children...)
		}
		return render.Text(s)
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
		label, labelledBy := landmarkLabel(props, "")
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
		label, labelledBy := landmarkLabel(props, "Main")
		return html.Nav(html.NavConfig{
			Label: label, LabelledBy: labelledBy,
			ID: propString(props, "id"), Class: propString(props, "class"),
			ExtraAttrs: extraAttrs(props, "id", "class", "label", "labelledby", "aria-label"),
		}, children...)
	case "aside":
		label, labelledBy := landmarkLabel(props, "Aside")
		return html.Aside(html.AsideConfig{
			Label: label, LabelledBy: labelledBy,
			ID: propString(props, "id"), Class: propString(props, "class"),
			ExtraAttrs: extraAttrs(props, "id", "class", "label", "labelledby", "aria-label"),
		}, children...)

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

	case "button":
		label := propString(props, "label")
		if label == "" {
			label = propString(props, "text")
		}
		attrs := extraAttrs(props, "id", "class", "label", "text", "type")
		if label == "" && attrs["aria-label"] == "" {
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
		content := render.Join(withTextProp(props, children)...)
		if propString(props, "href") == "" || (strings.TrimSpace(string(content)) == "" &&
			strings.TrimSpace(propString(props, "aria-label")) == "" && strings.TrimSpace(propString(props, "aria-labelledby")) == "") {
			return html.Span(textConfig(props), withTextProp(props, children)...)
		}
		return html.LinkHTML(html.LinkHTMLConfig{
			Href: propString(props, "href"), Content: content,
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
		if text != "" && len(children) == 0 && propString(props, "for") != "" {
			return html.Label(html.LabelConfig{
				For: propString(props, "for"), Text: text,
				ID: propString(props, "id"), Class: propString(props, "class"),
				ExtraAttrs: extraAttrs(props, "id", "class", "for", "text"),
			})
		}
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
		attrs := extraAttrs(props, "id", "class", "method", "action")
		if _, set := attrs["enctype"]; !set {
			if attrs == nil {
				attrs = map[string]string{}
			}
			attrs["enctype"] = "application/json"
		}
		return html.Form(html.FormConfig{
			Method: method, Action: propString(props, "action"),
			ID: propString(props, "id"), Class: propString(props, "class"),
			ExtraAttrs: attrs,
		}, children...)
	case "select":
		return render.Tag("select", attrsFromProps(props,
			"id", "class", "name", "required", "multiple",
		), children...)
	case "option":
		text := propString(props, "text")
		value := text
		if v, ok := props["value"]; ok && v != nil {
			value = propString(props, "value")
		}
		return html.Option(value, text, propBool(props, "selected"))
	case "textarea":
		name := propString(props, "name")
		if name == "" {
			name = "field"
		}
		content := textContentProp(props)
		return html.TextArea(html.TextAreaConfig{
			Name: name, Content: content,
			ID: propString(props, "id"), Class: propString(props, "class"),
			ExtraAttrs: extraAttrs(props, "id", "class", "name", "value"),
		})
	case "fieldset":
		if propString(props, "legend") == "" {
			return render.Tag("fieldset", attrsFromProps(props, "id", "class"), children...)
		}
		return html.FieldSet(html.FieldSetConfig{
			Legend: propString(props, "legend"),
			ID:     propString(props, "id"), Class: propString(props, "class"),
			ExtraAttrs: extraAttrs(props, "id", "class", "legend"),
		}, children...)

	case "image", "img":
		if propString(props, "src") == "" {
			return ""
		}
		return html.Image(html.ImageConfig{
			Src: propString(props, "src"), Alt: propString(props, "alt"),
			ID: propString(props, "id"), Class: propString(props, "class"),
			ExtraAttrs: extraAttrs(props, "id", "class", "src", "alt"),
		})

	case "list":
		ordered := propBool(props, "ordered")
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

	case "table":
		return html.Table(html.TableConfig{
			ID: propString(props, "id"), Class: propString(props, "class"),
			ExtraAttrs: extraAttrs(props, "id", "class", "role"),
		}, children...)
	case "thead":
		return html.Thead(tableSectionConfig(props), children...)
	case "tbody":
		return html.Tbody(tableSectionConfig(props), children...)
	case "tr":
		return html.TableRow(html.TableRowConfig{
			ID: propString(props, "id"), Class: propString(props, "class"),
			ExtraAttrs: extraAttrs(props, "id", "class", "role"),
		}, children...)
	case "th":
		return html.TH(html.THConfig{
			Scope: propString(props, "scope"),
			ID:    propString(props, "id"), Class: propString(props, "class"),
			ExtraAttrs: extraAttrs(props, "id", "class", "scope", "role", "text"),
		}, withTextProp(props, children)...)
	case "td":
		return html.TD(html.TDConfig{
			ID: propString(props, "id"), Class: propString(props, "class"),
			ExtraAttrs: extraAttrs(props, "id", "class", "role", "text"),
		}, withTextProp(props, children)...)

	default:
		return render.Raw(fmt.Sprintf("<!-- noderender: unknown kind %q -->", render.Escape(kind)))
	}
}

func textContentProp(props map[string]any) string {
	s := propString(props, "value")
	if s == "" {
		s = propString(props, "text")
	}
	if s == "" {
		s = propString(props, "content")
	}
	return s
}
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

func tableSectionConfig(props map[string]any) html.TableSectionConfig {
	return html.TableSectionConfig{
		ID: propString(props, "id"), Class: propString(props, "class"),
		ExtraAttrs: extraAttrs(props, "id", "class", "role"),
	}
}

func withTextProp(props map[string]any, children []render.HTML) []render.HTML {
	text := propString(props, "text")
	if text != "" {
		return append([]render.HTML{render.Text(text)}, children...)
	}
	return children
}

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

var allowedAttrs = map[string]bool{
	"id": true, "class": true, "title": true, "dir": true, "lang": true,
	"role": true, "slot": true, "hidden": true, "tabindex": true,
	"alt": true, "target": true, "rel": true, "type": true, "loading": true,
	"decoding": true, "width": true, "height": true, "download": true,
	"name": true, "value": true, "placeholder": true, "for": true,
	"method": true, "enctype": true, "autocomplete": true, "inputmode": true,
	"required": true, "disabled": true, "readonly": true, "checked": true,
	"selected": true, "multiple": true, "min": true, "max": true, "step": true,
	"minlength": true, "maxlength": true, "pattern": true, "rows": true, "cols": true,
	"colspan": true, "rowspan": true, "scope": true, "headers": true,
	"open": true,
}

var privilegedDataAttrs = map[string]bool{
	"data-behavior": true, "data-widget": true,
	"data-component": true, "data-bind": true,
	"data-island": true,
}

var privilegedDataPrefixes = []string{
	"data-cui-",
	//gofastr:allow(layerprefix) the privileged-prefix refusal names the framework prefix to bar it from node attrs
	"data-fui-",
}

func extraAttrs(props map[string]any, known ...string) html.Attrs {
	if len(props) == 0 {
		return nil
	}
	skip := make(map[string]struct{}, len(known))
	for _, k := range known {
		skip[asciicase.Lower(k)] = struct{}{}
	}
	out := html.Attrs{}
	for k, v := range props {
		if _, ok := skip[asciicase.Lower(k)]; ok {
			continue
		}
		if v == nil {
			continue
		}
		if !attrAllowed(k) {
			continue
		}
		if value, keep := attributeValue(k, v); keep {
			out[k] = value
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

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

func landmarkLabel(props map[string]any, fallback string) (label, labelledBy string) {
	label = propString(props, "label")
	if label == "" {
		label = propString(props, "aria-label")
	}
	labelledBy = propString(props, "labelledby")
	if label == "" && labelledBy == "" {
		label = fallback
	}
	return label, labelledBy
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
			if value, keep := attributeValue(k, v); keep {
				out[k] = value
			}
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func attributeValue(key string, value any) (string, bool) {
	if enabled, ok := value.(bool); ok {
		switch strings.ToLower(key) {
		case "hidden", "required", "disabled", "readonly", "checked", "selected", "multiple", "open", "download":
			return "", enabled
		}
	}
	return fmt.Sprint(value), true
}
