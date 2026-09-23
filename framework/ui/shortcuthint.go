package ui

import (
	"strings"

	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core-ui/registry"
	"github.com/DonaldMurillo/gofastr/core-ui/style"
	"github.com/DonaldMurillo/gofastr/core/render"
)

// ─── ShortcutHint ───────────────────────────────────────────────────
//
// Renders a keyboard chord (e.g. "Mod+K", "/", "Esc") as a sequence
// of <kbd> chips. Used to surface available keyboard shortcuts next
// to triggers: Command Palette buttons, search inputs, menu items.
//
// Mod-key glyphs auto-resolve to ⌘ on Mac / Ctrl elsewhere via the
// runtime-set <html data-fui-os> attribute, with both labels rendered
// in the SSR output and the wrong one hidden via CSS. Screen readers
// see the SR-only label exactly once via aria-label on the wrapper.
//
// When BindTarget is set, the runtime's data-fui-shortcut-click hook
// is applied to the target so pressing the chord clicks the target.
// The hint itself is purely visual.

// ShortcutHintConfig configures the chord display.
type ShortcutHintConfig struct {
	// Chord is the human-readable chord string accepted by the
	// runtime's parseCombo: "Mod+K", "Ctrl+/", "Shift+Tab", "/",
	// "Esc", "Enter". Required.
	Chord string
	// BindTarget is an optional CSS selector. When set, the hint
	// renders data-hui-shortcut-hint carrying the selector and the
	// headless-navigation module resolves the first connected match and
	// CLICKS it when the chord is pressed. The selector is validated at
	// render: control bytes and markup openers are refused, and the
	// value must be a single selector.
	BindTarget string

	// SROnlyLabel overrides the screen-reader announcement.
	// Default: humanized chord (e.g. "Command-K", "Slash", "Escape").
	SROnlyLabel string

	ID    string
	Class string
	// ExtraAttrs forwards additional attributes (data-* test hooks,
	// analytics markers, ARIA overrides) to the hint's root element.
	// Keys the component owns are dropped: class and id (use Class /
	// ID), data-fui-*, and aria-hidden — the wrapper must stay in the
	// accessibility tree for its SR-only label.
	ExtraAttrs html.Attrs
}

// ShortcutHint renders the visual chord chips.
func ShortcutHint(cfg ShortcutHintConfig) render.HTML {
	if cfg.Chord == "" {
		panic("ui: ShortcutHint requires Chord")
	}
	parts := parseChordParts(cfg.Chord)
	if len(parts) == 0 {
		panic("ui: ShortcutHint Chord parsed to zero parts: " + cfg.Chord)
	}
	if cfg.BindTarget != "" {
		checkShortcutSelector(cfg.BindTarget)
	}

	srLabel := cfg.SROnlyLabel
	if srLabel == "" {
		srLabel = humanizeChord(parts)
	}

	cls := "fui-shortcut-hint"
	if cfg.Class != "" {
		cls += " " + cfg.Class
	}

	chips := make([]render.HTML, 0, len(parts))
	for _, p := range parts {
		chips = append(chips, renderChordPart(p))
	}
	chips = append(chips, html.Span(html.TextConfig{Class: "fui-visually-hidden"}, render.Text("Shortcut: "+srLabel)))

	extras := html.SafeExtraAttrs(cfg.ExtraAttrs, "aria-hidden", "data-hui-shortcut-hint")
	if extras == nil {
		extras = html.Attrs{}
	}
	// The real binding: the chord names the selector the
	// headless-navigation module resolves and clicks. The module also
	// carries the chord itself, so one declaration serves both the
	// visual hint and the keyboard behavior.
	if cfg.BindTarget != "" {
		extras["data-hui-shortcut-hint"] = cfg.BindTarget
		extras["data-hui-shortcut-click"] = cfg.Chord
		extras["data-hui-shortcut-target"] = cfg.BindTarget
	}

	return shortcutHintStyle.WrapHTML(html.Span(html.TextConfig{
		Class: cls,
		ID:    cfg.ID,
		// Owned aria-hidden wins over any caller override (protected
		// above), so the SR-only label stays reachable.
		ExtraAttrs: html.MergeAttrs(extras, html.Attrs{"aria-hidden": "false"}),
	}, chips...))
}

// checkShortcutSelector refuses a BindTarget this component cannot hand
// to the module: control bytes and markup openers never reach the
// browser intact, and the module resolves ONE selector.
func checkShortcutSelector(sel string) {
	if sel == "" {
		panic("ui: ShortcutHint BindTarget is empty — a chord that clicks nothing is not a binding")
	}
	for _, r := range sel {
		if r < 0x20 || r == 0x7f {
			panic("ui: ShortcutHint BindTarget carries a control byte: " + sel)
		}
	}
	if strings.Contains(sel, "<") {
		panic("ui: ShortcutHint BindTarget must be a selector, not markup: " + sel)
	}
}

// chordPart is one normalized component of a chord.
type chordPart struct {
	kind string // "mod" | "shift" | "alt" | "key"
	key  string // for kind=key: literal text (e.g. "K", "/", "Esc")
}

