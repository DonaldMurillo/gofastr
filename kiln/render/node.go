package render

import (
	"fmt"
	"strings"

	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework/ui"
	"github.com/DonaldMurillo/gofastr/kiln/noderender"
	"github.com/DonaldMurillo/gofastr/kiln/world"
)

// RenderNode renders current design-system component kinds directly and falls
// back to the leaf noderender package for one-to-one semantic HTML nodes. This
// keeps Kiln's live preview on the same component/CSS surface as generated Go
// without pulling the authoring engine into generated apps.
func RenderNode(n world.Node) render.HTML {
	children := renderChildren(n.Children)
	switch strings.ToLower(strings.TrimSpace(n.Kind)) {
	case "page_header":
		title := propString(n.Props, "title", "text")
		if title == "" {
			return renderLeaf(n, children)
		}
		return ui.PageHeader(ui.PageHeaderConfig{
			Title: title, Subtitle: propString(n.Props, "subtitle", "description"),
			Eyebrow: propString(n.Props, "eyebrow"), Actions: render.Join(children...),
			HeadingLevel: propInt(n.Props, "heading_level", "level"), ID: propString(n.Props, "id"),
		})
	case "hero":
		return ui.Hero(ui.HeroConfig{
			Title:    propString(n.Props, "title", "text"),
			Subtitle: propString(n.Props, "subtitle", "description"),
			Eyebrow:  propString(n.Props, "eyebrow"), Actions: children,
		})
	case "section":
		return ui.Section(ui.SectionConfig{
			Heading:     propString(n.Props, "heading", "title"),
			Description: propString(n.Props, "description", "subtitle"),
			Eyebrow:     propString(n.Props, "eyebrow"), Label: propString(n.Props, "label"),
			ID: propString(n.Props, "id"),
		}, children...)
	case "card":
		return ui.Card(ui.CardConfig{
			Heading:      propString(n.Props, "heading", "title"),
			Description:  propString(n.Props, "description", "subtitle"),
			HeadingLevel: propInt(n.Props, "heading_level", "level"),
			Href:         safeHref(propString(n.Props, "href")), Variant: cardVariant(propString(n.Props, "variant")),
			ID: propString(n.Props, "id"),
		}, withTextFallback(children, propString(n.Props, "text"))...)
	case "link_button":
		label, href := propString(n.Props, "label", "text"), safeHref(propString(n.Props, "href"))
		if label == "" || href == "" {
			return renderLeaf(n, children)
		}
		return ui.LinkButton(ui.LinkButtonConfig{
			Label: label, Href: href, Variant: buttonVariant(propString(n.Props, "variant")),
			Size: buttonSize(propString(n.Props, "size")), External: propBool(n.Props, "external"),
			ID: propString(n.Props, "id"),
		})
	case "callout":
		return ui.Callout(ui.CalloutConfig{
			Title: propString(n.Props, "title"), Variant: statusVariant(propString(n.Props, "variant", "status")),
			ID: propString(n.Props, "id"),
		}, withTextFallback(children, propString(n.Props, "text", "description"))...)
	case "stat_card":
		label, value := propString(n.Props, "label"), propString(n.Props, "value")
		if label == "" || value == "" {
			return renderLeaf(n, children)
		}
		return ui.StatCard(ui.StatCardConfig{
			Label: label, Value: value, Trend: propString(n.Props, "trend"),
			Direction: trendDirection(propString(n.Props, "direction")), ID: propString(n.Props, "id"),
		})
	case "stat_row", "stat_grid":
		return ui.Grid(ui.GridConfig{Min: "12rem", Gap: gap(propString(n.Props, "gap")), ID: propString(n.Props, "id")}, children...)
	case "stack":
		return ui.Stack(ui.StackConfig{
			Gap: gap(propString(n.Props, "gap")), Align: align(propString(n.Props, "align")),
			Justify: justify(propString(n.Props, "justify")), ID: propString(n.Props, "id"),
		}, children...)
	case "cluster":
		return ui.Cluster(ui.ClusterConfig{
			Gap: gap(propString(n.Props, "gap")), Align: align(propString(n.Props, "align")),
			Justify: justify(propString(n.Props, "justify")), NoWrap: propBool(n.Props, "no_wrap"),
			ID: propString(n.Props, "id"),
		}, children...)
	case "grid":
		return ui.Grid(ui.GridConfig{
			Min: propString(n.Props, "min"), Gap: gap(propString(n.Props, "gap")), ID: propString(n.Props, "id"),
		}, children...)
	case "divider":
		orientation := ui.DividerHorizontal
		if propString(n.Props, "orientation") == "vertical" {
			orientation = ui.DividerVertical
		}
		return ui.Divider(ui.DividerConfig{
			Label: propString(n.Props, "label", "text"), Orientation: orientation, ID: propString(n.Props, "id"),
		})
	default:
		return renderLeaf(n, children)
	}
}

