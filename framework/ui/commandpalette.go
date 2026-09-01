package ui

import (
	"context"
	"strconv"

	"github.com/DonaldMurillo/gofastr/core-ui/component"
	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core-ui/patterns/combobox"
	"github.com/DonaldMurillo/gofastr/core-ui/registry"
	"github.com/DonaldMurillo/gofastr/core-ui/style"
	"github.com/DonaldMurillo/gofastr/core-ui/widget"
	"github.com/DonaldMurillo/gofastr/core-ui/widget/preset"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework/i18nui"
)

// ─── CommandPalette ─────────────────────────────────────────────────
//
// A Ctrl/Cmd+K-triggered overlay combining a Modal preset (role=dialog,
// aria-modal, focus trap, Escape close, backdrop) with an embedded
// combobox (debounced server-fuzzy-search, keyboard nav, listbox
// selection).
//
// The component returns:
//   - trigger: an SR-only button that opens the palette AND carries
//     the global shortcut binding (Meta+K by default).
//   - preset: a *widget.Builder the host mounts once at startup.
//
// Selecting an option does whatever the option's HTML wires it to:
// typically the server emits `<li role="option" data-fui-rpc="..."
// data-fui-push-state="...">…</li>` so picking it navigates or fires
// an action. The combobox runtime picks the option's data-value or
// textContent as the input replacement.

// CommandPaletteConfig configures the command palette.
type CommandPaletteConfig struct {
	// Name uniquely identifies the modal widget. Default
	// "command-palette".
	Name string

	// RPCPath is the search endpoint. The handler receives the
	// query string and returns `<li role="option">…</li>` fragments
	// to swap into the listbox. Required unless Commands is set.
	RPCPath string

	// Placeholder is the input placeholder. Default
	// "Type a command or search…".
	Placeholder string

	// Shortcut is the chord that opens the palette. Default "Meta+K"
	// (Cmd+K on Mac, Ctrl+K elsewhere: the runtime treats either as
	// Mod when matching).
	Shortcut string

	// DebounceMs is the search debounce window. Default 150 (snappier
	// than a generic combobox since results render eagerly).
	DebounceMs int

	// TriggerLabel is the SR-only trigger button text: what AT
	// users hear if they tab to it. Default "Open command palette".
	TriggerLabel string

	// EmptyHTML is the listbox HTML at first paint. Empty (default)
	// renders a placeholder hint.
	EmptyHTML string

	// Commands, when non-empty, renders a static, client-side-filtered
	// command list, no search endpoint needed. Use for a small fixed
	// set (docs/nav links) so the palette works on a serverless export
	// where no RPC handler exists. Takes precedence over RPCPath.
	Commands []PaletteCommand

	// Ctx carries the per-request context used to resolve i18n labels
	// (placeholder, trigger + dialog titles, hint chips). When nil,
	// English fallbacks apply.
	Ctx context.Context

	// ExtraAttrs forwards additional attributes (data-* test hooks,
	// analytics markers, ARIA overrides) onto the palette's own root
	// (the ui-cmd-palette panel inside the modal; the modal chrome is
	// widget machinery). Keys the component owns are dropped:
	// class, id, and data-fui-*.
	ExtraAttrs html.Attrs
}

// PaletteCommand is one entry in a static command-palette list.
type PaletteCommand struct {
	Label string // visible text
	Href  string // route to navigate to on pick (data-fui-push-state)
	Meta  string // optional muted secondary text (e.g. the route path)
}

