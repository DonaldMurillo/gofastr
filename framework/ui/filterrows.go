package ui

import (
	"context"
	"strconv"

	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core-ui/registry"
	"github.com/DonaldMurillo/gofastr/core-ui/style"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework/headless"
)

// ─── FilterRows ────────────────────────────────────────────────────
//
// Structured filter rows inside a list's filter form: each row is a
// field, an operator and a value, submitted as three repeated params
// (FieldName, OpName, ValueName) in row order. The rows a filter
// already holds come first, then one blank row to add another. The
// server reads the rows and writes the filter; a row with an empty
// value is ignored, which is how a row is removed without script.

// FilterRow is one row's values.
type FilterRow struct{ Field, Op, Value string }

// FilterRowsConfig configures FilterRows.
type FilterRowsConfig struct {
	// ID prefixes each control's id. Required.
	ID string
	// FieldName, OpName and ValueName are the three params. Required.
	FieldName, OpName, ValueName string
	// Fields and Operators are the choices of the first two controls.
	Fields, Operators []SelectOption
	// Rows are the filter's rows; a blank row follows them.
	Rows []FilterRow
	// Legend names the group; FieldLabel, OpLabel and ValueLabel name
	// each row's controls, numbered ("Field 1").
	Legend, FieldLabel, OpLabel, ValueLabel string
	// ExtraAttrs forwards attributes to the fieldset; keys the
	// component owns are dropped.
	ExtraAttrs html.Attrs
	Ctx        context.Context
}

// FilterRows renders the rows.
func FilterRows(cfg FilterRowsConfig) render.HTML {
	if cfg.ID == "" || cfg.FieldName == "" || cfg.OpName == "" || cfg.ValueName == "" {
		panic("ui: FilterRows requires ID, FieldName, OpName and ValueName")
	}
	if cfg.Legend == "" || cfg.FieldLabel == "" || cfg.OpLabel == "" || cfg.ValueLabel == "" {
		panic("ui: FilterRows requires Legend and the three control labels — an unnamed control is announced as nothing")
	}
	rows := append(append([]FilterRow{}, cfg.Rows...), FilterRow{})
	if len(cfg.Operators) > 0 {
		rows[len(rows)-1].Op = cfg.Operators[0].Value
	}
	out := make([]render.HTML, 0, len(rows)+1)
	out = append(out, render.Tag("legend", map[string]string{"class": "fui-filter-rows__legend", "data-cui-internal": ""}, render.Text(cfg.Legend)))
	for i, r := range rows {
		n := strconv.Itoa(i + 1)
		id := cfg.ID + "-" + n
		out = append(out, html.Div(html.DivConfig{Class: "fui-filter-rows__row", ExtraAttrs: html.Attrs{"data-cui-internal": ""}},
			filterRowSelect(id+"-f", cfg.FieldName, cfg.FieldLabel+" "+n, cfg.Fields, r.Field),
			filterRowSelect(id+"-o", cfg.OpName, cfg.OpLabel+" "+n, cfg.Operators, r.Op),
			render.Join(
				render.Tag("label", map[string]string{"for": id + "-v", "class": "fui-visually-hidden"}, render.Text(cfg.ValueLabel+" "+n)),
				Control(ControlConfig{Field: headless.FieldControl{ID: id + "-v"}, Type: "text", Name: cfg.ValueName, Value: r.Value}),
			),
		))
	}
	attrs := map[string]string{}
	for k, v := range headless.Safe(cfg.ExtraAttrs) {
		attrs[k] = v
	}
	attrs["class"] = "fui-filter-rows"
	return filterRowsStyle.WrapHTML(render.Tag("fieldset", attrs, out...))
}

// filterRowSelect is one row's select under a visually hidden label.
func filterRowSelect(id, name, label string, opts []SelectOption, selected string) render.HTML {
	hopts := make([]headless.Option, 0, len(opts))
	for _, o := range opts {
		hopts = append(hopts, headless.Option{Value: o.Value, Label: o.Text})
	}
	return render.Join(
		render.Tag("label", map[string]string{"for": id, "class": "fui-visually-hidden"}, render.Text(label)),
		selectStyle.WrapHTML(headless.Select(headless.SelectProps{Name: name, ID: id, Options: hopts, Selected: selected}, selectClasses)),
	)
}

var filterRowsStyle = registry.RegisterStyle("ui-filter-rows", filterRowsCSS)

// filterRowsCSS lays each row out as field, operator and value, the
// value taking the rest; on a narrow container the value drops below.
func filterRowsCSS(_ style.Theme) string {
	return `[data-cui-comp="ui-filter-rows"] {
  border: 0;
  margin: 0;
  padding: 0;
  min-inline-size: 0;
  display: grid;
  gap: var(--spacing-sm, 4px);
  container-type: inline-size;
}
[data-cui-comp="ui-filter-rows"] .fui-filter-rows__legend {
  padding: 0;
  margin-block-end: var(--spacing-sm, 4px);
  font-size: var(--text-sm, 0.875rem);
  font-weight: var(--font-weight-medium);
  color: var(--color-text);
}
[data-cui-comp="ui-filter-rows"] .fui-filter-rows__row {
  display: grid;
  grid-template-columns: minmax(0, 1fr) minmax(0, 0.8fr) minmax(0, 1.2fr);
  gap: var(--spacing-sm, 4px);
  align-items: center;
}
@container (max-width: 420px) {
  [data-cui-comp="ui-filter-rows"] .fui-filter-rows__row {
    grid-template-columns: minmax(0, 1fr) minmax(0, 1fr);
  }
  [data-cui-comp="ui-filter-rows"] .fui-filter-rows__row > :last-child {
    grid-column: 1 / -1;
  }
}
[data-cui-comp="ui-filter-rows"] .fui-visually-hidden {
  position: absolute;
  inline-size: 1px;
  block-size: 1px;
  padding: 0;
  margin: -1px;
  overflow: hidden;
  clip: rect(0 0 0 0);
  white-space: nowrap;
  border: 0;
}`
}
