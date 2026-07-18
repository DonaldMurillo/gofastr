package uinodev1

// Component is the closed allowlist of semantic components a ui.node.v1
// tree may reference. Anything not in this enum is whole-tree rejected at
// decode time (design §9). The string values are the canonical wire names
// (lowercase, hyphenated); case-variants and typos do NOT match.
type Component string

const (
	// Layout components — accept children.
	CompStack   Component = "stack"
	CompCluster Component = "cluster"
	CompGrid    Component = "grid"
	CompSection Component = "section"
	CompCard    Component = "card"
	CompDivider Component = "divider"

	// Text components — no children (text comes via props).
	CompHeading   Component = "heading"
	CompParagraph Component = "paragraph"
	CompText      Component = "text"
	CompStrong    Component = "strong"
	CompEm        Component = "em"
	CompCode      Component = "code"
	CompSmall     Component = "small"
	CompBadge     Component = "badge"

	// Read-only data components — no children.
	CompDetailList Component = "detail-list"
	CompKeyValue   Component = "key-value"
	CompStatCard   Component = "stat-card"
	CompDataTable  Component = "data-table"

	// Interactive components — by reference only.
	// A button carries an ActionRef at the Node level.
	// A link carries either To (host-relative) in Props or an ActionRef.
	CompButton Component = "button"
	CompLink   Component = "link"

	// Media component — no children.
	// Src is a host-relative same-origin path (URL-guarded); Alt is
	// required non-empty so every module image is accessible.
	CompImage Component = "image"
)

// Node is a single element in a validated ui.node.v1 tree.
//
// Props is a sealed union — only the concrete prop types in this file
// implement it. There is no escape hatch: a Bindings/Actions/free-bag
// node is unrepresentable, and any unknown JSON field rejects the whole
// tree (see [Validate]).
type Node struct {
	// Component is the closed enum value (one of the Comp* constants).
	Component Component
	// Props is the typed, per-component prop struct. Never nil for a
	// decoded node — a component with no fields uses its zero-value
	// struct (e.g. DividerProps{}).
	Props Props
	// Children is the optional list of child nodes. Only layout
	// components accept children; the validator rejects children on
	// text / data / interactive components.
	Children []Node
	// ActionRef is an opaque reference to an action/route the host
	// renderer resolves against the module's installed routes. Required
	// on button; optional-but-mutually-exclusive with Props.To on link.
	// Empty on every other component. The validator checks only its
	// shape here; resolution is the host renderer's job.
	ActionRef string
}

// Tree is the validated root of a ui.node.v1 document. Returned by
// [Validate] only when every node, prop, URL, and cap check has passed.
type Tree struct {
	Root Node
}

// Props is the sealed union of per-component prop structs. Every concrete
// implementation lives in this file; external packages cannot add new
// prop types. Each implementation's validate method enforces per-component
// invariants (URL scheme, range bounds, enum values, child policy).
type Props interface {
	propsMarker()
	// validate enforces per-component invariants against the given limits.
	// String fields are checked against Limits.MaxPropString; URLs against
	// the host-relative guard; ranges against their documented bounds.
	validate(lim Limits) error
	// childPolicy reports whether this component accepts child nodes.
	childPolicy() childPolicy
	// estimatedTextSize returns a conservative upper bound on the text
	// bytes this node contributes, for the total-text cap.
	estimatedTextSize() int
}

type childPolicy int

const (
	childPolicyNone childPolicy = iota // no children allowed
	childPolicyAny                     // any number up to MaxChildrenPerNode
)

// --- Layout props --------------------------------------------------------

// StackProps configures a vertical-or-horizontal stack (flex column/row).
type StackProps struct {
	Direction string `json:"direction,omitempty"` // "horizontal" | "vertical" (default vertical)
	Gap       string `json:"gap,omitempty"`       // free-form token-ish string, bounded
	Align     string `json:"align,omitempty"`     // "start" | "center" | "end" | "stretch"
}

