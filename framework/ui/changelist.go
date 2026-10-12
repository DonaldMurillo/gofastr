package ui

// ─── ChangeList ─────────────────────────────────────────────────────
//
// What one edit changed, field by field: the field's label, the old
// value struck through, an arrow, the new value. An audit trail draws it
// under each update. The old value sits in <del> and the new one in
// <ins>, each led by visually hidden "from" / "to" words, because most
// screen readers announce neither element.

import (
	"context"

	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core-ui/registry"
	"github.com/DonaldMurillo/gofastr/core-ui/style"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework/headless"
	"github.com/DonaldMurillo/gofastr/framework/i18nui"
)

// Change is one field's edit. An empty From or To draws the empty
// mark: a value set for the first time, or cleared.
type Change struct {
	Label string
	From  render.HTML
	To    render.HTML
}

// ChangeListConfig configures a ChangeList.
type ChangeListConfig struct {
	Changes []Change
	// Ctx resolves the visually hidden "from" / "to" words. When nil,
	// English applies.
	Ctx   context.Context
	ID    string
	Class string
	// ExtraAttrs forwards additional attributes to the <ul> root. Keys
	// the component owns are dropped: class and id (use Class / ID),
	// style and data-cui-*.
	ExtraAttrs html.Attrs
}

// ChangeList renders the edits as a list, one field per row. It panics
// on an empty Changes: an edit that changed nothing has nothing to list,
// so the caller draws no list at all.
func ChangeList(cfg ChangeListConfig) render.HTML {
	if len(cfg.Changes) == 0 {
		panic("ui: ChangeList requires at least one Change")
	}
	ctx := cfg.Ctx
	if ctx == nil {
		ctx = context.Background()
	}
	from := i18nui.T(ctx, i18nui.KeyChangeFrom)
	to := i18nui.T(ctx, i18nui.KeyChangeTo)
	// The label, arrow, hidden words and empty mark are the component's
	// own; so is a side, or a whole row, holding no caller value.
	own := func(tag, class string, kids ...render.HTML) render.HTML {
		return headless.Own(render.Tag(tag, map[string]string{"class": class}, kids...))
	}
	side := func(tag, class, word string, v render.HTML) render.HTML {
		lead := own("span", "fui-visually-hidden", render.Text(word+" "))
		if v == "" {
			return own(tag, class, lead, headless.Own(EmptyValue()))
		}
		return render.Tag(tag, map[string]string{"class": class}, lead, v)
	}
	items := make([]render.HTML, len(cfg.Changes))
	for i, c := range cfg.Changes {
		arrow := headless.Own(render.Tag("span", map[string]string{"class": "fui-change-list__arrow", "aria-hidden": "true"}, render.Text("→")))
		item := render.Tag("li", map[string]string{"class": "fui-change-list__item"},
			own("span", "fui-change-list__label", render.Text(c.Label)),
			side("del", "fui-change-list__from", from, c.From),
			arrow,
			side("ins", "fui-change-list__to", to, c.To),
		)
		if c.From == "" && c.To == "" {
			item = headless.Own(item)
		}
		items[i] = item
	}
	attrs := headless.Safe(cfg.ExtraAttrs, "class", "id")
	if attrs == nil {
		attrs = html.Attrs{}
	}
	cls := "fui-change-list"
	if cfg.Class != "" {
		cls += " " + cfg.Class
	}
	attrs["class"] = cls
	if cfg.ID != "" {
		attrs["id"] = cfg.ID
	}
	return changeListStyle.WrapHTML(render.Tag("ul", attrs, items...))
}

var changeListStyle = registry.RegisterStyle("ui-change-list", changeListCSS)

func changeListCSS(_ style.Theme) string {
	return `[data-cui-comp="ui-change-list"] {
  display: grid;
  gap: var(--spacing-xs, 2px);
  margin: 0;
  padding: 0;
  list-style: none;
  /* In a table cell that scrolls, min-content would wrap every word
     onto its own line; hold a readable width instead. */
  min-width: var(--ui-change-list-min-width, 14rem);
  font-size: var(--text-sm, 0.875rem);
  line-height: var(--leading-normal, 1.5);
}
[data-cui-comp="ui-change-list"] .fui-change-list__item {
  display: flex;
  flex-wrap: wrap;
  align-items: baseline;
  gap: var(--spacing-xs, 2px) var(--spacing-sm, 4px);
  min-width: 0;
}
[data-cui-comp="ui-change-list"] .fui-change-list__label {
  font-weight: var(--font-weight-medium);
  color: var(--color-text);
}
[data-cui-comp="ui-change-list"] .fui-change-list__from {
  color: var(--color-text-muted);
  text-decoration: line-through;
  overflow-wrap: anywhere;
}
[data-cui-comp="ui-change-list"] .fui-change-list__arrow {
  color: var(--color-text-subtle, var(--color-text-muted));
}
[data-cui-comp="ui-change-list"] .fui-change-list__to {
  color: var(--color-text);
  text-decoration: none;
  overflow-wrap: anywhere;
}
`
}
