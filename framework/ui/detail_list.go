package ui

// DetailList: a label/value description list for record detail screens
// ("Name: Ada Lovelace", "Status: <badge>"). headless.DetailList
// renders the <dl>/<dt>/<dd> contract — a term and the value after it
// are one pair to assistive technology — dressed with this package's
// fui-detail-list class map. The framework owns the layout so detail
// screens don't hand-roll key/value CSS.

import (
	"strings"

	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core-ui/registry"
	"github.com/DonaldMurillo/gofastr/core-ui/style"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework/headless"
)

// DetailItem is one label/value row.
type DetailItem struct {
	Label string
	Value render.HTML
}

// DetailListConfig configures a DetailList.
type DetailListConfig struct {
	Items []DetailItem
	Class string
	// Inline keeps short label/value pairs on one line in narrow panes.
	// Long values wrap within their column instead of moving below the label.
	Inline bool
	// Stacked draws each pair the way a form field sits: the label
	// above, the value in a box with a control's height, padding and
	// radius on the soft surface, so a read-only value in a form lines
	// up with the inputs around it and reads as locked. It drops the
	// row rules and the measure cap. Stacked and Inline are exclusive.
	Stacked bool
	// Spread draws compact facts for a side column: the label at the
	// row's start, the value at its end, no row rules, one line each.
	// A value that does not fit truncates its first element (an id in
	// <code>) while the elements after it (a copy button) keep their
	// size. Spread excludes Inline and Stacked.
	Spread bool

	// ExtraAttrs forwards additional attributes (data-* test hooks,
	// analytics markers, ARIA overrides) to the root <dl>. Keys the
	// component owns are dropped: class (use Class), id, style and
	// data-cui-*.
	ExtraAttrs html.Attrs
}

// detailListClasses dresses headless.DetailList's parts.
var detailListClasses = headless.Classes{
	headless.PartRoot:        "fui-detail-list",
	headless.PartDetailRow:   "fui-detail-list__row",
	headless.PartDetailTerm:  "fui-detail-list__label",
	headless.PartDetailValue: "fui-detail-list__value",
}

// DetailList renders a label/value description list on
// headless.DetailList. An item with no Value renders the empty-value
// dash, so an absence reads as deliberate. A list with no items
// renders nothing: the primitive refuses an empty <dl>, and a record
// with no fields is data, not a developer's mistake.
func DetailList(cfg DetailListConfig) render.HTML {
	if len(cfg.Items) == 0 {
		return render.HTML("")
	}
	rows := make([]headless.DetailRow, 0, len(cfg.Items))
	for _, it := range cfg.Items {
		value := it.Value
		if value == "" {
			value = headless.Own(EmptyValue())
		}
		rows = append(rows, headless.DetailRow{Label: it.Label, Value: value})
	}
	attrs := headless.Safe(cfg.ExtraAttrs, "class", "id")
	if attrs == nil {
		attrs = html.Attrs{}
	}
	if (cfg.Inline && cfg.Stacked) || (cfg.Spread && (cfg.Inline || cfg.Stacked)) {
		panic("ui: DetailList Inline, Stacked and Spread are exclusive")
	}
	class := ""
	switch {
	case cfg.Inline:
		class = "fui-detail-list--inline"
	case cfg.Stacked:
		class = "fui-detail-list--stacked"
	case cfg.Spread:
		class = "fui-detail-list--spread"
	}
	if cfg.Class != "" {
		class = strings.TrimSpace(class + " " + cfg.Class)
	}
	if class != "" {
		attrs["class"] = class
	}
	return detailListStyle.WrapHTML(headless.DetailList(headless.DetailListProps{
		Rows: rows,
		Parts: headless.Parts{Attrs: headless.PartAttrs{
			headless.PartRoot: attrs,
		}},
	}, detailListClasses))
}

var detailListStyle = registry.RegisterStyle("ui-detail-list", detailListCSS)