func parseChordParts(chord string) []chordPart {
	out := []chordPart{}
	for raw := range strings.SplitSeq(chord, "+") {
		t := strings.TrimSpace(raw)
		if t == "" {
			continue
		}
		lower := strings.ToLower(t)
		switch lower {
		case "mod", "cmd", "ctrl", "control":
			out = append(out, chordPart{kind: "mod"})
		case "shift":
			out = append(out, chordPart{kind: "shift"})
		case "alt", "option", "opt":
			out = append(out, chordPart{kind: "alt"})
		default:
			out = append(out, chordPart{kind: "key", key: displayKey(t)})
		}
	}
	return out
}

func displayKey(k string) string {
	switch strings.ToLower(k) {
	case "enter", "return":
		return "↵"
	case "esc", "escape":
		return "Esc"
	case "tab":
		return "Tab"
	case "space":
		return "Space"
	case "arrowup", "up":
		return "↑"
	case "arrowdown", "down":
		return "↓"
	case "arrowleft", "left":
		return "←"
	case "arrowright", "right":
		return "→"
	}
	if len(k) == 1 {
		return strings.ToUpper(k)
	}
	return k
}

func renderChordPart(p chordPart) render.HTML {
	switch p.kind {
	case "mod":
		// Two spans: ⌘ for Mac, Ctrl for others. CSS hides the wrong one.
		return html.Kbd(html.TextConfig{
			Class:      "fui-shortcut-hint__key fui-shortcut-hint__key--mod",
			ExtraAttrs: html.Attrs{"aria-hidden": "true"},
		},
			html.Span(html.TextConfig{Class: "fui-shortcut-hint__mod-mac"}, render.Text("⌘")),
			html.Span(html.TextConfig{Class: "fui-shortcut-hint__mod-other"}, render.Text("Ctrl")),
		)
	case "shift":
		return html.Kbd(html.TextConfig{
			Class:      "fui-shortcut-hint__key",
			ExtraAttrs: html.Attrs{"aria-hidden": "true"},
		}, render.Text("⇧"))
	case "alt":
		return html.Kbd(html.TextConfig{
			Class:      "fui-shortcut-hint__key",
			ExtraAttrs: html.Attrs{"aria-hidden": "true"},
		}, render.Text("⌥"))
	default:
		return html.Kbd(html.TextConfig{
			Class:      "fui-shortcut-hint__key",
			ExtraAttrs: html.Attrs{"aria-hidden": "true"},
		}, render.Text(p.key))
	}
}

func humanizeChord(parts []chordPart) string {
	out := []string{}
	for _, p := range parts {
		switch p.kind {
		case "mod":
			out = append(out, "Mod")
		case "shift":
			out = append(out, "Shift")
		case "alt":
			out = append(out, "Alt")
		default:
			switch p.key {
			case "↵":
				out = append(out, "Enter")
			case "↑":
				out = append(out, "Up")
			case "↓":
				out = append(out, "Down")
			case "←":
				out = append(out, "Left")
			case "→":
				out = append(out, "Right")
			default:
				out = append(out, p.key)
			}
		}
	}
	return strings.Join(out, "-")
}

var shortcutHintStyle = registry.RegisterStyle("ui-shortcut-hint", shortcutHintCSS)

func shortcutHintCSS(_ style.Theme) string {
	return `[data-fui-comp="ui-shortcut-hint"] {
  display: inline-flex;
  align-items: center;
  gap: var(--spacing-xs, 2px);
  font-family: var(--fonts-mono, ui-monospace, "SF Mono", "Cascadia Mono", "Roboto Mono", monospace);
  vertical-align: middle;
}
[data-fui-comp="ui-shortcut-hint"] .fui-shortcut-hint__key {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  min-inline-size: 1.5em;
  block-size: 1.5em;
  padding: 0 6px;
  border: 1px solid var(--color-border, #d0d0d8);
  border-bottom-width: 2px;
  border-radius: var(--radii-sm, 4px);
  background: var(--color-muted, #f5f5f7);
  color: var(--color-text, #111);
  font-size: var(--text-xs, 0.75rem);
  font-weight: 600;
  line-height: 1;
}
[data-fui-comp="ui-shortcut-hint"] .fui-shortcut-hint__mod-mac,
[data-fui-comp="ui-shortcut-hint"] .fui-shortcut-hint__mod-other {
  display: inline;
}
html[data-fui-os="mac"] [data-fui-comp="ui-shortcut-hint"] .fui-shortcut-hint__mod-other { display: none; }
html[data-fui-os="other"] [data-fui-comp="ui-shortcut-hint"] .fui-shortcut-hint__mod-mac { display: none; }
/* Default (SSR before runtime boots, or non-JS): show Mac symbol. */
html:not([data-fui-os]) [data-fui-comp="ui-shortcut-hint"] .fui-shortcut-hint__mod-other { display: none; }

/* Touch devices have no physical keyboard — hide hints to avoid confusion. */
@media (pointer: coarse) and (hover: none) {
  [data-fui-comp="ui-shortcut-hint"] { display: none; }
}
`
}
