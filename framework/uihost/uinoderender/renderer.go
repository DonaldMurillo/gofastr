package uinoderender

import (
	"fmt"

	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core-ui/uinodev1"
	"github.com/DonaldMurillo/gofastr/core/render"
	ui "github.com/DonaldMurillo/gofastr/framework/ui"
)

// ActionResolver turns a ui.node.v1 ActionRef into the real namespaced
// data-fui-rpc URL the host assigned when installing the module's routes
// (resolve actionRef → route id → namespaced `/…/module/<name>/…` URL).
//
// ok=false means the ref does not resolve to an installed route. The
// renderer treats that as a hard failure (design §9): it returns an error
// and renders nothing for the node, never a dead or guessed URL. The
// module never sees or sets the URL — only the host does.
//
// A nil ActionResolver fails closed on every ActionRef.
type ActionResolver func(actionRef string) (rpcURL string, ok bool)

// Renderer maps a validated ui.node.v1 tree to host-owned HTML by
// composing framework/ui + core-ui/html design-system primitives. It
// implements [uinodev1.Renderer].
//
// Every id, class, ARIA attribute, visual variant, and data-fui-rpc URL
// is assigned HERE by the trusted mapping — the validated tree carries
// none of them (that is the whole point of the closed wire type).
type Renderer struct {
	resolve ActionResolver
}

// New returns a Renderer that uses resolve to turn ActionRef values into
// real data-fui-rpc URLs. A nil resolve is permitted: it fails closed on
// every ActionRef (every button / action_ref link renders as an error),
// which is the correct posture for a host that has not yet wired the
// module's route table.
func New(resolve ActionResolver) *Renderer {
	return &Renderer{resolve: resolve}
}

// Render converts a validated tree to HTML, satisfying
// [uinodev1.Renderer]. Callers MUST pass a tree produced by
// [uinodev1.Validate]; the renderer trusts that input's structure and
// fails closed only on the two host-owned concerns: unmapped shapes and
// unresolved ActionRefs.
func (r *Renderer) Render(t *uinodev1.Tree) (render.HTML, error) {
	if t == nil {
		return "", errNil("input tree")
	}
	return r.renderNode(t.Root)
}

// renderNode dispatches one node to its design-system primitive and
// recurses on children for layout components. It returns an error for
// any component it does not map (fail closed) and for any ActionRef it
// cannot resolve.
func (r *Renderer) renderNode(n uinodev1.Node) (render.HTML, error) {
	switch n.Component {
	// --- Layout (accept children) ---
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

	// --- Text (leaf) ---
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

	// --- Read-only data (leaf) ---
	case uinodev1.CompDetailList:
		p := n.Props.(uinodev1.DetailListProps)
		return renderDetailList(p.Items, false), nil
	case uinodev1.CompKeyValue:
		p := n.Props.(uinodev1.KeyValueProps)
		return renderDetailList(kvItems(p.Items), true), nil
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

	// --- Interactive (by reference only) ---
	case uinodev1.CompButton:
		return r.renderButton(n)
	case uinodev1.CompLink:
		return r.renderLink(n)

	// --- Media (leaf) ---
	case uinodev1.CompImage:
		p := n.Props.(uinodev1.ImageProps)
		return html.Image(html.ImageConfig{Src: p.Src, Alt: p.Alt}), nil
	}
	// The validator's closed enum makes this unreachable for a validated
	// tree. Fail closed anyway — never improvise markup for an unknown
	// component (the noderender debug-comment hole, design §9).
	return "", errUnknownComponent(n.Component)
}

// --- Layout renderers ----------------------------------------------------

