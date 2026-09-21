package ui

// DetailList: a label/value description list for record detail screens
// ("Name: Ada Lovelace", "Status: <badge>"). headless.DetailList
// renders the <dl>/<dt>/<dd> contract — a term and the value after it
// are one pair to assistive technology — dressed with this package's
// fui-detail-list class map. The framework owns the layout so detail
// screens don't hand-roll key/value CSS.

import (
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

	// ExtraAttrs forwards additional attributes (data-* test hooks,
	// analytics markers, ARIA overrides) to the root <dl>. Keys the
	// component owns are dropped: class (use Class), id, style and
	// data-fui-*.
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
// dash, so an absence reads as deliberate.
func DetailList(cfg DetailListConfig) render.HTML {
	rows := make([]headless.DetailRow, 0, len(cfg.Items))
	for _, it := range cfg.Items {
		value := it.Value
		if value == "" {
			value = EmptyValue()
		}
		rows = append(rows, headless.DetailRow{Label: it.Label, Value: value})
	}
	attrs := headless.Safe(cfg.ExtraAttrs, "class", "id")
	if attrs == nil {
		attrs = html.Attrs{}
	}
	if cfg.Class != "" {
		attrs["class"] = cfg.Class
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
	return `[data-fui-comp="ui-detail-list"] {
  display: flex;
  flex-direction: column;
  max-width: 44rem;
  margin: 0;
}
[data-fui-comp="ui-detail-list"] .fui-detail-list__row {
  display: grid;
  grid-template-columns: minmax(7rem, 13rem) 1fr;
  gap: var(--spacing-lg, 16px);
  align-items: baseline;
  padding: var(--spacing-sm, 4px) 0;
  border-bottom: 1px solid var(--color-border, rgba(0,0,0,0.1));
}
[data-fui-comp="ui-detail-list"] .fui-detail-list__row:last-child { border-bottom: none; }
[data-fui-comp="ui-detail-list"] .fui-detail-list__label {
  margin: 0;
  color: var(--color-text-muted, inherit);
  font-weight: 500;
}
[data-fui-comp="ui-detail-list"] .fui-detail-list__value {
  margin: 0;
  color: var(--color-text, inherit);
}
@media (max-width: 30rem) {
  [data-fui-comp="ui-detail-list"] .fui-detail-list__row { grid-template-columns: 1fr; gap: var(--spacing-xs, 2px); }
}
`
}
