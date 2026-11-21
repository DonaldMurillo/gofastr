package uinodev1

// Component is the closed set of component names a tree may use.
type Component string

const (
	// Layout components: accept children.
	CompStack   Component = "stack"
	CompCluster Component = "cluster"
	CompGrid    Component = "grid"
	CompSection Component = "section"
	CompCard    Component = "card"
	CompDivider Component = "divider"

	// Text components: no children (text comes via props).
	CompHeading   Component = "heading"
	CompParagraph Component = "paragraph"
	CompText      Component = "text"
	CompStrong    Component = "strong"
	CompEm        Component = "em"
	CompCode      Component = "code"
	CompSmall     Component = "small"
	CompBadge     Component = "badge"

	// Read-only data components: no children.
	CompDetailList Component = "detail-list"
	CompKeyValue   Component = "key-value"
	CompStatCard   Component = "stat-card"
	CompDataTable  Component = "data-table"

	// CompButton needs an ActionRef.
	CompButton Component = "button"
	CompLink   Component = "link"

	// CompImage takes a host-relative src and a required alt.
	CompImage Component = "image"
)

// Node is one element of a validated tree.
type Node struct {
	// Component is the closed enum value (one of the Comp* constants).
	Component Component
	// Props is the component's typed props, never nil once decoded.
	Props Props
	// Children are accepted only on layout components.
	Children []Node
	// ActionRef is an opaque route reference the host resolves; button and link only.
	ActionRef string
}

// Tree is a validated ui.node.v1 document.
type Tree struct {
	Root Node
}

// Props is the sealed union of the per-component prop structs.
type Props interface {
	propsMarker()
	validate(lim Limits) error
	childPolicy() childPolicy
	estimatedTextSize() int
}

type childPolicy int

const (
	childPolicyNone childPolicy = iota
	childPolicyAny
)

// StackProps configures a vertical-or-horizontal stack (flex column/row).
type StackProps struct {
	Direction string `json:"direction,omitempty"`
	Gap       string `json:"gap,omitempty"`
	Align     string `json:"align,omitempty"`
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
	return checkStrings(lim, "stack", p.Gap, p.Align, p.Direction)
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
	return checkStrings(lim, "cluster", p.Gap, p.Align)
}
func (p ClusterProps) estimatedTextSize() int { return len(p.Gap) + len(p.Align) }

// GridProps configures a column grid.
type GridProps struct {
	Columns int    `json:"columns,omitempty"`
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
	Elevation string `json:"elevation,omitempty"`
}

func (CardProps) propsMarker()               {}
func (p CardProps) childPolicy() childPolicy { return childPolicyAny }
func (p CardProps) validate(lim Limits) error {
	if p.Elevation != "" && p.Elevation != "flat" && p.Elevation != "low" && p.Elevation != "high" {
		return errBadEnum("card.elevation", p.Elevation)
	}
	return checkStrings(lim, "card", p.Title, p.Elevation)
}
func (p CardProps) estimatedTextSize() int { return len(p.Title) + len(p.Elevation) }

// DividerProps has no fields, a divider is purely structural.
type DividerProps struct{}

func (DividerProps) propsMarker()             {}
func (DividerProps) childPolicy() childPolicy { return childPolicyNone }
func (DividerProps) validate(Limits) error    { return nil }
func (DividerProps) estimatedTextSize() int   { return 0 }

// HeadingProps configures a heading. Level (1–6) and Text are required.
type HeadingProps struct {
	Level int    `json:"level"`
	Text  string `json:"text"`
}