// CommandPalette returns the trigger button and a Modal preset for
// the palette. Mount the preset once at startup; render the trigger
// in your global chrome (Sidebar, top nav, etc).
func CommandPalette(cfg CommandPaletteConfig) (render.HTML, *widget.Builder) {
	if cfg.RPCPath == "" && len(cfg.Commands) == 0 {
		panic("ui: CommandPalette requires RPCPath or Commands")
	}
	ctx := cfg.Ctx
	if ctx == nil {
		ctx = context.Background()
	}
	name := cfg.Name
	if name == "" {
		name = "command-palette"
	}
	placeholder := cfg.Placeholder
	if placeholder == "" {
		placeholder = i18nui.T(ctx, i18nui.KeyCommandPalettePlaceholder)
	}
	shortcut := cfg.Shortcut
	if shortcut == "" {
		shortcut = "Meta+K"
	}
	debounce := cfg.DebounceMs
	if debounce <= 0 {
		debounce = 150
	}
	triggerLabel := cfg.TriggerLabel
	if triggerLabel == "" {
		triggerLabel = i18nui.T(ctx, i18nui.KeyCommandPaletteOpen)
	}

	trigger := render.Tag("button", map[string]string{
		"type":                    "button",
		"class":                   "ui-visually-hidden",
		"data-fui-open":           name,
		"data-fui-shortcut-click": shortcut,
		"aria-label":              triggerLabel,
	}, render.Text(triggerLabel))

	slot := &commandPaletteSlot{
		widgetName:    name,
		rpcPath:       cfg.RPCPath,
		placeholder:   placeholder,
		debounceMs:    debounce,
		emptyHTML:     cfg.EmptyHTML,
		options:       paletteCommandsToOptions(cfg.Commands),
		title:         i18nui.T(ctx, i18nui.KeyCommandPaletteTitle),
		navigateLabel: i18nui.T(ctx, i18nui.KeyCommandPaletteNavigate),
		selectLabel:   i18nui.T(ctx, i18nui.KeyCommandPaletteSelect),
		closeLabel:    i18nui.T(ctx, i18nui.KeyCommandPaletteClose),
		extraAttrs:    html.SafeExtraAttrs(cfg.ExtraAttrs),
	}
	b := preset.Modal(name).
		Hidden().
		Role("dialog").
		LabelledBy(name+"-title").
		Slot("body", slot)
	return trigger, b
}

// paletteCommandsToOptions maps the palette's public Commands into the
// combobox's Option shape. data-value defaults to the label.
func paletteCommandsToOptions(cmds []PaletteCommand) []combobox.Option {
	if len(cmds) == 0 {
		return nil
	}
	opts := make([]combobox.Option, 0, len(cmds))
	for _, c := range cmds {
		opts = append(opts, combobox.Option{Label: c.Label, Value: c.Label, Href: c.Href, Meta: c.Meta})
	}
	return opts
}

type commandPaletteSlot struct {
	widgetName    string
	rpcPath       string
	placeholder   string
	debounceMs    int
	emptyHTML     string
	options       []combobox.Option
	title         string
	navigateLabel string
	selectLabel   string
	closeLabel    string
	extraAttrs    html.Attrs
}