func (StackProps) propsMarker()               {}
func (p StackProps) childPolicy() childPolicy { return childPolicyAny }
func (p StackProps) validate(lim Limits) error {
	if p.Direction != "" && p.Direction != "horizontal" && p.Direction != "vertical" {
		return errBadEnum("stack.direction", p.Direction)
	}
	if p.Align != "" && !isAlignValue(p.Align) {
		return errBadEnum("stack.align", p.Align)
	}
	return checkStrings(lim, "stack", p.Gap)
}
func (p StackProps) estimatedTextSize() int { return len(p.Gap) + len(p.Align) + len(p.Direction) }

// ClusterProps configures a wrapping cluster (flex wrap).
type ClusterProps struct {
	Gap   string `json:"gap,omitempty"`
	Align string `json:"align,omitempty"`
}

func (ClusterProps) propsMarker()               {}
func (p ClusterProps) childPolicy() childPolicy { return childPolicyAny }
func (p ClusterProps) validate(lim Limits) error {
	if p.Align != "" && !isAlignValue(p.Align) {
		return errBadEnum("cluster.align", p.Align)
	}
	return checkStrings(lim, "cluster", p.Gap)
}
func (p ClusterProps) estimatedTextSize() int { return len(p.Gap) + len(p.Align) }

// GridProps configures a column grid.
type GridProps struct {
	Columns int    `json:"columns,omitempty"` // 1..12; 0 = host default
	Gap     string `json:"gap,omitempty"`
}

func (GridProps) propsMarker()               {}
func (p GridProps) childPolicy() childPolicy { return childPolicyAny }
func (p GridProps) validate(lim Limits) error {
	if p.Columns < 0 || p.Columns > 12 {
		return errBadRange("grid.columns", p.Columns, 0, 12)
	}
	return checkStrings(lim, "grid", p.Gap)
}
func (p GridProps) estimatedTextSize() int { return len(p.Gap) }

// SectionProps configures a titled section landmark.
type SectionProps struct {
	Title    string `json:"title,omitempty"`
	Subtitle string `json:"subtitle,omitempty"`
}

func (SectionProps) propsMarker()               {}
func (p SectionProps) childPolicy() childPolicy { return childPolicyAny }
func (p SectionProps) validate(lim Limits) error {
	return checkStrings(lim, "section", p.Title, p.Subtitle)
}
func (p SectionProps) estimatedTextSize() int { return len(p.Title) + len(p.Subtitle) }

// CardProps configures a card surface.
type CardProps struct {
	Title     string `json:"title,omitempty"`
	Elevation string `json:"elevation,omitempty"` // "flat" | "low" | "high"
}

func (CardProps) propsMarker()               {}
func (p CardProps) childPolicy() childPolicy { return childPolicyAny }
func (p CardProps) validate(lim Limits) error {
	if p.Elevation != "" && p.Elevation != "flat" && p.Elevation != "low" && p.Elevation != "high" {
		return errBadEnum("card.elevation", p.Elevation)
	}
	return checkStrings(lim, "card", p.Title)
}
func (p CardProps) estimatedTextSize() int { return len(p.Title) + len(p.Elevation) }

// DividerProps has no fields — a divider is purely structural.
type DividerProps struct{}

func (DividerProps) propsMarker()             {}
func (DividerProps) childPolicy() childPolicy { return childPolicyNone }
func (DividerProps) validate(Limits) error    { return nil }
func (DividerProps) estimatedTextSize() int   { return 0 }

// --- Text props ----------------------------------------------------------

// HeadingProps configures a heading. Level (1–6) and Text are required.
type HeadingProps struct {
	Level int    `json:"level"` // 1..6, required
	Text  string `json:"text"`  // required
}

func (HeadingProps) propsMarker()             {}
func (HeadingProps) childPolicy() childPolicy { return childPolicyNone }
func (p HeadingProps) validate(lim Limits) error {
	if p.Level < 1 || p.Level > 6 {
		return errBadRange("heading.level", p.Level, 1, 6)
	}
	if p.Text == "" {
		return errRequired("heading.text")
	}
	return checkStrings(lim, "heading", p.Text)
}
func (p HeadingProps) estimatedTextSize() int { return len(p.Text) }

