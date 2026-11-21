package ui

import (
	"context"
	"strconv"
	"strings"

	"github.com/DonaldMurillo/gofastr/core-ui/component"
	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core-ui/registry"
	"github.com/DonaldMurillo/gofastr/core-ui/style"
	"github.com/DonaldMurillo/gofastr/core-ui/widget"
	"github.com/DonaldMurillo/gofastr/core-ui/widget/preset"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework/headless"
	"github.com/DonaldMurillo/gofastr/framework/i18nui"
)

// CommandPaletteConfig configures the command palette.
type CommandPaletteConfig struct {
	// Name identifies the modal widget. Default "command-palette".
	Name string
	// RPCPath is the search endpoint answering option rows. Required unless Commands is set.
	RPCPath string
	// Placeholder is the input placeholder. Default "Type a command or search…".
	Placeholder string
	// Shortcut is the chord that opens the palette. Default "Meta+K".
	Shortcut string
	// DebounceMs is the search debounce window. Default 150.
	DebounceMs int
	// TriggerLabel is the trigger's accessible name. Default "Open command palette".
	TriggerLabel string
	// Trigger picks how the trigger draws. Default PaletteTriggerHidden.
	Trigger PaletteTrigger
	// TriggerText is the field trigger's visible text. Default: the placeholder.
	TriggerText string
	// EmptyHTML is the listbox HTML at first paint.
	EmptyHTML string
	// Commands is a static, client-filtered list; it takes precedence over RPCPath.
	Commands []PaletteCommand
	// Search adds battery/sitesearch full-text results after the Commands; it requires Commands.
	Search bool
	// SearchPage is the same-origin search page the last result row opens; it requires Search.
	SearchPage string
	// FallbackHref is the same-origin page the trigger links to without script. Required.
	FallbackHref string
	// Ctx resolves the trigger CommandPalette returns. Nil uses English.
	Ctx context.Context
	// ExtraAttrs land on the palette panel; class, id and data-cui-* are dropped.
	ExtraAttrs html.Attrs
}

// PaletteCommand is one entry in a static command-palette list, or
// one result row a search endpoint answers through PaletteResults.
type PaletteCommand struct {
	Label string
	Href  string
	Meta  string
}

// PaletteTrigger names how CommandPalette draws its trigger.
type PaletteTrigger string

const (
	// PaletteTriggerHidden is a visually hidden link carrying the shortcut.
	PaletteTriggerHidden PaletteTrigger = ""
	// PaletteTriggerField is a visible search field with the shortcut's keycaps.
	PaletteTriggerField PaletteTrigger = "field"
)

// CommandPalette returns a trigger resolved with cfg.Ctx and the Modal
// preset to mount once. It panics on a config no palette can draw.
func CommandPalette(cfg CommandPaletteConfig) (render.HTML, *widget.Builder) {
	return commandPalette(cfg, nil)
}

// CommandPaletteFunc is CommandPalette with the body built per request
// by build; the widget's Name and Shortcut come from
// build(context.Background()).
func CommandPaletteFunc(build func(ctx context.Context) CommandPaletteConfig) *widget.Builder {
	if build == nil {
		panic("ui.CommandPaletteFunc: build is nil")
	}
	_, b := commandPalette(build(context.Background()), build)
	return b
}

func checkCommandPalette(cfg CommandPaletteConfig) {
	if cfg.RPCPath == "" && len(cfg.Commands) == 0 {
		panic("ui: CommandPalette requires RPCPath or Commands")
	}
	if cfg.Search && len(cfg.Commands) == 0 {
		panic("ui: CommandPalette Search requires Commands — they are the list a reader sees before typing")
	}
	if cfg.SearchPage != "" && (!cfg.Search || !strings.HasPrefix(cfg.SearchPage, "/") ||
		strings.HasPrefix(cfg.SearchPage, "//") || strings.ContainsAny(cfg.SearchPage, "\\?#")) {
		panic("ui: CommandPalette SearchPage needs Search and a same-origin path starting with /, not " + strconv.Quote(cfg.SearchPage))
	}
	if cfg.FallbackHref == "" {
		panic("ui: CommandPalette requires FallbackHref — a modal trigger with no ordinary navigation path is a button a scriptless reader cannot use")
	}
	if cfg.FallbackHref == "#" {
		panic("ui: CommandPalette FallbackHref must be a real same-origin destination, not #")
	}
	if !strings.HasPrefix(cfg.FallbackHref, "/") || strings.HasPrefix(cfg.FallbackHref, "//") ||
		strings.ContainsRune(cfg.FallbackHref, '\\') {
		panic("ui: CommandPalette FallbackHref must be same-origin and start with /, not " + cfg.FallbackHref)
	}
}

