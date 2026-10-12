package ui

import (
	"context"
	"strconv"
	"strings"

	"github.com/DonaldMurillo/gofastr/core-ui/component"
	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core-ui/registry"
	"github.com/DonaldMurillo/gofastr/core-ui/style"
	"github.com/DonaldMurillo/gofastr/core-ui/urlsafe"
	"github.com/DonaldMurillo/gofastr/core-ui/widget"
	"github.com/DonaldMurillo/gofastr/core-ui/widget/preset"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework/headless"
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
// typically the server emits `<li role="option" data-cui-rpc="..."
// data-cui-push-state="...">…</li>` so picking it navigates or fires
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

	// TriggerLabel is the trigger's accessible name: what AT users
	// hear when they reach it. Default "Open command palette".
	TriggerLabel string

	// Trigger picks how the returned trigger draws. Zero
	// (PaletteTriggerHidden) is a visually hidden link that only
	// carries the shortcut, for chrome that draws its own search
	// button. PaletteTriggerField draws a search field: a magnifier,
	// TriggerText and the shortcut's keycaps, shrinking to an icon
	// button on phones.
	Trigger PaletteTrigger

	// TriggerText is the field trigger's visible text. Default: the
	// placeholder. Ignored by the hidden trigger.
	TriggerText string

	// EmptyHTML is the listbox HTML at first paint. Empty (default)
	// renders a placeholder hint.
	EmptyHTML string

	// Commands, when non-empty, renders a static, client-side-filtered
	// command list, no search endpoint needed. Use for a small fixed
	// set (docs/nav links) so the palette works on a serverless export
	// where no RPC handler exists. Takes precedence over RPCPath.
	Commands []PaletteCommand

	// FallbackHref is the ordinary same-origin destination the trigger
	// navigates to without script: a modal trigger with no navigation
	// path is a button a scriptless reader cannot use. Required; "#"
	// and cross-origin values are refused at render.
	FallbackHref string

	// Ctx carries the per-request context used to resolve i18n labels
	// (placeholder, trigger + dialog titles, hint chips). When nil,
	// English fallbacks apply.
	Ctx context.Context

	// ExtraAttrs forwards additional attributes (data-* test hooks,
	// analytics markers, ARIA overrides) onto the palette's own root
	// (the ui-cmd-palette panel inside the modal; the modal chrome is
	// widget machinery). Keys the component owns are dropped:
	// class, id, and data-cui-*.
	ExtraAttrs html.Attrs
}

// PaletteCommand is one entry in a static command-palette list, or
// one result row a search endpoint answers through PaletteResults.
type PaletteCommand struct {
	Label string // visible text
	Href  string // route to navigate to on pick (data-cui-push-state)
	Meta  string // optional muted secondary text (e.g. the route path)
}

// PaletteTrigger names how CommandPalette draws its trigger.
type PaletteTrigger string

const (
	// PaletteTriggerHidden is a visually hidden link carrying the
	// shortcut. The default.
	PaletteTriggerHidden PaletteTrigger = ""
	// PaletteTriggerField is a visible search field that opens the
	// palette, with the shortcut's keycaps; an icon button on phones.
	PaletteTriggerField PaletteTrigger = "field"
)