func (r *Renderer) renderStack(n uinodev1.Node) (render.HTML, error) {
	p := n.Props.(uinodev1.StackProps)
	children, err := r.renderChildren(n.Children)
	if err != nil {
		return "", err
	}
	// ui.Stack is vertical-only; a horizontal stack maps to a non-wrapping
	// cluster (semantically a row that does not wrap, vs Cluster which
	// wraps by default). Both are the same ui-layout primitive family.
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
	// html.Section requires an a11y label (panics without one). The host
	// derives it from the typed Title prop, falling back to a neutral
	// label so a section without a title still announces itself.
	label := p.Title
	if label == "" {
		label = p.Subtitle
	}
	if label == "" {
		label = "Section"
	}
	parts := make([]render.HTML, 0, len(children)+1)
	if p.Title != "" {
		// Visible heading inside the landmark so sighted users see the
		// name too; the aria-label above carries the SR name.
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

// renderChildren fans out to every child node. A single child error
// rejects the whole render — the tree is all-or-nothing (design §9).
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

// --- Data renderers ------------------------------------------------------

// renderDetailList maps both detail-list and key-value to the framework's
// <dl>-based DetailList primitive. There is no distinct KV component in
// the design system; a flat key/value map is semantically a description
// list of label/value rows (design §9 lists both; this is the honest
// 1:1 primitive that exists).
func renderDetailList(items []uinodev1.DetailItem, _ bool) render.HTML {
	rows := make([]ui.DetailItem, len(items))
	for i, it := range items {
		rows[i] = ui.DetailItem{
			Label: it.Label,
			Value: render.Text(it.Value),
		}
	}
	return ui.DetailList(ui.DetailListConfig{Items: rows})
}

// kvItems adapts KeyValueItem (Key/Value) to DetailItem (Label/Value) so
// the key-value component reuses the same DetailList primitive.
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

// renderDataTable maps a read-only data-table. PANIC-SAFE: the framework's
// ui.DataTable panics on empty Columns (datatable.go:154). The validator
// already rejects empty Columns, but this is a defensive guard so a
// hand-built (unvalidated) Tree can never crash the renderer — it renders
// an empty-state instead. Empty Rows are handled by ui.DataTable itself
// (it already composes EmptyState for zero rows).
func (r *Renderer) renderDataTable(n uinodev1.Node) (render.HTML, error) {
	p := n.Props.(uinodev1.DataTableProps)
	if len(p.Columns) == 0 {
		return ui.EmptyState(ui.EmptyStateConfig{
			Title:       "No columns",
			Description: "This table has no columns to display.",
		}), nil
	}
	cols := make([]ui.Column, len(p.Columns))
	for i, c := range p.Columns {
		cols[i] = ui.Column{Key: c.Key, Header: c.Label}
	}
	rows := make([]ui.Row, len(p.Rows))
	for i, dr := range p.Rows {
		cells := make(map[string]render.HTML, len(cols))
		// Map ordered cells onto column keys by index. Extra cells beyond
		// the column count are dropped; missing cells render empty (the
		// framework's Row.Cells is a key→HTML map, so absent keys are
		// simply not present).
		for j, cell := range dr.Cells {
			if j < len(cols) {
				cells[cols[j].Key] = render.Text(cell.Text)
			}
		}
		rows[i] = ui.Row{Cells: cells}
	}
	return ui.DataTable(ui.DataTableConfig{Columns: cols, Rows: rows}), nil
}

// --- Interactive renderers ----------------------------------------------

func (r *Renderer) renderButton(n uinodev1.Node) (render.HTML, error) {
	p := n.Props.(uinodev1.ButtonProps)
	rpcURL, err := r.resolveActionRef(n.ActionRef)
	if err != nil {
		return "", err
	}
	return ui.Button(ui.ButtonConfig{
		Label:   p.Label,
		Variant: buttonVariant(p.Variant),
		ExtraAttrs: html.Attrs{
			"data-fui-rpc": rpcURL,
		},
	}), nil
}

func (r *Renderer) renderLink(n uinodev1.Node) (render.HTML, error) {
	p := n.Props.(uinodev1.LinkProps)
	if p.To != "" {
		// Pure navigation: host-relative href. The validator already
		// guaranteed To is a same-origin path; ui.Link scrubs it again.
		return ui.Link(ui.LinkConfig{Href: p.To, Text: p.Text}), nil
	}
	// ActionRef link: resolve to the real URL and emit data-fui-rpc so
	// the runtime upgrades the click to an island RPC (with JS) while
	// the href remains the graceful no-JS fallback to the same URL.
	rpcURL, err := r.resolveActionRef(n.ActionRef)
	if err != nil {
		return "", err
	}
	return ui.Link(ui.LinkConfig{
		Href: rpcURL,
		Text: p.Text,
		ExtraAttrs: html.Attrs{
			"data-fui-rpc": rpcURL,
		},
	}), nil
}

// resolveActionRef resolves an ActionRef via the injected seam. An empty
// ref or an unresolvable ref is a hard error — the renderer never emits a
// dead or guessed URL. A nil resolver fails closed on every ref.
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

// --- Variant / tone mapping (host-assigned from typed props) ------------

// badgeTone maps the wire tone enum to the design system's StatusVariant.
// The wire names are deliberately distinct from the framework's so a
// future wire rename does not collide; the mapping lives here, in the
// trusted host layer.
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
	default: // "" | "neutral"
		return ui.StatusNeutral
	}
}

func buttonVariant(v string) ui.ButtonVariant {
	switch v {
	case "secondary":
		return ui.ButtonSecondary
	case "ghost":
		return ui.ButtonGhost
	case "danger":
		return ui.ButtonDanger
	default: // "" | "primary"
		return ui.ButtonPrimary
	}
}

func cardVariant(elevation string) ui.CardVariant {
	switch elevation {
	case "flat":
		return ui.CardFlat
	default: // "" | "low" | "high" — closest is the shadowed elevated card
		return ui.CardElevated
	}
}

func statDirection(trend string) ui.TrendDirection {
	switch trend {
	case "up":
		return ui.TrendUp
	case "down":
		return ui.TrendDown
	default: // "" | "flat"
		return ui.TrendFlat
	}
}

// gridMinForColumns approximates the requested fixed column count using
// the auto-fit Grid primitive's Min knob (passed via the --ui-grid-min
// custom property, CSP-clean — no inline style). The design system's Grid
// is auto-fit only; a true fixed-column-count grid would be an upstream
// gap to fill. 0 (unset) defers to the framework default.
func gridMinForColumns(columns int) string {
	switch {
	case columns <= 0:
		return ""
	case columns >= 6:
		return "8rem"
	default:
		// 1→60rem, 2→28rem, 3→18rem, 4→13rem, 5→10rem
		return [...]string{"60rem", "28rem", "18rem", "13rem", "10rem"}[columns-1]
	}
}

// --- Errors --------------------------------------------------------------

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