// ParagraphProps is a paragraph body.
type ParagraphProps struct {
	Text string `json:"text"`
}

func (ParagraphProps) propsMarker()             {}
func (ParagraphProps) childPolicy() childPolicy { return childPolicyNone }
func (p ParagraphProps) validate(lim Limits) error {
	if p.Text == "" {
		return errRequired("paragraph.text")
	}
	return checkStrings(lim, "paragraph", p.Text)
}
func (p ParagraphProps) estimatedTextSize() int { return len(p.Text) }

// TextProps is bare inline/block text.
type TextProps struct {
	Text string `json:"text"`
}

func (TextProps) propsMarker()             {}
func (TextProps) childPolicy() childPolicy { return childPolicyNone }
func (p TextProps) validate(lim Limits) error {
	if p.Text == "" {
		return errRequired("text.text")
	}
	return checkStrings(lim, "text", p.Text)
}
func (p TextProps) estimatedTextSize() int { return len(p.Text) }

// StrongProps is strongly-emphasized text.
type StrongProps struct {
	Text string `json:"text"`
}

func (StrongProps) propsMarker()             {}
func (StrongProps) childPolicy() childPolicy { return childPolicyNone }
func (p StrongProps) validate(lim Limits) error {
	if p.Text == "" {
		return errRequired("strong.text")
	}
	return checkStrings(lim, "strong", p.Text)
}
func (p StrongProps) estimatedTextSize() int { return len(p.Text) }

// EmProps is emphasized text.
type EmProps struct {
	Text string `json:"text"`
}

func (EmProps) propsMarker()             {}
func (EmProps) childPolicy() childPolicy { return childPolicyNone }
func (p EmProps) validate(lim Limits) error {
	if p.Text == "" {
		return errRequired("em.text")
	}
	return checkStrings(lim, "em", p.Text)
}
func (p EmProps) estimatedTextSize() int { return len(p.Text) }

// CodeProps is inline or block code.
type CodeProps struct {
	Text string `json:"text"`
}

func (CodeProps) propsMarker()             {}
func (CodeProps) childPolicy() childPolicy { return childPolicyNone }
func (p CodeProps) validate(lim Limits) error {
	if p.Text == "" {
		return errRequired("code.text")
	}
	return checkStrings(lim, "code", p.Text)
}
func (p CodeProps) estimatedTextSize() int { return len(p.Text) }

// SmallProps is fine-print text.
type SmallProps struct {
	Text string `json:"text"`
}

func (SmallProps) propsMarker()             {}
func (SmallProps) childPolicy() childPolicy { return childPolicyNone }
func (p SmallProps) validate(lim Limits) error {
	if p.Text == "" {
		return errRequired("small.text")
	}
	return checkStrings(lim, "small", p.Text)
}
func (p SmallProps) estimatedTextSize() int { return len(p.Text) }

// BadgeProps is a small status label.
type BadgeProps struct {
	Text string `json:"text"`
	Tone string `json:"tone,omitempty"` // neutral | positive | negative | warning | info
}

func (BadgeProps) propsMarker()             {}
func (BadgeProps) childPolicy() childPolicy { return childPolicyNone }
func (p BadgeProps) validate(lim Limits) error {
	if p.Text == "" {
		return errRequired("badge.text")
	}
	if p.Tone != "" && !isToneValue(p.Tone) {
		return errBadEnum("badge.tone", p.Tone)
	}
	return checkStrings(lim, "badge", p.Text)
}
func (p BadgeProps) estimatedTextSize() int { return len(p.Text) + len(p.Tone) }

// --- Data props ----------------------------------------------------------

// DetailItem is one row of a detail-list.
type DetailItem struct {
	Label string `json:"label"`
	Value string `json:"value"`
}

// DetailListProps is a label/value list.
type DetailListProps struct {
	Items []DetailItem `json:"items"`
}