// CommandPalette returns the trigger button and a Modal preset for
// the palette. Mount the preset once at startup; render the trigger
// in your global chrome (Sidebar, top nav, etc).
func CommandPalette(cfg CommandPaletteConfig) (render.HTML, *widget.Builder) {
	if cfg.RPCPath == "" && len(cfg.Commands) == 0 {
		panic("ui: CommandPalette requires RPCPath or Commands")
	}
	if cfg.FallbackHref == "" {
		panic("ui: CommandPalette requires FallbackHref — a modal trigger with no ordinary navigation path is a button a scriptless reader cannot use")
	}
	if cfg.FallbackHref == "#" {
		panic("ui: CommandPalette FallbackHref must be a real same-origin destination, not #")
	}
	// Same shape as headless's shared same-origin refusal (island.go
	// checkSameOrigin), plus any backslash anywhere: browsers normalise
	// `\` to `/` at the authority boundary, so `/\evil.example` reads
	// same-origin to this check and resolves cross-origin in the URL
	// parser. urlsafe.OK refuses the byte outright; so does this.
	if !strings.HasPrefix(cfg.FallbackHref, "/") || strings.HasPrefix(cfg.FallbackHref, "//") ||
		strings.ContainsRune(cfg.FallbackHref, '\\') {
		panic("ui: CommandPalette FallbackHref must be same-origin and start with /, not " + cfg.FallbackHref)
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

	// The trigger is an anchor to the fallback: with script the widget
	// runtime's open handler preventDefaults the navigation and opens
	// the modal; without script it is an ordinary link.
	triggerAttrs := map[string]string{
		"href":                    cfg.FallbackHref,
		"class":                   "fui-visually-hidden",
		"data-cui-open":           name,
		"data-hui-shortcut-click": shortcut,
		"aria-label":              triggerLabel,
	}
	var trigger render.HTML
	switch cfg.Trigger {
	case PaletteTriggerHidden:
		trigger = render.Tag("a", triggerAttrs, render.Text(triggerLabel))
	case PaletteTriggerField:
		text := cfg.TriggerText
		if text == "" {
			text = placeholder
		}
		triggerAttrs["class"] = "fui-cmd-trigger"
		trigger = commandPaletteTriggerStyle.WrapHTML(render.Tag("a", triggerAttrs,
			Icon("search", IconConfig{Class: "fui-cmd-trigger__icon"}),
			html.Span(html.TextConfig{Class: "fui-cmd-trigger__text"}, render.Text(text)),
			html.Kbd(html.TextConfig{Class: "fui-cmd-trigger__kbd", ExtraAttrs: html.Attrs{"aria-hidden": "true"}},
				render.Text(shortcutGlyphs(shortcut))),
		))
	default:
		panic("ui: CommandPalette unknown Trigger " + strconv.Quote(string(cfg.Trigger)) + "; use PaletteTriggerHidden or PaletteTriggerField")
	}

	slot := &commandPaletteSlot{
		widgetName:    name,
		rpcPath:       cfg.RPCPath,
		fallbackHref:  cfg.FallbackHref,
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

// shortcutGlyphs spells a chord the way a keycap shows it: "Meta+K"
// reads ⌘K, "Shift+Meta+P" reads ⇧⌘P. Keys the map does not know keep
// their name.
func shortcutGlyphs(chord string) string {
	glyph := map[string]string{"meta": "⌘", "mod": "⌘", "cmd": "⌘", "ctrl": "Ctrl ", "control": "Ctrl ", "alt": "⌥", "option": "⌥", "shift": "⇧"}
	var b strings.Builder
	for _, part := range strings.Split(chord, "+") {
		if g, ok := glyph[strings.ToLower(part)]; ok {
			b.WriteString(g)
			continue
		}
		b.WriteString(strings.ToUpper(part))
	}
	return b.String()
}

// PaletteResults renders the option rows a CommandPalette search
// endpoint answers: one row per command, picked by keyboard or click,
// navigating to its Href. name is the palette's Name ("" for the
// default) and keeps the row ids unique on the page. With no commands
// it renders one disabled row reading empty, or the localized "No
// results" when empty is "".
func PaletteResults(ctx context.Context, name string, cmds []PaletteCommand, empty string) render.HTML {
	if ctx == nil {
		ctx = context.Background()
	}
	if name == "" {
		name = "command-palette"
	}
	if len(cmds) == 0 {
		if empty == "" {
			empty = i18nui.T(ctx, i18nui.KeyHuiComboboxNoResults)
		}
		return render.Tag("li", map[string]string{
			"role":          "option",
			"aria-disabled": "true",
			"class":         "fui-cmd-palette__option",
		}, render.Text(empty))
	}
	prefix := name + "-input-listbox-res-"
	rows := make([]render.HTML, 0, len(cmds))
	for i, c := range cmds {
		attrs := map[string]string{
			"role":       "option",
			"id":         prefix + strconv.Itoa(i),
			"class":      "fui-cmd-palette__option",
			"data-value": c.Label,
		}
		if href := urlsafe.Clean(c.Href, urlsafe.Anchor); href != "" {
			attrs["data-cui-push-state"] = href
		}
		kids := []render.HTML{html.Span(html.TextConfig{Class: "fui-cmd-palette__option-label", ExtraAttrs: html.Attrs{"data-cui-internal": ""}}, render.Text(c.Label))}
		if c.Meta != "" {
			kids = append(kids, html.Span(html.TextConfig{Class: "fui-cmd-palette__option-meta", ExtraAttrs: html.Attrs{"data-cui-internal": ""}}, render.Text(c.Meta)))
		}
		rows = append(rows, render.Tag("li", attrs, kids...))
	}
	return render.Join(rows...)
}

// paletteCommandsToOptions maps the palette's public Commands into the
// combobox's Option shape. data-value defaults to the label.
func paletteCommandsToOptions(cmds []PaletteCommand) []headless.ComboboxOption {
	if len(cmds) == 0 {
		return nil
	}
	opts := make([]headless.ComboboxOption, 0, len(cmds))
	for _, c := range cmds {
		opts = append(opts, headless.ComboboxOption{Label: c.Label, Value: c.Label, Href: c.Href, Meta: c.Meta})
	}
	return opts
}

type commandPaletteSlot struct {
	widgetName    string
	fallbackHref  string
	rpcPath       string
	placeholder   string
	debounceMs    int
	emptyHTML     string
	options       []headless.ComboboxOption
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
		Level: 2, ID: titleID, Class: "fui-visually-hidden",
	}, render.Text(title))

	var island *headless.Island
	if s.rpcPath != "" && len(s.options) == 0 {
		island = &headless.Island{Endpoint: s.rpcPath, Signal: signalName}
	}
	opts := make([]headless.ComboboxOption, len(s.options))
	for i, o := range s.options {
		opts[i] = headless.ComboboxOption{Value: o.Value, Label: o.Label, Href: o.Href, Meta: o.Meta}
	}
	combo := headless.Combobox(headless.ComboboxProps{
		ID:             inputID,
		Name:           "q",
		Label:          title,
		Placeholder:    s.placeholder,
		Island:         island,
		NoScriptAction: s.fallbackHref,
		DebounceMS:     s.debounceMs,
		Options:        opts,
	}, headless.Classes{
		headless.PartRoot:            "fui-cmd-palette__field",
		headless.PartLabel:           "fui-visually-hidden",
		headless.PartComboboxInput:   "fui-cmd-palette__input",
		headless.PartComboboxForm:    "fui-cmd-palette__combobox",
		headless.PartComboboxListbox: "fui-cmd-palette__listbox",
		headless.PartComboboxOption:  "fui-cmd-palette__option",
		headless.PartText:            "fui-cmd-palette__option-label",
		headless.PartComboboxStatus:  "fui-visually-hidden",
	})

	// Visible close control (#325). data-cui-action="close" is the
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
		"class":           "fui-cmd-palette__close",
		"type":            "button",
		"data-cui-action": "close",
		"aria-label":      closeLabel,
	}, Icon("close", IconConfig{Class: "fui-cmd-palette__close-icon"}))

	// Footer hints (visible row of useful shortcuts). aria-hidden moved
	// from the footer onto the hints row: the hints stay decorative,
	// but the footer now hosts the close button, which must remain in
	// the accessibility tree and the focus order.
	hints := html.Div(html.DivConfig{
		Class:      "fui-cmd-palette__hints",
		ExtraAttrs: html.Attrs{"aria-hidden": "true"},
	},
		hintChip("↑↓", s.navigateLabel),
		hintChip("↵", s.selectLabel),
		hintChip("Esc", closeLabel),
	)
	footer := html.Div(html.DivConfig{Class: "fui-cmd-palette__footer"},
		closeBtn,
		hints,
	)

	return commandPaletteStyle.WrapHTML(html.Div(html.DivConfig{
		Class:      "fui-cmd-palette cui-slot-bare",
		ExtraAttrs: s.extraAttrs,
	}, srTitle, combo, footer))
}