// CommandPaletteTrigger renders the palette's trigger with ctx, not
// cfg.Ctx, resolving its default words.
func CommandPaletteTrigger(ctx context.Context, cfg CommandPaletteConfig) render.HTML {
	checkCommandPalette(cfg)
	if ctx == nil {
		ctx = context.Background()
	}
	name := cfg.Name
	if name == "" {
		name = "command-palette"
	}
	placeholder := palettePlaceholder(ctx, cfg)
	shortcut := cfg.Shortcut
	if shortcut == "" {
		shortcut = "Meta+K"
	}
	triggerLabel := cfg.TriggerLabel
	if triggerLabel == "" {
		triggerLabel = i18nui.CommandPaletteOpen.Text(ctx)
	}
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
			headless.Own(Icon("search", IconConfig{Class: "fui-cmd-trigger__icon"})),
			html.Span(html.TextConfig{Class: "fui-cmd-trigger__text", ExtraAttrs: html.Attrs{"data-cui-internal": ""}}, render.Text(text)),
			html.Kbd(html.TextConfig{Class: "fui-cmd-trigger__kbd", ExtraAttrs: html.Attrs{"aria-hidden": "true", "data-cui-internal": ""}},
				render.Text(shortcutGlyphs(shortcut))),
		))
	default:
		panic("ui: CommandPalette unknown Trigger " + strconv.Quote(string(cfg.Trigger)) + "; use PaletteTriggerHidden or PaletteTriggerField")
	}
	return trigger
}

func commandPalette(cfg CommandPaletteConfig, build func(context.Context) CommandPaletteConfig) (render.HTML, *widget.Builder) {
	trigger := CommandPaletteTrigger(cfg.Ctx, cfg)
	name := cfg.Name
	if name == "" {
		name = "command-palette"
	}
	slot := requestSlot{build: func(ctx context.Context) component.Component {
		c := cfg
		if build != nil {
			c = build(ctx)
			checkCommandPalette(c)
		}
		debounce := c.DebounceMs
		if debounce <= 0 {
			debounce = 150
		}
		return &commandPaletteSlot{
			widgetName:    name,
			rpcPath:       c.RPCPath,
			fallbackHref:  c.FallbackHref,
			placeholder:   palettePlaceholder(ctx, c),
			debounceMs:    debounce,
			emptyHTML:     c.EmptyHTML,
			search:        c.Search,
			options:       paletteCommandsToOptions(c.Commands),
			title:         i18nui.CommandPaletteTitle.Text(ctx),
			navigateLabel: i18nui.CommandPaletteNavigate.Text(ctx),
			selectLabel:   i18nui.CommandPaletteSelect.Text(ctx),
			closeLabel:    i18nui.CommandPaletteClose.Text(ctx),
			escKey:        i18nui.KeyEsc.Text(ctx),
			loadingLabel:  i18nui.HuiComboboxLoading.Text(ctx),
			emptyLabel:    i18nui.HuiComboboxNoResults.Text(ctx),
			searchPage:    c.SearchPage,
			allLabel:      i18nui.SearchResultsAll.Text(ctx),
			words:         StringsFor(ctx),
			extraAttrs:    html.SafeExtraAttrs(c.ExtraAttrs),
		}
	}}
	b := preset.Modal(name).
		Hidden().
		Role("dialog").
		LabelledBy(name+"-title").
		Slot("body", slot)
	return trigger, b
}

func palettePlaceholder(ctx context.Context, cfg CommandPaletteConfig) string {
	if cfg.Placeholder != "" {
		return cfg.Placeholder
	}
	return i18nui.CommandPalettePlaceholder.Text(ctx)
}

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

// PaletteResults renders the option rows a palette search endpoint
// answers for the named palette. A safe Href renders a link.
// With no commands it renders one disabled row with a localized default.
func PaletteResults(ctx context.Context, name string, cmds []PaletteCommand, empty string) render.HTML {
	if ctx == nil {
		ctx = context.Background()
	}
	if name == "" {
		name = "command-palette"
	}
	opts := paletteCommandsToOptions(cmds)
	if len(opts) == 0 {
		if empty == "" {
			empty = i18nui.HuiComboboxNoResults.Text(ctx)
		}
		opts = []headless.ComboboxOption{{Label: empty, Disabled: true}}
	}
	listboxID := name + "-input-listbox"
	for i := range opts {
		opts[i].ID = listboxID + "-res-" + strconv.Itoa(i)
	}
	return headless.ComboboxRows(listboxID, opts, paletteComboboxClasses)
}

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

