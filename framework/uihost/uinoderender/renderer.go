package uinoderender

import (
	"context"
	"fmt"

	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core-ui/uinodev1"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework/i18nui"
	ui "github.com/DonaldMurillo/gofastr/framework/ui"
)

// ActionResolver maps an ActionRef to the namespaced data-cui-rpc URL of
// an installed module route; ok is false when none is installed.
type ActionResolver func(actionRef string) (rpcURL string, ok bool)

// Renderer renders a validated ui.node.v1 tree as host-owned HTML; it
// implements [uinodev1.Renderer].
type Renderer struct {
	ctx     context.Context
	resolve ActionResolver
}

// New returns a Renderer that resolves ActionRefs through resolve and
// speaks ctx's language. A nil resolve fails every ActionRef.
func New(ctx context.Context, resolve ActionResolver) *Renderer {
	if ctx == nil {
		ctx = context.Background()
	}
	return &Renderer{ctx: ctx, resolve: resolve}
}

// Render renders a tree from [uinodev1.Validate]. It returns an error and
// no HTML when any node has no mapping or any ActionRef does not resolve.
func (r *Renderer) Render(t *uinodev1.Tree) (render.HTML, error) {
	if t == nil {
		return "", errNil("input tree")
	}
	return r.renderNode(t.Root)
}

func (r *Renderer) renderNode(n uinodev1.Node) (render.HTML, error) {
	switch n.Component {
	case uinodev1.CompStack:
		return r.renderStack(n)
	case uinodev1.CompCluster:
		return r.renderCluster(n)
	case uinodev1.CompGrid:
		return r.renderGrid(n)
	case uinodev1.CompSection:
		return r.renderSection(n)
	case uinodev1.CompCard:
		return r.renderCard(n)
	case uinodev1.CompDivider:
		return ui.Divider(ui.DividerConfig{}), nil
	case uinodev1.CompHeading:
		p := n.Props.(uinodev1.HeadingProps)
		return html.Heading(html.HeadingConfig{Level: p.Level}, render.Text(p.Text)), nil
	case uinodev1.CompParagraph:
		p := n.Props.(uinodev1.ParagraphProps)
		return html.Paragraph(html.TextConfig{}, render.Text(p.Text)), nil
	case uinodev1.CompText:
		p := n.Props.(uinodev1.TextProps)
		return html.Span(html.TextConfig{}, render.Text(p.Text)), nil
	case uinodev1.CompStrong:
		p := n.Props.(uinodev1.StrongProps)
		return html.Strong(html.TextConfig{}, render.Text(p.Text)), nil
	case uinodev1.CompEm:
		p := n.Props.(uinodev1.EmProps)
		return html.Em(html.TextConfig{}, render.Text(p.Text)), nil
	case uinodev1.CompCode:
		p := n.Props.(uinodev1.CodeProps)
		return html.Code(html.TextConfig{}, render.Text(p.Text)), nil
	case uinodev1.CompSmall:
		p := n.Props.(uinodev1.SmallProps)
		return html.Small(html.TextConfig{}, render.Text(p.Text)), nil
	case uinodev1.CompBadge:
		p := n.Props.(uinodev1.BadgeProps)
		return ui.StatusBadge(ui.StatusBadgeConfig{Label: p.Text, Variant: badgeTone(p.Tone)}), nil
	case uinodev1.CompDetailList:
		p := n.Props.(uinodev1.DetailListProps)
		return renderDetailList(p.Items), nil
	case uinodev1.CompKeyValue:
		p := n.Props.(uinodev1.KeyValueProps)
		return renderDetailList(kvItems(p.Items)), nil
	case uinodev1.CompStatCard:
		p := n.Props.(uinodev1.StatCardProps)
		return ui.StatCard(ui.StatCardConfig{
			Label:     p.Label,
			Value:     statValue(p.Value, p.Unit),
			Trend:     p.Trend,
			Direction: statDirection(p.Trend),
		}), nil
	case uinodev1.CompDataTable:
		return r.renderDataTable(n)
	case uinodev1.CompButton:
		return r.renderButton(n)
	case uinodev1.CompLink:
		return r.renderLink(n)
	case uinodev1.CompImage:
		p := n.Props.(uinodev1.ImageProps)
		return html.Image(html.ImageConfig{Src: p.Src, Alt: p.Alt}), nil
	}
	return "", errUnknownComponent(n.Component)
}