func (s *commandPaletteSlot) Render() render.HTML {
	titleID := s.widgetName + "-title"
	signalName := s.widgetName + "-results"
	inputID := s.widgetName + "-input"
	// title is resolved at CommandPalette() call time and stashed on
	// the slot. Slots constructed directly (e.g. in tests) may leave it
	// empty: fall back to the localized default so combobox.Render's
	// non-empty Label contract holds.
	title := s.title
	if title == "" {
		title = i18nui.T(context.Background(), i18nui.KeyCommandPaletteTitle)
	}
	// closeLabel is resolved alongside title at CommandPalette() call
	// time; same direct-construction fallback for slots built in tests.
	closeLabel := s.closeLabel
	if closeLabel == "" {
		closeLabel = i18nui.T(context.Background(), i18nui.KeyCommandPaletteClose)
	}

	srTitle := html.Heading(html.HeadingConfig{
		Level: 2, ID: titleID, Class: "ui-visually-hidden",
	}, render.Text(title))

	combo := combobox.Render(combobox.Config{
		ID:          inputID,
		Label:       title,
		Name:        "q",
		RPCPath:     s.rpcPath,
		SignalName:  signalName,
		DebounceMs:  s.debounceMs,
		Placeholder: s.placeholder,
		EmptyHTML:   s.emptyHTML,
		LabelHidden: true,
		Class:       "ui-cmd-palette__combobox",
		Options:     s.options,
	})

	// Visible close control (#325). data-fui-action="close" is the
	// framework's declarative widget-dismiss hook (same wiring as the
	// section-menu drawer): the widget runtime's own click handler
	// dismisses the palette and restores focus to the trigger, so the
	// button needs no bespoke JS. Rendered at every breakpoint, not
	// just mobile: a control that appears only under a breakpoint is a
	// discoverability inconsistency, and the Esc hint chip cannot be
	// followed on a touch device. Icon-only: named via aria-label,
	// the icon itself is decorative (aria-hidden) — the same
	// convention as Banner dismiss and the section-menu drawer close.
	closeBtn := render.Tag("button", map[string]string{
		"class":           "ui-cmd-palette__close",
		"type":            "button",
		"data-fui-action": "close",
		"aria-label":      closeLabel,
	}, Icon("close", IconConfig{Class: "ui-cmd-palette__close-icon"}))

	// Footer hints (visible row of useful shortcuts). aria-hidden moved
	// from the footer onto the hints row: the hints stay decorative,
	// but the footer now hosts the close button, which must remain in
	// the accessibility tree and the focus order.
	hints := html.Div(html.DivConfig{
		Class:      "ui-cmd-palette__hints",
		ExtraAttrs: html.Attrs{"aria-hidden": "true"},
	},
		hintChip("↑↓", s.navigateLabel),
		hintChip("↵", s.selectLabel),
		hintChip("Esc", closeLabel),
	)
	footer := html.Div(html.DivConfig{Class: "ui-cmd-palette__footer"},
		closeBtn,
		hints,
	)

	return commandPaletteStyle.WrapHTML(html.Div(html.DivConfig{
		Class:      "ui-cmd-palette",
		ExtraAttrs: s.extraAttrs,
	}, srTitle, combo, footer))
}

func hintChip(key, label string) render.HTML {
	return html.Span(html.TextConfig{Class: "ui-cmd-palette__hint"},
		html.Kbd(html.TextConfig{Class: "ui-cmd-palette__kbd"}, render.Text(key)),
		html.Span(html.TextConfig{Class: "ui-cmd-palette__hint-label"}, render.Text(label)),
	)
}

var _ component.Component = (*commandPaletteSlot)(nil)

var commandPaletteStyle = registry.RegisterStyle("ui-cmd-palette", commandPaletteCSS)

