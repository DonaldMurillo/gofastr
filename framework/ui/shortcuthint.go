package ui

import (
	"context"
	"strings"

	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core-ui/registry"
	"github.com/DonaldMurillo/gofastr/core-ui/style"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework/i18nui"
)

// ShortcutHintConfig configures the chord display.
type ShortcutHintConfig struct {
	// Chord is the chord to draw, such as "Mod+K", "/" or "Esc". Required.
	Chord string
	// BindTarget is a CSS selector the chord clicks; control bytes and markup are refused.
	BindTarget string
	// SROnlyLabel overrides the screen-reader words. Default: the chord spelled out.
	SROnlyLabel string
	ID          string
	Class       string
	// ExtraAttrs land on the root; class, id, data-cui-* and aria-hidden are dropped.
	ExtraAttrs html.Attrs
	// Ctx resolves the keycap words and the sentence. Nil uses English.
	Ctx context.Context
}

// ShortcutHint renders a chord as <kbd> chips with a screen-reader sentence.
func ShortcutHint(cfg ShortcutHintConfig) render.HTML {
	if cfg.Chord == "" {
		panic("ui: ShortcutHint requires Chord")
	}
	ctx := cfg.Ctx
	if ctx == nil {
		ctx = context.Background()
	}
	parts := parseChordParts(ctx, cfg.Chord)
	if len(parts) == 0 {
		panic("ui: ShortcutHint Chord parsed to zero parts: " + cfg.Chord)
	}
	if cfg.BindTarget != "" {
		checkShortcutSelector(cfg.BindTarget)
	}

	srLabel := cfg.SROnlyLabel
	if srLabel == "" {
		srLabel = humanizeChord(ctx, parts)
	}

	cls := "fui-shortcut-hint"
	if cfg.Class != "" {
		cls += " " + cfg.Class
	}

	chips := make([]render.HTML, 0, len(parts))
	for _, p := range parts {
		chips = append(chips, renderChordPart(ctx, p))
	}
	chips = append(chips, html.Span(html.TextConfig{Class: "fui-visually-hidden", ExtraAttrs: html.Attrs{"data-cui-internal": ""}}, render.Text(i18nui.ShortcutLabel.Text(ctx, map[string]any{"keys": srLabel}))))

	extras := html.SafeExtraAttrs(cfg.ExtraAttrs, "aria-hidden", "data-hui-shortcut-hint")
	if extras == nil {
		extras = html.Attrs{}
	}
	if cfg.BindTarget != "" {
		extras["data-hui-shortcut-hint"] = cfg.BindTarget
		extras["data-hui-shortcut-click"] = cfg.Chord
		extras["data-hui-shortcut-target"] = cfg.BindTarget
	}

	return shortcutHintStyle.WrapHTML(html.Span(html.TextConfig{
		Class:      cls,
		ID:         cfg.ID,
		ExtraAttrs: html.MergeAttrs(extras, html.Attrs{"aria-hidden": "false"}),
	}, chips...))
}

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

type chordPart struct {
	kind string
	key  string
}

func parseChordParts(ctx context.Context, chord string) []chordPart {
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
			out = append(out, chordPart{kind: "key", key: displayKey(ctx, t)})
		}
	}
	return out
}