func (DetailListProps) propsMarker()             {}
func (DetailListProps) childPolicy() childPolicy { return childPolicyNone }
func (p DetailListProps) validate(lim Limits) error {
	if len(p.Items) == 0 {
		return errRequired("detail-list.items")
	}
	for i, it := range p.Items {
		if it.Label == "" || it.Value == "" {
			return errRequiredIndex("detail-list.items", i)
		}
		if err := checkStrings(lim, "detail-list", it.Label, it.Value); err != nil {
			return err
		}
	}
	return nil
}
func (p DetailListProps) estimatedTextSize() int {
	n := 0
	for _, it := range p.Items {
		n += len(it.Label) + len(it.Value)
	}
	return n
}

// KeyValueItem is one entry of a key-value list.
type KeyValueItem struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

// KeyValueProps is a flat key/value map rendered as a list.
type KeyValueProps struct {
	Items []KeyValueItem `json:"items"`
}

func (KeyValueProps) propsMarker()             {}
func (KeyValueProps) childPolicy() childPolicy { return childPolicyNone }
func (p KeyValueProps) validate(lim Limits) error {
	if len(p.Items) == 0 {
		return errRequired("key-value.items")
	}
	for i, it := range p.Items {
		if it.Key == "" || it.Value == "" {
			return errRequiredIndex("key-value.items", i)
		}
		if err := checkStrings(lim, "key-value", it.Key, it.Value); err != nil {
			return err
		}
	}
	return nil
}
func (p KeyValueProps) estimatedTextSize() int {
	n := 0
	for _, it := range p.Items {
		n += len(it.Key) + len(it.Value)
	}
	return n
}

// StatCardProps is a single labeled metric.
type StatCardProps struct {
	Label string `json:"label"`
	Value string `json:"value"`
	Unit  string `json:"unit,omitempty"`
	Trend string `json:"trend,omitempty"` // up | down | flat
}

func (StatCardProps) propsMarker()             {}
func (StatCardProps) childPolicy() childPolicy { return childPolicyNone }
func (p StatCardProps) validate(lim Limits) error {
	if p.Label == "" || p.Value == "" {
		return errRequired("stat-card.label/value")
	}
	if p.Trend != "" && p.Trend != "up" && p.Trend != "down" && p.Trend != "flat" {
		return errBadEnum("stat-card.trend", p.Trend)
	}
	return checkStrings(lim, "stat-card", p.Label, p.Value, p.Unit)
}
func (p StatCardProps) estimatedTextSize() int {
	return len(p.Label) + len(p.Value) + len(p.Unit) + len(p.Trend)
}

// DataColumn is one column definition for a data-table.
type DataColumn struct {
	Key   string `json:"key"`
	Label string `json:"label"`
}

// DataCell is one scalar cell in a data-table row.
type DataCell struct {
	Text string `json:"text"`
}

// DataRow is one row of cells in a data-table. The validator does NOT
// enforce cell-count == column-count here; the host renderer is expected
// to handle short/long rows defensively. (v1 minimal: shape only.)
type DataRow struct {
	Cells []DataCell `json:"cells"`
}

// DataTableProps configures a read-only table.
type DataTableProps struct {
	Columns []DataColumn `json:"columns"`
	Rows    []DataRow    `json:"rows"`
}

func (DataTableProps) propsMarker()             {}
func (DataTableProps) childPolicy() childPolicy { return childPolicyNone }
func (p DataTableProps) validate(lim Limits) error {
	if len(p.Columns) == 0 {
		return errRequired("data-table.columns")
	}
	for i, c := range p.Columns {
		if c.Key == "" || c.Label == "" {
			return errRequiredIndex("data-table.columns", i)
		}
		if err := checkStrings(lim, "data-table.column", c.Key, c.Label); err != nil {
			return err
		}
	}
	for i, r := range p.Rows {
		if len(r.Cells) == 0 {
			return errRequiredIndex("data-table.rows.cells", i)
		}
		for j, cell := range r.Cells {
			if err := checkStrings(lim, "data-table.cell", cell.Text); err != nil {
				_ = j
				return err
			}
		}
	}
	return nil
}
func (p DataTableProps) estimatedTextSize() int {
	n := 0
	for _, c := range p.Columns {
		n += len(c.Key) + len(c.Label)
	}
	for _, r := range p.Rows {
		for _, cell := range r.Cells {
			n += len(cell.Text)
		}
	}
	return n
}

