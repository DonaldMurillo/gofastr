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
			return renderLeaf(n)
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
			Href:         propString(n.Props, "href"), Variant: cardVariant(propString(n.Props, "variant")),
			ID: propString(n.Props, "id"),
		}, withTextFallback(children, propString(n.Props, "text"))...)
	case "link_button":
		label, href := propString(n.Props, "label", "text"), propString(n.Props, "href")
		if label == "" || href == "" {
			return renderLeaf(n)
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
			return renderLeaf(n)
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
		return renderLeaf(n)
	}
}

// renderLeaf keeps legacy journals renderable without letting them reintroduce
// app-local styling. Structural classes belong to typed design-system
// components; one-to-one semantic HTML nodes receive only their safe props.
func renderLeaf(n world.Node) render.HTML {
	if len(n.Props) > 0 {
		props := make(map[string]any, len(n.Props))
		for key, value := range n.Props {
			if strings.EqualFold(key, "class") {
				continue
			}
			props[key] = value
		}
		n.Props = props
	}
	return noderender.RenderNode(n)
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