func (r *Renderer) renderStack(n uinodev1.Node) (render.HTML, error) {
	p := n.Props.(uinodev1.StackProps)
	children, err := r.renderChildren(n.Children)
	if err != nil {
		return "", err
	}
	if p.Direction == "horizontal" {
		return ui.Cluster(ui.ClusterConfig{
			Gap:    ui.Gap(p.Gap),
			Align:  ui.Align(p.Align),
			NoWrap: true,
		}, children...), nil
	}
	return ui.Stack(ui.StackConfig{
		Gap:   ui.Gap(p.Gap),
		Align: ui.Align(p.Align),
	}, children...), nil
}

func (r *Renderer) renderCluster(n uinodev1.Node) (render.HTML, error) {
	p := n.Props.(uinodev1.ClusterProps)
	children, err := r.renderChildren(n.Children)
	if err != nil {
		return "", err
	}
	return ui.Cluster(ui.ClusterConfig{
		Gap:   ui.Gap(p.Gap),
		Align: ui.Align(p.Align),
	}, children...), nil
}

func (r *Renderer) renderGrid(n uinodev1.Node) (render.HTML, error) {
	p := n.Props.(uinodev1.GridProps)
	children, err := r.renderChildren(n.Children)
	if err != nil {
		return "", err
	}
	return ui.Grid(ui.GridConfig{
		Min: gridMinForColumns(p.Columns),
		Gap: ui.Gap(p.Gap),
	}, children...), nil
}

func (r *Renderer) renderSection(n uinodev1.Node) (render.HTML, error) {
	p := n.Props.(uinodev1.SectionProps)
	children, err := r.renderChildren(n.Children)
	if err != nil {
		return "", err
	}
	label := p.Title
	if label == "" {
		label = p.Subtitle
	}
	if label == "" {
		label = i18nui.Section.Text(r.ctx)
	}
	parts := make([]render.HTML, 0, len(children)+1)
	if p.Title != "" {
		parts = append(parts, html.Heading(html.HeadingConfig{Level: 2}, render.Text(p.Title)))
	}
	if p.Subtitle != "" {
		parts = append(parts, html.Paragraph(html.TextConfig{}, render.Text(p.Subtitle)))
	}
	parts = append(parts, children...)
	return html.Section(html.SectionConfig{Label: label}, parts...), nil
}

func (r *Renderer) renderCard(n uinodev1.Node) (render.HTML, error) {
	p := n.Props.(uinodev1.CardProps)
	children, err := r.renderChildren(n.Children)
	if err != nil {
		return "", err
	}
	return ui.Card(ui.CardConfig{
		Heading: p.Title,
		Variant: cardVariant(p.Elevation),
	}, children...), nil
}

func (r *Renderer) renderChildren(nodes []uinodev1.Node) ([]render.HTML, error) {
	if len(nodes) == 0 {
		return nil, nil
	}
	out := make([]render.HTML, len(nodes))
	for i, c := range nodes {
		h, err := r.renderNode(c)
		if err != nil {
			return nil, err
		}
		out[i] = h
	}
	return out, nil
}

func renderDetailList(items []uinodev1.DetailItem) render.HTML {
	rows := make([]ui.DetailItem, len(items))
	for i, it := range items {
		rows[i] = ui.DetailItem{
			Label: it.Label,
			Value: render.Text(it.Value),
		}
	}
	return ui.DetailList(ui.DetailListConfig{Items: rows})
}

func kvItems(items []uinodev1.KeyValueItem) []uinodev1.DetailItem {
	out := make([]uinodev1.DetailItem, len(items))
	for i, it := range items {
		out[i] = uinodev1.DetailItem{Label: it.Key, Value: it.Value}
	}
	return out
}

func statValue(value, unit string) string {
	if unit == "" {
		return value
	}
	return value + " " + unit
}