func displayKey(ctx context.Context, k string) string {
	switch strings.ToLower(k) {
	case "enter", "return":
		return "↵"
	case "esc", "escape":
		return i18nui.KeyEsc.Text(ctx)
	case "tab":
		return i18nui.KeyTab.Text(ctx)
	case "space":
		return i18nui.KeySpace.Text(ctx)
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

func renderChordPart(ctx context.Context, p chordPart) render.HTML {
	switch p.kind {
	case "mod":
		return html.Kbd(html.TextConfig{
			Class:      "fui-shortcut-hint__key fui-shortcut-hint__key--mod",
			ExtraAttrs: html.Attrs{"aria-hidden": "true", "data-cui-internal": ""},
		},
			html.Span(html.TextConfig{Class: "fui-shortcut-hint__mod-mac"}, render.Text("⌘")),
			html.Span(html.TextConfig{Class: "fui-shortcut-hint__mod-other"}, render.Text(i18nui.KeyCtrl.Text(ctx))),
		)
	case "shift":
		return html.Kbd(html.TextConfig{
			Class:      "fui-shortcut-hint__key",
			ExtraAttrs: html.Attrs{"aria-hidden": "true", "data-cui-internal": ""},
		}, render.Text("⇧"))
	case "alt":
		return html.Kbd(html.TextConfig{
			Class:      "fui-shortcut-hint__key",
			ExtraAttrs: html.Attrs{"aria-hidden": "true", "data-cui-internal": ""},
		}, render.Text("⌥"))
	default:
		return html.Kbd(html.TextConfig{
			Class:      "fui-shortcut-hint__key",
			ExtraAttrs: html.Attrs{"aria-hidden": "true", "data-cui-internal": ""},
		}, render.Text(p.key))
	}
}

func ariaKeyShortcuts(chord string) string {
	var mods []string
	key := ""
	mod := false
	for raw := range strings.SplitSeq(chord, "+") {
		switch t := strings.TrimSpace(raw); strings.ToLower(t) {
		case "":
		case "mod", "cmd", "ctrl", "control":
			mod = true
		case "shift":
			mods = append(mods, "Shift")
		case "alt", "option", "opt":
			mods = append(mods, "Alt")
		default:
			key = t
			if len(t) == 1 {
				key = strings.ToUpper(t)
			}
		}
	}
	spell := func(m string) string {
		return strings.Join(append(append([]string{m}, mods...), key), "+")
	}
	if !mod {
		return strings.Join(append(mods, key), "+")
	}
	return spell("Meta") + " " + spell("Control")
}

func humanizeChord(ctx context.Context, parts []chordPart) string {
	out := []string{}
	for _, p := range parts {
		switch p.kind {
		case "mod":
			out = append(out, i18nui.KeyMod.Text(ctx))
		case "shift":
			out = append(out, i18nui.KeyShift.Text(ctx))
		case "alt":
			out = append(out, i18nui.KeyAlt.Text(ctx))
		default:
			switch p.key {
			case "↵":
				out = append(out, i18nui.KeyEnter.Text(ctx))
			case "↑":
				out = append(out, i18nui.KeyUp.Text(ctx))
			case "↓":
				out = append(out, i18nui.KeyDown.Text(ctx))
			case "←":
				out = append(out, i18nui.KeyLeft.Text(ctx))
			case "→":
				out = append(out, i18nui.KeyRight.Text(ctx))
			default:
				out = append(out, p.key)
			}
		}
	}
	return strings.Join(out, "-")
}

var shortcutHintStyle = registry.RegisterStyle("ui-shortcut-hint", shortcutHintCSS)

func shortcutHintCSS(_ style.Theme) string {
	return `[data-cui-comp="ui-shortcut-hint"] {
  display: inline-flex;
  align-items: center;
  gap: var(--spacing-xs, 2px);
  font-family: var(--font-mono, ui-monospace, "SF Mono", "Cascadia Mono", "Roboto Mono", monospace);
  vertical-align: middle;
}
[data-cui-comp="ui-shortcut-hint"] .fui-shortcut-hint__key {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  min-inline-size: 1.5em;
  block-size: 1.5em;
  padding: 0 calc(var(--spacing-sm, 4px) * 1.5);
  border: var(--stroke-thin, 1px) solid var(--color-border, #E4E4E7);
  border-bottom-width: var(--stroke-thick, 2px);
  border-radius: var(--radii-sm, 6px);
  background: var(--color-surface-soft, #F4F4F5);
  color: var(--color-text, #09090B);
  font-size: var(--text-xs, 0.75rem);
  font-weight: var(--font-weight-semibold);
  line-height: 1;
}
[data-cui-comp="ui-shortcut-hint"] .fui-shortcut-hint__mod-mac,
[data-cui-comp="ui-shortcut-hint"] .fui-shortcut-hint__mod-other {
  display: inline;
}
html[data-cui-os="mac"] [data-cui-comp="ui-shortcut-hint"] .fui-shortcut-hint__mod-other { display: none; }
html[data-cui-os="other"] [data-cui-comp="ui-shortcut-hint"] .fui-shortcut-hint__mod-mac { display: none; }
/* Default (SSR before runtime boots, or non-JS): show Mac symbol. */
html:not([data-cui-os]) [data-cui-comp="ui-shortcut-hint"] .fui-shortcut-hint__mod-other { display: none; }

/* Inside a button the chips take the button's own ink, so a hint on
   a primary button reads as part of it rather than a pasted-on key. */
[data-cui-comp="ui-button"] [data-cui-comp="ui-shortcut-hint"] .fui-shortcut-hint__key {
  border-color: color-mix(in srgb, currentColor 30%, transparent);
  border-bottom-width: var(--stroke-thin, 1px);
  background: color-mix(in srgb, currentColor 12%, transparent);
  color: inherit;
}

/* Touch devices have no physical keyboard — hide hints to avoid confusion. */
@media (pointer: coarse) and (hover: none) {
  [data-cui-comp="ui-shortcut-hint"] { display: none; }
}
`
}