func detailListCSS(_ style.Theme) string {
	// Knobs: --ui-detail-list-max-width (44rem) caps the list's
	// measure (the label-column knob sits on the row rule below).
	return `[data-cui-comp="ui-detail-list"] {
  display: flex;
  flex-direction: column;
  container-type: inline-size;
  max-width: var(--ui-detail-list-max-width, 44rem);
  margin: 0;
  font-size: var(--text-sm, 0.875rem);
}
[data-cui-comp="ui-detail-list"] .fui-detail-list__row {
  display: grid;
  /* --ui-detail-list-label-track lets a narrow container (a side
     panel) cap the label column: the default track grows to 13rem
     before the value column gets any width. */
  grid-template-columns: var(--ui-detail-list-label-track, minmax(7rem, 13rem)) 1fr;
  gap: var(--spacing-lg, 16px);
  align-items: baseline;
  padding: var(--spacing-md, 8px) 0;
  border-bottom: var(--stroke-thin, 1px) solid var(--color-border, rgba(0,0,0,0.1));
}
[data-cui-comp="ui-detail-list"] .fui-detail-list__row:last-child { border-bottom: none; }
[data-cui-comp="ui-detail-list"] .fui-detail-list__label {
  margin: 0;
  color: var(--color-text-muted, inherit);
}
[data-cui-comp="ui-detail-list"] .fui-detail-list__value {
  margin: 0;
  color: var(--color-text, inherit);
  font-weight: var(--font-weight-medium);
}
@container (max-width: 30rem) {
  [data-cui-comp="ui-detail-list"] .fui-detail-list__row { grid-template-columns: 1fr; gap: var(--spacing-xs, 2px); }
}
@media (max-width: 30rem) {
  [data-cui-comp="ui-detail-list"] .fui-detail-list__row { grid-template-columns: 1fr; gap: var(--spacing-xs, 2px); }
}
[data-cui-comp="ui-detail-list"].fui-detail-list--inline .fui-detail-list__row { grid-template-columns: minmax(0, 2fr) minmax(0, 3fr); gap: var(--spacing-sm); }
[data-cui-comp="ui-detail-list"].fui-detail-list--inline .fui-detail-list__value { min-inline-size: 0; overflow-wrap: anywhere; }
/* Spread: side-column facts, label start, value end, one line. */
[data-cui-comp="ui-detail-list"].fui-detail-list--spread { --ui-detail-list-max-width: none; }
[data-cui-comp="ui-detail-list"].fui-detail-list--spread .fui-detail-list__row {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: var(--spacing-md, 8px);
  min-block-size: var(--fui-density-control-h, 2rem);
  padding: 0;
  border-bottom: none;
}
[data-cui-comp="ui-detail-list"].fui-detail-list--spread .fui-detail-list__label { flex: none; }
[data-cui-comp="ui-detail-list"].fui-detail-list--spread .fui-detail-list__value {
  display: flex;
  align-items: center;
  justify-content: flex-end;
  gap: var(--spacing-xs, 2px);
  min-inline-size: 0;
  white-space: nowrap;
}
[data-cui-comp="ui-detail-list"].fui-detail-list--spread .fui-detail-list__value > :first-child {
  min-inline-size: 0;
  overflow: hidden;
  text-overflow: ellipsis;
}
[data-cui-comp="ui-detail-list"].fui-detail-list--spread .fui-detail-list__value > :not(:first-child) { flex: none; }
/* Stacked: a form field's anatomy, the value boxed like a control. */
[data-cui-comp="ui-detail-list"].fui-detail-list--stacked {
  --ui-detail-list-max-width: none;
  gap: var(--spacing-lg, 16px);
}
[data-cui-comp="ui-detail-list"].fui-detail-list--stacked .fui-detail-list__row {
  grid-template-columns: minmax(0, 1fr);
  gap: var(--fui-density-gap, var(--spacing-sm, 4px));
  padding: 0;
  border-bottom: none;
}
[data-cui-comp="ui-detail-list"].fui-detail-list--stacked .fui-detail-list__label {
  font-weight: var(--font-weight-medium);
  color: var(--color-text, #18181B);
}
[data-cui-comp="ui-detail-list"].fui-detail-list--stacked .fui-detail-list__value {
  display: flex;
  align-items: center;
  box-sizing: border-box;
  min-height: var(--fui-density-control-h, var(--spacing-touch-target, 44px));
  padding: var(--ui-control-padding-y, 10px) calc(var(--spacing-sm, 4px) * 3);
  border: var(--stroke-thin, 1px) solid var(--color-border);
  border-radius: var(--fui-field-radius, var(--radii-md, 8px));
  background: var(--color-surface-soft);
  color: var(--color-text-muted);
  font-weight: var(--font-weight-normal, 400);
  min-inline-size: 0;
  overflow-wrap: anywhere;
}
`
}
