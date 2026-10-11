package ui

import (
	"context"

	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core-ui/registry"
	"github.com/DonaldMurillo/gofastr/core-ui/style"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework/headless"
	"github.com/DonaldMurillo/gofastr/framework/i18nui"
)

// ─── ColumnPicker ──────────────────────────────────────────────────
//
// A list's Columns control: a dropdown of its columns in display order,
// each a link that shows or hides it, with move up and move down links
// beside a shown one, and a Reset link. Every link is a navigation the
// caller builds (the list's state lives in its URL), so it works without
// script and the server owns the order.

// ColumnChoice is one column's row.
type ColumnChoice struct {
	// Label names the column. Required.
	Label string
	// Shown marks the column on; Locked keeps it on with no toggle (the
	// column that links to the record).
	Shown, Locked bool
	// ToggleHref shows or hides it; UpHref and DownHref move a shown
	// column one place. An empty href draws that control disabled.
	ToggleHref, UpHref, DownHref string
}

// ColumnPickerConfig configures a ColumnPicker.
type ColumnPickerConfig struct {
	// ID is the dropdown's id. Required.
	ID string
	// Label is the trigger's text ("Columns"). Required.
	Label   string
	Columns []ColumnChoice
	// ResetHref and ResetLabel draw the Reset link; empty draws none.
	ResetHref, ResetLabel string
	// ExtraAttrs forwards attributes to the dropdown's root.
	ExtraAttrs html.Attrs
	Ctx        context.Context
}

// ColumnPicker renders the control.
func ColumnPicker(cfg ColumnPickerConfig) render.HTML {
	if cfg.ID == "" || cfg.Label == "" {
		panic("ui: ColumnPicker requires ID and Label")
	}
	ctx := cfg.Ctx
	if ctx == nil {
		ctx = context.Background()
	}
	rows := make([]render.HTML, 0, len(cfg.Columns)+1)
	for _, c := range cfg.Columns {
		markClass, glyph := "fui-colpick__mark", render.HTML("")
		if c.Shown {
			markClass, glyph = "fui-colpick__mark fui-colpick__mark--on", Icon("check", IconConfig{Size: "14"})
		}
		mark := render.Tag("span", map[string]string{"class": markClass, "aria-hidden": "true"}, glyph)
		var toggle render.HTML
		switch {
		case c.Locked || c.ToggleHref == "":
			toggle = render.Tag("span", map[string]string{"class": "fui-colpick__toggle", "aria-disabled": "true"}, mark, render.Text(c.Label))
		default:
			key := i18nui.KeyColumnShow
			if c.Shown {
				key = i18nui.KeyColumnHide
			}
			toggle = render.Tag("a", map[string]string{
				"class":      "fui-colpick__toggle",
				"href":       c.ToggleHref,
				"aria-label": i18nui.TVars(ctx, key, map[string]string{"column": c.Label}),
			}, mark, render.Text(c.Label))
		}
		kids := []render.HTML{toggle}
		if c.Shown {
			kids = append(kids,
				colpickMove(ctx, c.UpHref, "arrow-up", i18nui.KeyColumnMoveUp, c.Label),
				colpickMove(ctx, c.DownHref, "arrow-down", i18nui.KeyColumnMoveDown, c.Label))
		}
		rows = append(rows, render.Tag("li", map[string]string{"class": "fui-colpick__row"}, kids...))
	}
	body := []render.HTML{render.Tag("ul", map[string]string{"class": "fui-colpick__list"}, rows...)}
	if cfg.ResetHref != "" && cfg.ResetLabel != "" {
		body = append(body, render.Tag("a", map[string]string{"class": "fui-colpick__reset", "href": cfg.ResetHref}, render.Text(cfg.ResetLabel)))
	}
	// The rows are built from strings and the caller's hrefs: the
	// panel is this component's own.
	content := headless.Own(columnPickerStyle.WrapHTML(html.Div(html.DivConfig{Class: "fui-colpick"}, body...)))
	return Dropdown(DropdownConfig{
		ID: cfg.ID, Label: cfg.Label, Icon: "columns", Content: content,
		Align: DropdownEnd, ExtraAttrs: html.SafeExtraAttrs(cfg.ExtraAttrs),
	})
}