func commandPaletteCSS(_ style.Theme) string {
	return `[data-fui-comp="ui-cmd-palette"] {
  display: flex;
  flex-direction: column;
  inline-size: min(36rem, 92vw);
  /* Bound the dialog to the viewport (#325). The modal chrome centers
     the panel in a fixed wrapper padded by --spacing-lg on all sides,
     so the cap is the viewport minus both paddings. Without it a list
     longer than the screen grows the centered panel past the top edge,
     where clipped content is unreachable (no page scroll under a modal
     scroll lock). */
  max-block-size: calc(100dvh - 2 * var(--spacing-lg, 16px));
  background: var(--color-surface, #fff);
  border-radius: var(--radii-md, 8px);
  box-shadow: 0 16px 48px rgba(0,0,0,0.18);
  overflow: hidden;
}
[data-fui-comp="ui-cmd-palette"] .ui-cmd-palette__combobox {
  max-inline-size: none;
  /* Flex child of the palette column AND flex container for the form +
     listbox. min-block-size: 0 lets it shrink below its content so the
     listbox (not the whole dialog) absorbs long command lists. */
  display: flex;
  flex-direction: column;
  min-block-size: 0;
}
[data-fui-comp="ui-cmd-palette"] .ui-cmd-palette__combobox .combobox__form { padding: var(--spacing-md, 8px); border-bottom: 1px solid var(--color-border, #d0d0d8); flex: 0 0 auto; }
[data-fui-comp="ui-cmd-palette"] .ui-cmd-palette__combobox .combobox__input {
  font-size: var(--text-base, 1rem);
  border: none;
  background: transparent;
  padding: 0;
  min-height: var(--spacing-touch-target, 44px);
}
[data-fui-comp="ui-cmd-palette"] .ui-cmd-palette__combobox .combobox__input:focus-visible {
  box-shadow: none;
  outline: none;
}
[data-fui-comp="ui-cmd-palette"] .ui-cmd-palette__combobox .combobox__listbox {
  position: static;
  margin: 0;
  border: none;
  border-radius: 0;
  box-shadow: none;
  max-block-size: min(50vh, 24rem);
  /* The only scrolling region: takes whatever space the bounded dialog
     has left. overflow-y: auto does double duty — it scrolls AND, per
     flexbox §4.5, zeroes the item's automatic minimum size, so the
     list shrinks into the remaining space instead of pushing the form
     or footer out of the dialog. (The combobox wrapper above needs its
     explicit min-block-size: 0 because its overflow is visible.) */
  flex: 1 1 auto;
  overflow-y: auto;
}
[data-fui-comp="ui-cmd-palette"] .ui-cmd-palette__footer {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: var(--spacing-md, 8px);
  padding: var(--spacing-sm, 4px) var(--spacing-md, 8px);
  border-top: 1px solid var(--color-border, #d0d0d8);
  background: var(--color-muted, #f7f7f8);
  flex: 0 0 auto;
}
[data-fui-comp="ui-cmd-palette"] .ui-cmd-palette__close {
  flex: 0 0 auto;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  /* WCAG 2.5.5 — the dismiss X needs the 44px tap floor; negative
     block margins cancel the footer's --spacing-sm padding so the
     visual footer stays one row tall (same trick as Banner dismiss). */
  min-block-size: var(--spacing-touch-target, 44px);
  min-inline-size: var(--spacing-touch-target, 44px);
  margin-block: calc(-1 * var(--spacing-sm, 4px));
  background: transparent;
  border: 0;
  color: var(--color-text-muted, #6b7280);
  cursor: pointer;
  border-radius: var(--radii-sm, 4px);
}
[data-fui-comp="ui-cmd-palette"] .ui-cmd-palette__close:hover {
  background: var(--color-surface-soft, #f4f4f5);
  color: var(--color-text, #18181b);
}
[data-fui-comp="ui-cmd-palette"] .ui-cmd-palette__close:focus-visible {
  outline: 2px solid var(--color-primary, #4F46E5);
  outline-offset: 2px;
}
[data-fui-comp="ui-cmd-palette"] .ui-cmd-palette__hints {
  display: inline-flex;
  gap: var(--spacing-md, 8px);
  font-size: var(--text-xs, 0.75rem);
  color: var(--color-text-muted, #6b7280);
}
[data-fui-comp="ui-cmd-palette"] .ui-cmd-palette__hint {
  display: inline-flex;
  align-items: center;
  gap: var(--spacing-sm, 4px);
}
[data-fui-comp="ui-cmd-palette"] .ui-cmd-palette__kbd {
  font-family: var(--fonts-mono, ui-monospace, monospace);
  padding: 1px 6px;
  border: 1px solid var(--color-border, #d0d0d8);
  border-bottom-width: 2px;
  border-radius: var(--radii-sm, 4px);
  background: var(--color-surface, #fff);
  font-size: var(--text-xs, 0.75rem);
}
@media (max-width: 540px) {
  /* Full-screen sheet, bounded to exactly the dynamic viewport: the
     wrapper's --spacing-lg padding is cancelled by centering overflow
     (the 100vw/100dvh panel spans the padding box edge to edge), and
     the listbox cap is dropped because the palette cap now governs —
     the list takes every remaining pixel and scrolls inside. */
  [data-fui-comp="ui-cmd-palette"] { inline-size: 100vw; block-size: 100dvh; min-block-size: 100dvh; max-block-size: 100dvh; border-radius: 0; }
  [data-fui-comp="ui-cmd-palette"] .ui-cmd-palette__combobox .combobox__listbox { max-block-size: none; }
}
`
}

// _ keeps strconv referenced when DebounceMs is templated; the
// combobox package handles the encoding internally.
var _ = strconv.Itoa