func hintChip(key, label string) render.HTML {
	return html.Span(html.TextConfig{Class: "fui-cmd-palette__hint"},
		html.Kbd(html.TextConfig{Class: "fui-cmd-palette__kbd"}, render.Text(key)),
		html.Span(html.TextConfig{Class: "fui-cmd-palette__hint-label"}, render.Text(label)),
	)
}

var _ component.Component = (*commandPaletteSlot)(nil)

var commandPaletteStyle = registry.RegisterStyle("ui-cmd-palette", commandPaletteCSS)

func commandPaletteCSS(_ style.Theme) string {
	return `[data-cui-comp="ui-cmd-palette"] {
  display: flex;
  flex-direction: column;
  /* Knobs: --ui-cmd-palette-width (36rem) is the panel's inline size
     (capped by the viewport); --ui-cmd-palette-listbox-max-height
     (24rem) caps the scrolling command list (capped by half the
     viewport). The elevation reads the theme's dialog-tier shadow. */
  inline-size: min(var(--ui-cmd-palette-width, 36rem), 92vw);
  /* Bound the dialog to the viewport (#325). The modal chrome centers
     the panel in a fixed wrapper padded by --spacing-lg on all sides,
     so the cap is the viewport minus both paddings. Without it a list
     longer than the screen grows the centered panel past the top edge,
     where clipped content is unreachable (no page scroll under a modal
     scroll lock). */
  max-block-size: calc(100dvh - 2 * var(--spacing-lg, 16px));
  background: var(--color-surface, #fff);
  border-radius: var(--radii-md, 8px);
  box-shadow: var(--shadow-xl, 0 16px 48px rgba(0,0,0,0.18));
  overflow: hidden;
}
[data-cui-comp="ui-cmd-palette"] .fui-cmd-palette__combobox {
  max-inline-size: none;
  /* Flex child of the palette column AND flex container for the form +
     listbox. min-block-size: 0 lets it shrink below its content so the
     listbox (not the whole dialog) absorbs long command lists. */
  display: flex;
  flex-direction: column;
  min-block-size: 0;
}
[data-cui-comp="ui-cmd-palette"] .fui-cmd-palette__field {
  /* The combobox root between the no-script form and the listbox: a
     flex column that may shrink, or the listbox grows to its content
     and the dialog clips the tail on a phone, where the listbox has no
     height cap of its own. */
  display: flex;
  flex-direction: column;
  flex: 1 1 auto;
  min-block-size: 0;
}
[data-cui-comp="ui-cmd-palette"] .fui-cmd-palette__combobox:has(> .fui-cmd-palette__input) {
  /* The carrier row (the div directly wrapping the input — the
     no-script FORM also wears the combobox class, so :has() picks
     the row): the search field's padding and seam. */
  padding: var(--spacing-md, 8px);
  border-bottom: var(--stroke-thin, 1px) solid var(--color-border, #d0d0d8);
  flex: 0 0 auto;
}
[data-cui-comp="ui-cmd-palette"] .fui-cmd-palette__input {
  font-size: var(--text-base, 1rem);
  border: none;
  background: transparent;
  padding: 0;
  min-block-size: var(--spacing-touch-target, 44px);
  inline-size: 100%;
  box-sizing: border-box;
}
[data-cui-comp="ui-cmd-palette"] .fui-cmd-palette__input:focus-visible {
  box-shadow: none;
  outline: none;
}
[data-cui-comp="ui-cmd-palette"] .fui-cmd-palette__listbox {
  position: static;
  margin: 0;
  border: none;
  border-radius: 0;
  box-shadow: none;
  max-block-size: min(50vh, var(--ui-cmd-palette-listbox-max-height, 24rem));
  /* The only scrolling region: Takes whatever space the bounded dialog
     has left. overflow-y: auto does double duty — it scrolls AND, per
     flexbox §4.5, zeroes the item's automatic minimum size, so the
     list shrinks into the remaining space instead of pushing the form
     or footer out of the dialog. (The combobox wrapper above needs its
     explicit min-block-size: 0 because its overflow is visible.) */
  flex: 1 1 auto;
  overflow-y: auto;
}
[data-cui-comp="ui-cmd-palette"] .fui-cmd-palette__listbox {
  list-style: none;
  padding: var(--spacing-sm, 4px);
}
[data-cui-comp="ui-cmd-palette"] .fui-cmd-palette__option[hidden] {
  /* Out-specifies the option rule below, which would otherwise paint
     the rows a static list filtered away. */
  display: none;
}
:where([data-cui-comp="ui-cmd-palette"]) .fui-cmd-palette__option {
  display: flex;
  align-items: baseline;
  justify-content: space-between;
  gap: var(--spacing-md, 8px);
  padding: calc(var(--spacing-sm, 4px) * 2) var(--spacing-md, 8px);
  border-radius: var(--radii-sm, 6px);
  font-size: var(--text-sm, 0.875rem);
  color: var(--color-text, #09090B);
  cursor: pointer;
  user-select: none;
}
[data-cui-comp="ui-cmd-palette"] .fui-cmd-palette__option.is-active {
  background: var(--color-surface-soft, #f1f1f3);
}
[data-cui-comp="ui-cmd-palette"] .fui-cmd-palette__option[aria-disabled="true"] {
  color: var(--color-text-muted, #6b7280);
  cursor: default;
}
[data-cui-comp="ui-cmd-palette"] .fui-cmd-palette__option > span:not(:first-child) {
  /* The Meta text: a kind or a path, quiet beside the label. */
  color: var(--color-text-muted, #6b7280);
  font-size: var(--text-xs, 0.75rem);
  flex: 0 0 auto;
}
@media (pointer: coarse) {
  :where([data-cui-comp="ui-cmd-palette"]) .fui-cmd-palette__option {
    min-block-size: var(--spacing-touch-target, 44px);
    align-items: center;
  }
}
[data-cui-comp="ui-cmd-palette"] .fui-cmd-palette__footer {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: var(--spacing-md, 8px);
  padding: var(--spacing-sm, 4px) var(--spacing-md, 8px);
  border-top: var(--stroke-thin, 1px) solid var(--color-border, #d0d0d8);
  background: var(--color-surface-soft, #f7f7f8);
  flex: 0 0 auto;
}
[data-cui-comp="ui-cmd-palette"] .fui-cmd-palette__close {
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
  border-radius: var(--radii-sm, 6px);
}
[data-cui-comp="ui-cmd-palette"] .fui-cmd-palette__close:hover {
  background: var(--color-surface-soft, #f4f4f5);
  color: var(--color-text, #18181b);
}
[data-cui-comp="ui-cmd-palette"] .fui-cmd-palette__close:focus-visible {
  outline: var(--stroke-focus, 2px) solid var(--color-text-subtle);
  outline-offset: var(--stroke-focus-offset, 2px);
}
[data-cui-comp="ui-cmd-palette"] .fui-cmd-palette__hints {
  display: inline-flex;
  gap: var(--spacing-md, 8px);
  font-size: var(--text-xs, 0.75rem);
  color: var(--color-text-muted, #6b7280);
}
[data-cui-comp="ui-cmd-palette"] .fui-cmd-palette__hint {
  display: inline-flex;
  align-items: center;
  gap: var(--spacing-sm, 4px);
}
[data-cui-comp="ui-cmd-palette"] .fui-cmd-palette__kbd {
  font-family: var(--font-mono, ui-monospace, monospace);
  padding: 1px calc(var(--spacing-sm, 4px) * 1.5);
  border: var(--stroke-thin, 1px) solid var(--color-border, #d0d0d8);
  border-bottom-width: var(--stroke-thick, 2px);
  border-radius: var(--radii-sm, 6px);
  background: var(--color-surface, #fff);
  font-size: var(--text-xs, 0.75rem);
}
@media (max-width: 540px) {
  /* Full-screen sheet, bounded to exactly the dynamic viewport: the
     wrapper's --spacing-lg padding is cancelled by centering overflow
     (the 100vw/100dvh panel spans the padding box edge to edge), and
     the listbox cap is dropped because the palette cap now governs —
     the list takes every remaining pixel and scrolls inside. */
  [data-cui-comp="ui-cmd-palette"] { inline-size: 100vw; block-size: 100dvh; min-block-size: 100dvh; max-block-size: 100dvh; border-radius: 0; }
  [data-cui-comp="ui-cmd-palette"] .fui-cmd-palette__listbox { max-block-size: none; }
}
`
}