// colpickMove is a move link, or the same glyph disabled with no href.
func colpickMove(ctx context.Context, href, icon string, key i18nui.Key, label string) render.HTML {
	name := i18nui.TVars(ctx, key, map[string]string{"column": label})
	attrs := map[string]string{"class": "fui-colpick__move", "aria-label": name, "title": name}
	tag := "a"
	if href == "" {
		tag = "span"
		attrs["aria-disabled"] = "true"
	} else {
		attrs["href"] = href
	}
	return render.Tag(tag, attrs, Icon(icon, IconConfig{Size: "14"}))
}

var columnPickerStyle = registry.RegisterStyle("ui-column-picker", columnPickerCSS)

func columnPickerCSS(_ style.Theme) string {
	return `[data-cui-comp="ui-column-picker"] {
  min-inline-size: var(--ui-column-picker-min-inline-size, 15rem);
  display: grid;
  gap: var(--spacing-xs, 2px);
}
[data-cui-comp="ui-column-picker"] .fui-colpick__list {
  list-style: none;
  margin: 0;
  padding: 0;
  display: grid;
  gap: var(--spacing-xs, 2px);
}
[data-cui-comp="ui-column-picker"] .fui-colpick__row {
  display: flex;
  align-items: center;
  gap: var(--spacing-xs, 2px);
}
[data-cui-comp="ui-column-picker"] .fui-colpick__toggle,
[data-cui-comp="ui-column-picker"] .fui-colpick__reset {
  flex: 1;
  display: flex;
  align-items: center;
  gap: var(--spacing-sm, 4px);
  min-block-size: var(--fui-density-control-h, 36px);
  padding-inline: var(--spacing-sm, 4px);
  border-radius: var(--radii-sm, 6px);
  color: var(--color-text);
  text-decoration: none;
  font-size: var(--text-sm, 0.875rem);
}
[data-cui-comp="ui-column-picker"] .fui-colpick__toggle[aria-disabled="true"] {
  color: var(--color-text-muted);
}
[data-cui-comp="ui-column-picker"] a.fui-colpick__toggle:hover,
[data-cui-comp="ui-column-picker"] .fui-colpick__reset:hover,
[data-cui-comp="ui-column-picker"] a.fui-colpick__move:hover {
  background: var(--color-surface-soft);
}
[data-cui-comp="ui-column-picker"] .fui-colpick__mark {
  display: inline-grid;
  place-items: center;
  inline-size: var(--ui-column-picker-mark-size, 1.125rem);
  block-size: var(--ui-column-picker-mark-size, 1.125rem);
  border: var(--stroke-thin, 1px) solid var(--color-border-strong, var(--color-border));
  border-radius: var(--radii-sm, 6px);
}
[data-cui-comp="ui-column-picker"] .fui-colpick__mark--on {
  background: var(--color-primary);
  border-color: var(--color-primary);
  color: var(--color-primary-fg, #fff);
}
[data-cui-comp="ui-column-picker"] .fui-colpick__move {
  display: inline-grid;
  place-items: center;
  inline-size: var(--ui-column-picker-move-size, 1.75rem);
  block-size: var(--ui-column-picker-move-size, 1.75rem);
  border-radius: var(--radii-sm, 6px);
  color: var(--color-text-muted);
}
[data-cui-comp="ui-column-picker"] .fui-colpick__move[aria-disabled="true"] {
  opacity: var(--opacity-disabled, 0.5);
}
[data-cui-comp="ui-column-picker"] .fui-colpick__reset {
  border-block-start: var(--stroke-thin, 1px) solid var(--color-border);
  border-radius: 0;
  margin-block-start: var(--spacing-xs, 2px);
}`
}
