package ui

import (
	"context"
	"strings"

	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core-ui/registry"
	"github.com/DonaldMurillo/gofastr/core-ui/style"
	"github.com/DonaldMurillo/gofastr/core-ui/widget"
	"github.com/DonaldMurillo/gofastr/core-ui/widget/preset"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework/i18nui"
)

// ─── ShortcutSheet ─────────────────────────────────────────────────
//
// The keyboard help sheet: a modal listing an app's shortcuts, each
// chord drawn as keycaps beside what it does, opened by its own chord
// ("?" by default) or a link. Like CommandPalette it returns the
// trigger and a Modal preset: mount the preset once at startup and
// render the trigger in the chrome. Without script the trigger is an
// ordinary link to FallbackHref.

// ShortcutItem is one row: a chord ("Mod+K", "/") and what it does.
type ShortcutItem struct{ Chord, Label string }

// ShortcutSheetConfig configures a ShortcutSheet.
type ShortcutSheetConfig struct {
	// Name is the widget name (default "shortcut-sheet").
	Name string
	// Shortcut opens the sheet (default "?").
	Shortcut string
	// FallbackHref is the trigger's href without script: a same-origin
	// page that lists the same shortcuts. Required.
	FallbackHref string
	// Items are the rows, in order. Required.
	Items []ShortcutItem
	// TriggerLabel names the trigger (default "Keyboard shortcuts").
	TriggerLabel string
	// TriggerVisible draws the trigger as a small link rather than a
	// visually hidden one.
	TriggerVisible bool
	// ExtraAttrs forwards attributes to the trigger; keys the component
	// owns are dropped.
	ExtraAttrs html.Attrs
	Ctx        context.Context
}

// ShortcutSheet returns the trigger and the modal preset.
func ShortcutSheet(cfg ShortcutSheetConfig) (render.HTML, *widget.Builder) {
	if len(cfg.Items) == 0 {
		panic("ui: ShortcutSheet requires Items")
	}
	if !strings.HasPrefix(cfg.FallbackHref, "/") || strings.HasPrefix(cfg.FallbackHref, "//") || strings.ContainsRune(cfg.FallbackHref, '\\') {
		panic("ui: ShortcutSheet FallbackHref must be same-origin and start with /, not " + cfg.FallbackHref)
	}
	ctx := cfg.Ctx
	if ctx == nil {
		ctx = context.Background()
	}
	name := cfg.Name
	if name == "" {
		name = "shortcut-sheet"
	}
	shortcut := cfg.Shortcut
	if shortcut == "" {
		shortcut = "?"
	}
	label := cfg.TriggerLabel
	if label == "" {
		label = i18nui.T(ctx, i18nui.KeyShortcutSheetTitle)
	}
	cls := "fui-visually-hidden"
	if cfg.TriggerVisible {
		cls = "fui-shortcut-sheet__trigger"
	}
	attrs := map[string]string{}
	for k, v := range html.SafeExtraAttrs(cfg.ExtraAttrs, "href", "aria-label", "data-hui-shortcut-click") {
		attrs[k] = v
	}
	attrs["href"] = cfg.FallbackHref
	attrs["class"] = cls
	attrs["data-cui-open"] = name
	attrs["data-hui-shortcut-click"] = shortcut
	attrs["aria-label"] = label
	trigger := render.Tag("a", attrs, render.Text(label))
	if cfg.TriggerVisible {
		trigger = shortcutSheetStyle.WrapHTML(trigger)
	}
	body := &shortcutSheetSlot{
		name:  name,
		title: label,
		close: i18nui.T(ctx, i18nui.KeyCommandPaletteClose),
		items: append([]ShortcutItem{}, cfg.Items...),
	}
	return trigger, preset.Modal(name).Hidden().Role("dialog").LabelledBy(name+"-title").Slot("body", body)
}