var commandPaletteTriggerStyle = registry.RegisterStyle("ui-cmd-palette-trigger", commandPaletteTriggerCSS)

func commandPaletteTriggerCSS(_ style.Theme) string {
	return `[data-cui-comp="ui-cmd-palette-trigger"] {
  /* Knobs: --ui-cmd-trigger-width (16rem) and --ui-cmd-trigger-height
     (36px) size the field at and above md; --ui-cmd-trigger-icon-size
     (1rem) sizes the magnifier. */
  display: inline-flex;
  align-items: center;
  gap: var(--spacing-sm, 4px);
  inline-size: var(--ui-cmd-trigger-width, 16rem);
  max-inline-size: 100%;
  min-block-size: var(--ui-cmd-trigger-height, 36px);
  padding-inline: calc(var(--spacing-sm, 4px) * 2.5);
  box-sizing: border-box;
  border: var(--stroke-thin, 1px) solid var(--color-border, #d0d0d8);
  border-radius: var(--radii-md, 8px);
  background: var(--color-surface, #fff);
  color: var(--color-text-muted, #6b7280);
  font: inherit;
  font-size: var(--text-sm, 0.875rem);
  text-decoration: none;
  cursor: pointer;
}
[data-cui-comp="ui-cmd-palette-trigger"]:hover {
  color: var(--color-text, #18181b);
}
[data-cui-comp="ui-cmd-palette-trigger"]:focus-visible {
  outline: var(--stroke-focus, 2px) solid var(--color-text-subtle);
  outline-offset: var(--stroke-focus-offset, 2px);
}
[data-cui-comp="ui-cmd-palette-trigger"] .fui-cmd-trigger__icon {
  flex: 0 0 auto;
  inline-size: var(--ui-cmd-trigger-icon-size, 1rem);
  block-size: var(--ui-cmd-trigger-icon-size, 1rem);
}
[data-cui-comp="ui-cmd-palette-trigger"] .fui-cmd-trigger__text {
  flex: 1 1 auto;
  min-inline-size: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
[data-cui-comp="ui-cmd-palette-trigger"] .fui-cmd-trigger__kbd {
  flex: 0 0 auto;
  font-family: var(--font-mono, ui-monospace, monospace);
  font-size: var(--text-xs, 0.75rem);
  padding: 1px calc(var(--spacing-sm, 4px) * 1.5);
  border: var(--stroke-thin, 1px) solid var(--color-border, #d0d0d8);
  border-radius: var(--radii-sm, 6px);
  background: var(--color-surface-soft, #f7f7f8);
}
@media (max-width: 47.98rem) {
  /* Phones: an icon button with the 44px tap floor. The text stays
     out of the box; the accessible name rides aria-label. */
  [data-cui-comp="ui-cmd-palette-trigger"] {
    inline-size: var(--spacing-touch-target, 44px);
    min-block-size: var(--spacing-touch-target, 44px);
    justify-content: center;
    padding: 0;
  }
  [data-cui-comp="ui-cmd-palette-trigger"] .fui-cmd-trigger__text,
  [data-cui-comp="ui-cmd-palette-trigger"] .fui-cmd-trigger__kbd { display: none; }
}
`
}

// _ keeps strconv referenced when DebounceMs is templated; the
// combobox package handles the encoding internally.
var _ = strconv.Itoa