// renderLeaf keeps legacy journals renderable without letting them reintroduce
// app-local styling. Structural classes belong to typed design-system
// components; one-to-one semantic HTML nodes receive only their safe props.
// It renders just this node's element around the already-kiln-rendered
// children, so typed design-system kinds nested inside a semantic container
// still dispatch through their components (and the class strip applies at
// every depth, since every node passes through here).
func renderLeaf(n world.Node, children []render.HTML) render.HTML {
	props := n.Props
	if len(props) > 0 {
		props = make(map[string]any, len(n.Props))
		for key, value := range n.Props {
			// Structural styling belongs to the typed design-system kinds.
			if strings.EqualFold(key, "class") {
				continue
			}
			if strings.EqualFold(strings.TrimSpace(key), "data-kiln-tool") {
				name, _ := value.(string)
				if !safeToolName(name) {
					continue
				}
			}
			props[key] = value
		}
	}
	return noderender.RenderKind(n.Kind, props, children)
}

// safeToolName bounds the value of data-kiln-tool to a bare tool
// identifier.
//
// The runtime's delegator builds `fetch('/kiln/tool/' + attr)` with no
// encodeURIComponent (core-ui/runtime frag/rpc.js), and dispatch is gated
// only on a data-fui-trusted ancestor — which worldScreen.Render puts around
// the entire agent-authored tree. core-ui/noderender allows data-kiln-tool
// through its data-* rule on the stated grounds that "the delegator
// additionally requires a data-fui-trusted ancestor, which this IR cannot
// produce" (noderender_security_test.go); in kiln's only render path the IR
// always produces one, so that justification does not hold here and the
// value has to be checked at ingestion.
//
// Unchecked, a tool name of "../../api/posts" has its dot-segments removed
// by the URL parser and the click becomes a same-origin POST to /api/posts —
// carrying the operator's cookies and the page's CSRF token — against routes
// the kiln tool API does not otherwise expose. "x?y=z" and "x#f" inject a
// query and a fragment the same way.
//
// A real kiln tool name is a lowercase identifier (chat, add_page,
// update_page_element), so anything else is dropped rather than rendered.
// The attribute itself stays supported: TestBrowser_ButtonToolCallFires
// pins that an agent-authored button naming a genuine tool still fires it.
func safeToolName(name string) bool {
	if name == "" || len(name) > 64 {
		return false
	}
	for i := 0; i < len(name); i++ {
		c := name[i]
		switch {
		case c >= 'a' && c <= 'z', c >= '0' && c <= '9':
		case c == '_' && i > 0:
		default:
			return false
		}
	}
	return true
}