// --- Interactive props ---------------------------------------------------

// ButtonProps configures a button. The action is referenced via the
// Node's ActionRef field (not in props) — a button without an ActionRef
// is rejected by the validator.
type ButtonProps struct {
	Label   string `json:"label"`
	Variant string `json:"variant,omitempty"` // primary | secondary | ghost | danger
}

func (ButtonProps) propsMarker()             {}
func (ButtonProps) childPolicy() childPolicy { return childPolicyNone }
func (p ButtonProps) validate(lim Limits) error {
	if p.Label == "" {
		return errRequired("button.label")
	}
	if p.Variant != "" && !isButtonVariant(p.Variant) {
		return errBadEnum("button.variant", p.Variant)
	}
	return checkStrings(lim, "button", p.Label)
}
func (p ButtonProps) estimatedTextSize() int { return len(p.Label) + len(p.Variant) }

// LinkProps configures a link. Either To (host-relative) is set in props,
// OR the Node carries an ActionRef; exactly one is required.
type LinkProps struct {
	Text string `json:"text"`
	To   string `json:"to,omitempty"` // host-relative path; empty if ActionRef is used
}

func (LinkProps) propsMarker()             {}
func (LinkProps) childPolicy() childPolicy { return childPolicyNone }
func (p LinkProps) validate(lim Limits) error {
	if p.Text == "" {
		return errRequired("link.text")
	}
	if p.To != "" && !IsValidHostRelative(p.To) {
		return errBadURL("link.to", p.To)
	}
	return checkStrings(lim, "link", p.Text, p.To)
}
func (p LinkProps) estimatedTextSize() int { return len(p.Text) + len(p.To) }

// --- Media props --------------------------------------------------------

// ImageProps configures an image. Src is a host-relative same-origin path
// (validated by IsValidHostRelative — design §9 rejects javascript:/data:/
// vbscript:/blob:/file:/off-origin); Alt is required non-empty so every
// module-supplied image carries accessible alternative text. A module cannot
// forge dimensions, loading, or srcset — those are host-assigned.
type ImageProps struct {
	Src string `json:"src"` // required, host-relative same-origin path
	Alt string `json:"alt"` // required, non-empty accessible description
}

func (ImageProps) propsMarker()             {}
func (ImageProps) childPolicy() childPolicy { return childPolicyNone }
func (p ImageProps) validate(lim Limits) error {
	if p.Src == "" {
		return errRequired("image.src")
	}
	if !IsValidHostRelative(p.Src) {
		return errBadURL("image.src", p.Src)
	}
	if p.Alt == "" {
		return errRequired("image.alt")
	}
	return checkStrings(lim, "image", p.Src, p.Alt)
}
func (p ImageProps) estimatedTextSize() int { return len(p.Src) + len(p.Alt) }

// --- enum helpers --------------------------------------------------------

func isAlignValue(s string) bool {
	switch s {
	case "start", "center", "end", "stretch":
		return true
	}
	return false
}

func isToneValue(s string) bool {
	switch s {
	case "neutral", "positive", "negative", "warning", "info":
		return true
	}
	return false
}

func isButtonVariant(s string) bool {
	switch s {
	case "primary", "secondary", "ghost", "danger":
		return true
	}
	return false
}

// checkStrings returns an error if any of vals exceeds Limits.MaxPropString.
func checkStrings(lim Limits, field string, vals ...string) error {
	for _, v := range vals {
		if len(v) > lim.MaxPropString {
			return errTooLong(field, len(v), lim.MaxPropString)
		}
	}
	return nil
}