// ShortcutList draws the rows a sheet holds, for a page of its own (the
// sheet's FallbackHref) or anywhere else: each chord as keycaps beside
// its label.
func ShortcutList(items []ShortcutItem) render.HTML {
	rows := make([]render.HTML, 0, len(items))
	for _, it := range items {
		rows = append(rows, render.Tag("div", map[string]string{"class": "fui-shortcut-list__row", "data-cui-internal": ""},
			render.Tag("dt", map[string]string{"class": "fui-shortcut-list__label"}, render.Text(it.Label)),
			render.Tag("dd", map[string]string{"class": "fui-shortcut-list__keys"}, ShortcutHint(ShortcutHintConfig{Chord: it.Chord})),
		))
	}
	return shortcutListStyle.WrapHTML(render.Tag("dl", map[string]string{"class": "fui-shortcut-list"}, rows...))
}

type shortcutSheetSlot struct {
	name, title, close string
	items              []ShortcutItem
}

func (s *shortcutSheetSlot) Render() render.HTML {
	head := html.Div(html.DivConfig{Class: "fui-shortcut-sheet__head"},
		html.Heading(html.HeadingConfig{Level: 2, ID: s.name + "-title", Class: "fui-shortcut-sheet__title"}, render.Text(s.title)),
		render.Tag("button", map[string]string{
			"type":            "button",
			"class":           "fui-shortcut-sheet__close",
			"data-cui-action": "close",
			"aria-label":      s.close,
		}, Icon("close", IconConfig{Size: "16"})),
	)
	return shortcutSheetStyle.WrapHTML(html.Div(html.DivConfig{Class: "fui-shortcut-sheet"}, head, ShortcutList(s.items)))
}

var (
	shortcutSheetStyle = registry.RegisterStyle("ui-shortcut-sheet", shortcutSheetCSS)
	shortcutListStyle  = registry.RegisterStyle("ui-shortcut-list", shortcutListCSS)
)

// shortcutListCSS lays each row out as the label and its keycaps at the
// line's end.
func shortcutListCSS(_ style.Theme) string {
	return `:where([data-cui-comp="ui-shortcut-list"]).fui-shortcut-list {
  margin: 0;
  display: grid;
}
[data-cui-comp="ui-shortcut-list"] .fui-shortcut-list__row {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: var(--spacing-md, 8px);
  padding-block: var(--spacing-sm, 4px);
  border-block-end: var(--stroke-thin, 1px) solid var(--color-border);
}
[data-cui-comp="ui-shortcut-list"] .fui-shortcut-list__row:last-child { border-block-end: 0; }
[data-cui-comp="ui-shortcut-list"] .fui-shortcut-list__label {
  font-size: var(--text-sm, 0.875rem);
  color: var(--color-text);
}
[data-cui-comp="ui-shortcut-list"] .fui-shortcut-list__keys { margin: 0; }`
}

func shortcutSheetCSS(_ style.Theme) string {
	return `:where([data-cui-comp="ui-shortcut-sheet"]).fui-shortcut-sheet {
  display: grid;
  gap: var(--spacing-md, 8px);
  padding: var(--spacing-lg, 16px);
  min-inline-size: min(var(--ui-shortcut-sheet-min-inline-size, 26rem), calc(100vw - 2 * var(--spacing-lg, 16px)));
}
[data-cui-comp="ui-shortcut-sheet"] .fui-shortcut-sheet__head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: var(--spacing-md, 8px);
}
[data-cui-comp="ui-shortcut-sheet"] .fui-shortcut-sheet__title {
  margin: 0;
  font-size: var(--text-lg, 1.125rem);
  font-family: var(--font-heading, inherit);
  font-weight: var(--font-weight-semibold, 600);
}
[data-cui-comp="ui-shortcut-sheet"] .fui-shortcut-sheet__close {
  display: inline-grid;
  place-items: center;
  inline-size: var(--ui-shortcut-sheet-close-size, 2rem);
  block-size: var(--ui-shortcut-sheet-close-size, 2rem);
  border: 0;
  border-radius: var(--radii-sm, 6px);
  background: transparent;
  color: var(--color-text-muted);
  cursor: pointer;
}
[data-cui-comp="ui-shortcut-sheet"] .fui-shortcut-sheet__close:hover {
  background: var(--color-surface-soft);
}
:where([data-cui-comp="ui-shortcut-sheet"]).fui-shortcut-sheet__trigger {
  font-size: var(--text-sm, 0.875rem);
  color: var(--color-text-muted);
}`
}