// safeHref filters an agent-authored href before it reaches a design-system
// component. The components disagree on what to do with a hostile scheme —
// ui.Card drops to "#" while ui.LinkButton panics (framework/ui:
// components.go) — and a panic on the typed path 500s the page on every
// request until the IR is edited. Both contracts are right for a Go author
// passing a literal; neither is right for request-authored IR, so the
// scheme decision happens here instead. Mirrors the degrade-don't-panic
// choice core-ui/noderender already made for the leaf path.
func safeHref(href string) string {
	trimmed := strings.TrimSpace(href)
	if trimmed == "" {
		return ""
	}
	// Strip the C0 range + DEL before testing: a browser ignores them when
	// resolving a scheme, so "\tjava\nscript:" is still javascript:.
	var b strings.Builder
	for _, r := range trimmed {
		if r <= 0x20 || r == 0x7f {
			continue
		}
		b.WriteRune(r)
	}
	clean := strings.ToLower(b.String())
	scheme, _, hasScheme := strings.Cut(clean, ":")
	if !hasScheme || strings.ContainsAny(scheme, "/?#") {
		return trimmed // relative, absolute-path, query, or fragment
	}
	switch scheme {
	case "http", "https", "mailto", "tel":
		return trimmed
	}
	return ""
}

func renderChildren(nodes []world.Node) []render.HTML {
	out := make([]render.HTML, 0, len(nodes))
	for _, child := range nodes {
		out = append(out, RenderNode(child))
	}
	return out
}

func withTextFallback(children []render.HTML, text string) []render.HTML {
	if len(children) == 0 && text != "" {
		return []render.HTML{render.Text(text)}
	}
	return children
}

func propString(props map[string]any, keys ...string) string {
	for _, key := range keys {
		if value, ok := props[key]; ok && value != nil {
			switch v := value.(type) {
			case string:
				if v != "" {
					return v
				}
			default:
				return fmt.Sprint(v)
			}
		}
	}
	return ""
}

func propInt(props map[string]any, keys ...string) int {
	for _, key := range keys {
		switch value := props[key].(type) {
		case int:
			return value
		case int64:
			return int(value)
		case float64:
			return int(value)
		}
	}
	return 0
}

func propBool(props map[string]any, key string) bool {
	value, _ := props[key].(bool)
	return value
}

func buttonVariant(value string) ui.ButtonVariant {
	switch value {
	case "secondary":
		return ui.ButtonSecondary
	case "danger":
		return ui.ButtonDanger
	case "ghost":
		return ui.ButtonGhost
	default:
		return ui.ButtonPrimary
	}
}

func buttonSize(value string) ui.ButtonSize {
	switch value {
	case "small":
		return ui.ButtonSizeSmall
	case "large":
		return ui.ButtonSizeLarge
	default:
		return ui.ButtonSizeDefault
	}
}

func cardVariant(value string) ui.CardVariant {
	switch value {
	case "outlined":
		return ui.CardOutlined
	case "flat":
		return ui.CardFlat
	default:
		return ui.CardElevated
	}
}

func statusVariant(value string) ui.StatusVariant {
	switch value {
	case "success":
		return ui.StatusSuccess
	case "warning":
		return ui.StatusWarning
	case "danger":
		return ui.StatusDanger
	case "neutral":
		return ui.StatusNeutral
	default:
		return ui.StatusInfo
	}
}

func trendDirection(value string) ui.TrendDirection {
	switch value {
	case "up":
		return ui.TrendUp
	case "down":
		return ui.TrendDown
	default:
		return ui.TrendFlat
	}
}

func gap(value string) ui.Gap {
	switch value {
	case "none":
		return ui.GapNone
	case "xs":
		return ui.GapXS
	case "sm":
		return ui.GapSM
	case "lg":
		return ui.GapLG
	case "xl":
		return ui.GapXL
	case "2xl":
		return ui.Gap2XL
	default:
		return ui.GapMD
	}
}

func align(value string) ui.Align {
	switch value {
	case "start":
		return ui.AlignStart
	case "center":
		return ui.AlignCenter
	case "end":
		return ui.AlignEnd
	case "baseline":
		return ui.AlignBaseline
	default:
		return ui.AlignStretch
	}
}

func justify(value string) ui.Justify {
	switch value {
	case "center":
		return ui.JustifyCenter
	case "end":
		return ui.JustifyEnd
	case "between":
		return ui.JustifyBetween
	case "around":
		return ui.JustifyAround
	default:
		return ui.JustifyStart
	}
}