var paletteComboboxClasses = headless.Classes{
	headless.PartRoot:            "fui-cmd-palette__field",
	headless.PartLabel:           "fui-visually-hidden",
	headless.PartComboboxInput:   "fui-cmd-palette__input",
	headless.PartComboboxForm:    "fui-cmd-palette__combobox",
	headless.PartComboboxListbox: "fui-cmd-palette__listbox",
	headless.PartComboboxOption:  "fui-cmd-palette__option",
	headless.PartText:            "fui-cmd-palette__option-label",
	headless.PartComboboxMeta:    "fui-cmd-palette__option-meta",
	headless.PartComboboxStatus:  "fui-visually-hidden",
}

type commandPaletteSlot struct {
	widgetName    string
	fallbackHref  string
	rpcPath       string
	placeholder   string
	debounceMs    int
	emptyHTML     string
	search        bool
	options       []headless.ComboboxOption
	title         string
	navigateLabel string
	selectLabel   string
	closeLabel    string
	escKey        string
	loadingLabel  string
	emptyLabel    string
	searchPage    string
	allLabel      string
	words         *headless.Strings
	extraAttrs    html.Attrs
}

func (s *commandPaletteSlot) Render() render.HTML {
	titleID := s.widgetName + "-title"
	signalName := s.widgetName + "-results"
	inputID := s.widgetName + "-input"
	title := s.title
	if title == "" {
		title = i18nui.CommandPaletteTitle.Text(context.Background())
	}
	escKey := s.escKey
	if escKey == "" {
		escKey = i18nui.KeyEsc.Text(context.Background())
	}
	closeLabel := s.closeLabel
	if closeLabel == "" {
		closeLabel = i18nui.CommandPaletteClose.Text(context.Background())
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
	var parts headless.Parts
	if s.search {
		parts.Attrs = headless.PartAttrs{headless.PartComboboxListbox: {
			"data-search":         "",
			"data-search-loading": s.loadingLabel,
			"data-search-empty":   s.emptyLabel,
		}}
		if s.searchPage != "" {
			parts.Attrs[headless.PartComboboxListbox]["data-search-all"] = s.searchPage
			parts.Attrs[headless.PartComboboxListbox]["data-search-all-label"] = s.allLabel
		}
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
		Inline:         true,
		Parts:          parts,
		Strings:        s.words,
	}, paletteComboboxClasses)

	closeBtn := render.Tag("button", map[string]string{
		"class":           "fui-cmd-palette__close",
		"type":            "button",
		"data-cui-action": "close",
		"aria-label":      closeLabel,
	}, Icon("close", IconConfig{Class: "fui-cmd-palette__close-icon"}))

	hints := html.Div(html.DivConfig{
		Class:      "fui-cmd-palette__hints",
		ExtraAttrs: html.Attrs{"aria-hidden": "true"},
	},
		hintChip("↑↓", s.navigateLabel),
		hintChip("↵", s.selectLabel),
		hintChip(escKey, closeLabel),
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
     (capped by the viewport); --ui-cmd-palette-height (30rem) is its
     block size, held while the reader types so a filter that leaves
     one row, or none, does not jump the field and footer. The list
     fills what the field and footer leave. The elevation reads the
     theme's dialog-tier shadow. */
  inline-size: min(var(--ui-cmd-palette-width, 36rem), 92vw);
  block-size: var(--ui-cmd-palette-height, 30rem);
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
     listbox (not the whole dialog) absorbs long command lists; growing
     keeps the footer at the bottom of the fixed-height panel when the
     list is short. */
  display: flex;
  flex-direction: column;
  flex: 1 1 auto;
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
  border-bottom: var(--stroke-thin, 1px) solid var(--color-border, #E4E4E7);
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
  /* The only scrolling region: takes whatever space the panel has
     left. overflow-y: auto does double duty — it scrolls AND, per
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
  background: var(--color-surface-soft, #F4F4F5);
}
[data-cui-comp="ui-cmd-palette"] .fui-cmd-palette__option[aria-disabled="true"] {
  color: var(--color-text-muted, #52525B);
  cursor: default;
}
[data-cui-comp="ui-cmd-palette"] .fui-cmd-palette__option > span:not(:first-child) {
  /* The Meta text: a kind or a path, quiet beside the label. */
  color: var(--color-text-muted, #52525B);
  font-size: var(--text-xs, 0.75rem);
  flex: 0 0 auto;
}
/* Search result rows (Search): the page's title and group on the
   first line, the matching text under them, hits marked; a section's
   row hangs from its page's on a rule. */
:where([data-cui-comp="ui-cmd-palette"]) .fui-cmd-palette__option--result {
  flex-wrap: wrap;
  row-gap: var(--spacing-xs, 2px);
  /* A result fades in as it arrives. The client keeps the row of a
     result that stays put across keystrokes, so only new ones play. */
  animation: fui-cmd-palette-row-in var(--duration-fast, 150ms) var(--easing-ease-out, cubic-bezier(0.16, 1, 0.3, 1));
}
@keyframes fui-cmd-palette-row-in {
  from { opacity: 0; transform: translateY(2px); }
}
@media (prefers-reduced-motion: reduce) {
  :where([data-cui-comp="ui-cmd-palette"]) .fui-cmd-palette__option--result { animation: none; }
}
[data-cui-comp="ui-cmd-palette"] .fui-cmd-palette__option > .fui-cmd-palette__option-excerpt:not(:first-child) {
  flex: 1 1 100%;
  min-inline-size: 0;
  line-height: var(--leading-snug, 1.4);
}
[data-cui-comp="ui-cmd-palette"] .fui-cmd-palette__option-excerpt mark {
  background: transparent;
  color: var(--color-text, #09090B);
  font-weight: var(--font-weight-medium);
}
/* The last result row opens the search page for the query: a link
   out of the list, set apart from the results above it. */
:where([data-cui-comp="ui-cmd-palette"]) .fui-cmd-palette__option--all {
  margin-block-start: var(--spacing-xs, 2px);
  border-block-start: var(--stroke-thin, 1px) solid var(--color-border, #E4E4E7);
  border-start-start-radius: 0;
  border-start-end-radius: 0;
  color: var(--color-primary);
  font-weight: var(--font-weight-medium);
}
:where([data-cui-comp="ui-cmd-palette"]) .fui-cmd-palette__option--section {
  margin-inline-start: var(--spacing-lg, 16px);
  border-inline-start: var(--stroke-thin, 1px) solid var(--color-border, #E4E4E7);
  border-start-start-radius: 0;
  border-end-start-radius: 0;
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
  border-top: var(--stroke-thin, 1px) solid var(--color-border, #E4E4E7);
  background: var(--color-surface-soft, #F4F4F5);
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
  color: var(--color-text-muted, #52525B);
  cursor: pointer;
  border-radius: var(--radii-sm, 6px);
}
[data-cui-comp="ui-cmd-palette"] .fui-cmd-palette__close:hover {
  background: var(--color-surface-soft, #f4f4f5);
  color: var(--color-text, #09090B);
}
[data-cui-comp="ui-cmd-palette"] .fui-cmd-palette__close:focus-visible {
  outline: var(--stroke-focus, 2px) solid var(--color-text-subtle);
  outline-offset: var(--stroke-focus-offset, 2px);
}
[data-cui-comp="ui-cmd-palette"] .fui-cmd-palette__hints {
  display: inline-flex;
  gap: var(--spacing-md, 8px);
  font-size: var(--text-xs, 0.75rem);
  color: var(--color-text-muted, #52525B);
}
[data-cui-comp="ui-cmd-palette"] .fui-cmd-palette__hint {
  display: inline-flex;
  align-items: center;
  gap: var(--spacing-sm, 4px);
}
[data-cui-comp="ui-cmd-palette"] .fui-cmd-palette__kbd {
  font-family: var(--font-mono, ui-monospace, monospace);
  padding: 1px calc(var(--spacing-sm, 4px) * 1.5);
  border: var(--stroke-thin, 1px) solid var(--color-border, #E4E4E7);
  border-bottom-width: var(--stroke-thick, 2px);
  border-radius: var(--radii-sm, 6px);
  background: var(--color-surface, #fff);
  font-size: var(--text-xs, 0.75rem);
}
/* A touch screen has no arrow keys or Esc to hint at: the footer keeps
   only its close button. */
@media (hover: none) and (pointer: coarse) {
  [data-cui-comp="ui-cmd-palette"] .fui-cmd-palette__hints { display: none; }
}
@media (max-width: 540px) {
  /* Full-screen sheet, bounded to exactly the dynamic viewport: the
     wrapper's --spacing-lg padding is cancelled by centering overflow
     (the 100vw/100dvh panel spans the padding box edge to edge), and
     the list takes every remaining pixel and scrolls inside. */
  [data-cui-comp="ui-cmd-palette"] { inline-size: 100vw; block-size: 100dvh; min-block-size: 100dvh; max-block-size: 100dvh; border-radius: 0; }
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
  border: var(--stroke-thin, 1px) solid var(--color-border, #E4E4E7);
  border-radius: var(--radii-md, 8px);
  background: var(--color-surface, #fff);
  color: var(--color-text-muted, #52525B);
  font: inherit;
  font-size: var(--text-sm, 0.875rem);
  text-decoration: none;
  cursor: pointer;
}
[data-cui-comp="ui-cmd-palette-trigger"]:hover {
  color: var(--color-text, #09090B);
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
  border: var(--stroke-thin, 1px) solid var(--color-border, #E4E4E7);
  border-radius: var(--radii-sm, 6px);
  background: var(--color-surface-soft, #F4F4F5);
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
