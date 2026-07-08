package ui

import (
	"strconv"

	"github.com/DonaldMurillo/gofastr/core-ui/component"
	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core-ui/patterns/combobox"
	"github.com/DonaldMurillo/gofastr/core-ui/registry"
	"github.com/DonaldMurillo/gofastr/core-ui/style"
	"github.com/DonaldMurillo/gofastr/core-ui/widget"
	"github.com/DonaldMurillo/gofastr/core-ui/widget/preset"
	"github.com/DonaldMurillo/gofastr/core/render"
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
// Selecting an option does whatever the option's HTML wires it to —
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
	// (Cmd+K on Mac, Ctrl+K elsewhere — the runtime treats either as
	// Mod when matching).
	Shortcut string

	// DebounceMs is the search debounce window. Default 150 (snappier
	// than a generic combobox since results render eagerly).
	DebounceMs int

	// TriggerLabel is the SR-only trigger button text — what AT
	// users hear if they tab to it. Default "Open command palette".
	TriggerLabel string

	// EmptyHTML is the listbox HTML at first paint. Empty (default)
	// renders a placeholder hint.
	EmptyHTML string

	// Commands, when non-empty, renders a static, client-side-filtered
	// command list — no search endpoint needed. Use for a small fixed
	// set (docs/nav links) so the palette works on a serverless export
	// where no RPC handler exists. Takes precedence over RPCPath.
	Commands []PaletteCommand
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
	name := cfg.Name
	if name == "" {
		name = "command-palette"
	}
	placeholder := cfg.Placeholder
	if placeholder == "" {
		placeholder = "Type a command or search…"
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
		triggerLabel = "Open command palette"
	}

	trigger := render.Tag("button", map[string]string{
		"type":                    "button",
		"class":                   "ui-visually-hidden",
		"data-fui-open":           name,
		"data-fui-shortcut-click": shortcut,
		"aria-label":              triggerLabel,
	}, render.Text(triggerLabel))

	slot := &commandPaletteSlot{
		widgetName:  name,
		rpcPath:     cfg.RPCPath,
		placeholder: placeholder,
		debounceMs:  debounce,
		emptyHTML:   cfg.EmptyHTML,
		options:     paletteCommandsToOptions(cfg.Commands),
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
	widgetName  string
	rpcPath     string
	placeholder string
	debounceMs  int
	emptyHTML   string
	options     []combobox.Option
}

func (s *commandPaletteSlot) Render() render.HTML {
	titleID := s.widgetName + "-title"
	signalName := s.widgetName + "-results"
	inputID := s.widgetName + "-input"

	srTitle := html.Heading(html.HeadingConfig{
		Level: 2, ID: titleID, Class: "ui-visually-hidden",
	}, render.Text("Command palette"))

	combo := combobox.Render(combobox.Config{
		ID:          inputID,
		Name:        "q",
		Label:       "Command palette",
		RPCPath:     s.rpcPath,
		SignalName:  signalName,
		DebounceMs:  s.debounceMs,
		Placeholder: s.placeholder,
		EmptyHTML:   s.emptyHTML,
		LabelHidden: true,
		Class:       "ui-cmd-palette__combobox",
		Options:     s.options,
	})

	// Footer hints (visible row of useful shortcuts).
	hints := html.Div(html.DivConfig{Class: "ui-cmd-palette__hints"},
		hintChip("↑↓", "Navigate"),
		hintChip("↵", "Select"),
		hintChip("Esc", "Close"),
	)
	footer := html.Div(html.DivConfig{Class: "ui-cmd-palette__footer", ExtraAttrs: html.Attrs{"aria-hidden": "true"}},
		hints,
	)

	return commandPaletteStyle.WrapHTML(html.Div(html.DivConfig{
		Class: "ui-cmd-palette",
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
  display: block;
  inline-size: min(36rem, 92vw);
  background: var(--color-surface, #fff);
  border-radius: var(--radii-md, 6px);
  box-shadow: 0 16px 48px rgba(0,0,0,0.18);
  overflow: hidden;
}
[data-fui-comp="ui-cmd-palette"] .ui-cmd-palette__combobox {
  max-inline-size: none;
}
[data-fui-comp="ui-cmd-palette"] .ui-cmd-palette__combobox .combobox__form { padding: var(--spacing-md, 12px); border-bottom: 1px solid var(--color-border, #d0d0d8); }
[data-fui-comp="ui-cmd-palette"] .ui-cmd-palette__combobox .combobox__input {
  font-size: var(--text-base, 1.05rem);
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
}
[data-fui-comp="ui-cmd-palette"] .ui-cmd-palette__footer {
  display: flex;
  justify-content: flex-end;
  padding: var(--spacing-sm, 8px) var(--spacing-md, 12px);
  border-top: 1px solid var(--color-border, #d0d0d8);
  background: var(--color-muted, #f7f7f8);
}
[data-fui-comp="ui-cmd-palette"] .ui-cmd-palette__hints {
  display: inline-flex;
  gap: var(--spacing-md, 12px);
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
  font-size: var(--text-xs, 0.7rem);
}
@media (max-width: 540px) {
  [data-fui-comp="ui-cmd-palette"] { inline-size: 100vw; min-block-size: 100dvh; border-radius: 0; }
  [data-fui-comp="ui-cmd-palette"] .ui-cmd-palette__combobox .combobox__listbox { max-block-size: none; }
}
`
}

// _ keeps strconv referenced when DebounceMs is templated; the
// combobox package handles the encoding internally.
var _ = strconv.Itoa