func (HeadingProps) propsMarker()             {}
func (HeadingProps) childPolicy() childPolicy { return childPolicyNone }
func (p HeadingProps) validate(lim Limits) error {
	if p.Level < 1 || p.Level > 6 {
		return errBadRange("heading.level", p.Level, 1, 6)
	}
	if err := requiredString("heading.text", p.Text); err != nil {
		return err
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
	if err := requiredString("paragraph.text", p.Text); err != nil {
		return err
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
	if err := requiredString("text.text", p.Text); err != nil {
		return err
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
	if err := requiredString("strong.text", p.Text); err != nil {
		return err
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
	if err := requiredString("em.text", p.Text); err != nil {
		return err
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
	if err := requiredString("code.text", p.Text); err != nil {
		return err
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
	if err := requiredString("small.text", p.Text); err != nil {
		return err
	}
	return checkStrings(lim, "small", p.Text)
}
func (p SmallProps) estimatedTextSize() int { return len(p.Text) }

// BadgeProps is a small status label.
type BadgeProps struct {
	Text string `json:"text"`
	Tone string `json:"tone,omitempty"`
}

func (BadgeProps) propsMarker()             {}
func (BadgeProps) childPolicy() childPolicy { return childPolicyNone }
func (p BadgeProps) validate(lim Limits) error {
	if err := requiredString("badge.text", p.Text); err != nil {
		return err
	}
	if p.Tone != "" && !isToneValue(p.Tone) {
		return errBadEnum("badge.tone", p.Tone)
	}
	return checkStrings(lim, "badge", p.Text, p.Tone)
}
func (p BadgeProps) estimatedTextSize() int { return len(p.Text) + len(p.Tone) }

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
	return validatePairs(lim, "detail-list", p.Items)
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
	return validatePairs(lim, "key-value", p.Items)
}
func (p KeyValueProps) estimatedTextSize() int {
	n := 0
	for _, it := range p.Items {
		n += len(it.Key) + len(it.Value)
	}
	return n
}

type labelValue interface {
	pair() (label, value string)
}

func (it DetailItem) pair() (string, string)   { return it.Label, it.Value }
func (it KeyValueItem) pair() (string, string) { return it.Key, it.Value }

func validatePairs[T labelValue](lim Limits, what string, items []T) error {
	if err := checkPropItems(lim, what+".items", len(items)); err != nil {
		return err
	}
	if len(items) == 0 {
		return errRequired(what + ".items")
	}
	for i, it := range items {
		label, value := it.pair()
		if label == "" || value == "" {
			return errRequiredIndex(what+".items", i)
		}
		if err := checkStrings(lim, what, label, value); err != nil {
			return err
		}
	}
	return nil
}

// StatCardProps is a single labeled metric.
type StatCardProps struct {
	Label string `json:"label"`
	Value string `json:"value"`
	Unit  string `json:"unit,omitempty"`
	Trend string `json:"trend,omitempty"`
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
	return checkStrings(lim, "stat-card", p.Label, p.Value, p.Unit, p.Trend)
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

// DataRow is one row of a data-table.
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
	if err := checkPropItems(lim, "data-table.columns", len(p.Columns)); err != nil {
		return err
	}
	if err := checkPropItems(lim, "data-table.rows", len(p.Rows)); err != nil {
		return err
	}
	if len(p.Columns) == 0 {
		return errRequired("data-table.columns")
	}
	seen := make(map[string]bool, len(p.Columns))
	for i, c := range p.Columns {
		if c.Key == "" || c.Label == "" {
			return errRequiredIndex("data-table.columns", i)
		}
		if err := checkStrings(lim, "data-table.column", c.Key, c.Label); err != nil {
			return err
		}
		if seen[c.Key] {
			return errDupColumnKey(c.Key)
		}
		seen[c.Key] = true
	}
	for i, r := range p.Rows {
		if err := checkPropItems(lim, "data-table.rows.cells", len(r.Cells)); err != nil {
			return err
		}
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

// ButtonProps configures a button; its action is the Node's ActionRef.
type ButtonProps struct {
	Label   string `json:"label"`
	Variant string `json:"variant,omitempty"`
}

func (ButtonProps) propsMarker()             {}
func (ButtonProps) childPolicy() childPolicy { return childPolicyNone }
func (p ButtonProps) validate(lim Limits) error {
	if err := requiredString("button.label", p.Label); err != nil {
		return err
	}
	if p.Variant != "" && !isButtonVariant(p.Variant) {
		return errBadEnum("button.variant", p.Variant)
	}
	return checkStrings(lim, "button", p.Label, p.Variant)
}
func (p ButtonProps) estimatedTextSize() int { return len(p.Label) + len(p.Variant) }

// LinkProps configures a link to To, or to the Node's ActionRef.
type LinkProps struct {
	Text string `json:"text"`
	To   string `json:"to,omitempty"`
}

func (LinkProps) propsMarker()             {}
func (LinkProps) childPolicy() childPolicy { return childPolicyNone }
func (p LinkProps) validate(lim Limits) error {
	if err := requiredString("link.text", p.Text); err != nil {
		return err
	}
	if p.To != "" && !IsValidHostRelative(p.To) {
		return errBadURL("link.to", p.To)
	}
	return checkStrings(lim, "link", p.Text, p.To)
}
func (p LinkProps) estimatedTextSize() int { return len(p.Text) + len(p.To) }

// ImageProps configures an image with a host-relative Src and a required Alt.
type ImageProps struct {
	Src string `json:"src"`
	Alt string `json:"alt"`
}

func (ImageProps) propsMarker()             {}
func (ImageProps) childPolicy() childPolicy { return childPolicyNone }
func (p ImageProps) validate(lim Limits) error {
	if err := requiredString("image.src", p.Src); err != nil {
		return err
	}
	if !IsValidHostRelative(p.Src) {
		return errBadURL("image.src", p.Src)
	}
	if err := requiredString("image.alt", p.Alt); err != nil {
		return err
	}
	return checkStrings(lim, "image", p.Src, p.Alt)
}
func (p ImageProps) estimatedTextSize() int { return len(p.Src) + len(p.Alt) }

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

func requiredString(field, v string) error {
	if v == "" {
		return errRequired(field)
	}
	return nil
}

func checkStrings(lim Limits, field string, vals ...string) error {
	for _, v := range vals {
		if len(v) > lim.MaxPropString {
			return errTooLong(field, len(v), lim.MaxPropString)
		}
	}
	return nil
}