func (r *Renderer) renderDataTable(n uinodev1.Node) (render.HTML, error) {
	p := n.Props.(uinodev1.DataTableProps)
	if len(p.Columns) == 0 {
		return ui.EmptyState(ui.EmptyStateConfig{
			Title:       pageNoColumns.Text(r.ctx),
			Description: pageNoColumnsBody.Text(r.ctx),
		}), nil
	}
	cols := make([]ui.Column, len(p.Columns))
	seen := make(map[string]bool, len(p.Columns))
	for i, c := range p.Columns {
		if c.Key == "" || seen[c.Key] {
			return "", &renderErr{op: "data-table", reason: fmt.Sprintf("column %d key %q is empty or repeated", i, c.Key)}
		}
		seen[c.Key] = true
		cols[i] = ui.Column{Key: c.Key, Header: c.Label}
	}
	rows := make([]ui.Row, len(p.Rows))
	for i, dr := range p.Rows {
		cells := make(map[string]render.HTML, len(cols))
		for j, cell := range dr.Cells {
			if j < len(cols) {
				cells[cols[j].Key] = render.Text(cell.Text)
			}
		}
		rows[i] = ui.Row{Cells: cells}
	}
	return ui.DataTable(ui.DataTableConfig{Columns: cols, Rows: rows, Ctx: r.ctx}), nil
}

func (r *Renderer) renderButton(n uinodev1.Node) (render.HTML, error) {
	p := n.Props.(uinodev1.ButtonProps)
	rpcURL, err := r.resolveActionRef(n.ActionRef)
	if err != nil {
		return "", err
	}
	variant, ok := ui.ParseButtonVariant(p.Variant)
	if !ok {
		variant = ui.ButtonPrimary
	}
	return ui.Button(ui.ButtonConfig{
		Label:   p.Label,
		Variant: variant,
		Ctx:     r.ctx,
		ExtraAttrs: html.Attrs{
			"data-cui-rpc": rpcURL,
		},
	}), nil
}

func (r *Renderer) renderLink(n uinodev1.Node) (render.HTML, error) {
	p := n.Props.(uinodev1.LinkProps)
	if p.To != "" {
		return ui.Link(ui.LinkConfig{Href: p.To, Text: p.Text}), nil
	}
	rpcURL, err := r.resolveActionRef(n.ActionRef)
	if err != nil {
		return "", err
	}
	return ui.Link(ui.LinkConfig{
		Href: rpcURL,
		Text: p.Text,
		ExtraAttrs: html.Attrs{
			"data-cui-rpc": rpcURL,
		},
	}), nil
}

// resolveActionRef resolves ref through the installed resolver; an
// unresolved ref is an error, never an improvised URL.
func (r *Renderer) resolveActionRef(ref string) (string, error) {
	if ref == "" {
		return "", errActionRef("empty action_ref")
	}
	if r.resolve == nil {
		return "", errActionRef("no ActionResolver wired")
	}
	url, ok := r.resolve(ref)
	if !ok || url == "" {
		return "", errActionRef("unresolved action_ref " + fmt.Sprintf("%q", ref))
	}
	return url, nil
}

func badgeTone(tone string) ui.StatusVariant {
	switch tone {
	case "positive":
		return ui.StatusSuccess
	case "negative":
		return ui.StatusDanger
	case "warning":
		return ui.StatusWarning
	case "info":
		return ui.StatusInfo
	default:
		return ui.StatusNeutral
	}
}

func cardVariant(elevation string) ui.CardVariant {
	switch elevation {
	case "flat":
		return ui.CardFlat
	default:
		return ui.CardElevated
	}
}

func statDirection(trend string) ui.TrendDirection {
	switch trend {
	case "up":
		return ui.TrendUp
	case "down":
		return ui.TrendDown
	default:
		return ui.TrendFlat
	}
}

func gridMinForColumns(columns int) string {
	switch {
	case columns <= 0:
		return ""
	case columns >= 6:
		return "8rem"
	default:
		return [...]string{"60rem", "28rem", "18rem", "13rem", "10rem"}[columns-1]
	}
}

type renderErr struct{ op, reason string }

func (e *renderErr) Error() string {
	return fmt.Sprintf("uinoderender: %s: %s", e.op, e.reason)
}

func errNil(what string) error { return &renderErr{op: "input", reason: what + " is nil"} }
func errActionRef(reason string) error {
	return &renderErr{op: "action_ref", reason: reason}
}
func errUnknownComponent(c uinodev1.Component) error {
	return &renderErr{op: "component", reason: fmt.Sprintf("no mapping for %q", string(c))}
}
