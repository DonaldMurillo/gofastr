package ui

// DetailList — a label/value description list for record detail screens
// ("Name: Ada Lovelace", "Status: <badge>"). Renders semantic <dl>/<dt>/<dd>
// with a two-column grid that collapses gracefully. The framework owns the
// layout so detail screens don't hand-roll key/value CSS.

import (
	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core-ui/registry"
	"github.com/DonaldMurillo/gofastr/core-ui/style"
	"github.com/DonaldMurillo/gofastr/core/render"
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
}

// DetailList renders a label/value description list.
func DetailList(cfg DetailListConfig) render.HTML {
	rows := make([]render.HTML, 0, len(cfg.Items))
	for _, it := range cfg.Items {
		rows = append(rows, html.Div(html.DivConfig{Class: "ui-detail-list__row"},
			render.Tag("dt", map[string]string{"class": "ui-detail-list__label"}, render.Text(it.Label)),
			render.Tag("dd", map[string]string{"class": "ui-detail-list__value"}, it.Value),
		))
	}
	cls := "ui-detail-list"
	if cfg.Class != "" {
		cls = cls + " " + cfg.Class
	}
	return detailListStyle.WrapHTML(render.Tag("dl", map[string]string{"class": cls}, rows...))
}

var detailListStyle = registry.RegisterStyle("ui-detail-list", detailListCSS)

func detailListCSS(_ style.Theme) string {
	return `[data-fui-comp="ui-detail-list"] {
  display: flex;
  flex-direction: column;
  max-width: 44rem;
  margin: 0;
}
[data-fui-comp="ui-detail-list"] .ui-detail-list__row {
  display: grid;
  grid-template-columns: minmax(7rem, 13rem) 1fr;
  gap: var(--spacing-lg, 24px);
  align-items: baseline;
  padding: var(--spacing-sm, 11px) 0;
  border-bottom: 1px solid var(--color-border, rgba(0,0,0,0.1));
}
[data-fui-comp="ui-detail-list"] .ui-detail-list__row:last-child { border-bottom: none; }
[data-fui-comp="ui-detail-list"] .ui-detail-list__label {
  margin: 0;
  color: var(--color-text-muted, inherit);
  font-weight: 500;
}
[data-fui-comp="ui-detail-list"] .ui-detail-list__value {
  margin: 0;
  color: var(--color-text, inherit);
}
@media (max-width: 30rem) {
  [data-fui-comp="ui-detail-list"] .ui-detail-list__row { grid-template-columns: 1fr; gap: 2px; }
}
`
}
