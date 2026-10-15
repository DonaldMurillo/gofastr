package ui

// TerminalBlock: a presentational CLI/terminal mock: a labelled header with
// a status dot over a mono, pre-wrapped body. For docs and marketing pages
// that show commands and their output ("$ go install …" → "→ installed").
//
// It is NOT an interactive terminal. There is no input, no execution. Pair
// the body lines with the tone helpers TerminalOut (dim output) and
// TerminalOK (success) or TerminalErr (failure); plain command text
// goes in as render.Text.

import (
	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core-ui/registry"
	"github.com/DonaldMurillo/gofastr/core-ui/style"
	"github.com/DonaldMurillo/gofastr/core/render"
)

// TerminalBlockConfig configures a TerminalBlock.
type TerminalBlockConfig struct {
	Label string // required header text, e.g. "$ install"
	Class string
	ID    string

	// ExtraAttrs forwards additional attributes (data-* test hooks,
	// analytics markers, ARIA overrides) to the block's root
	// wrapper <div>. Keys the component owns are dropped: class
	// and id (use Class / ID), data-cui-*.
	ExtraAttrs html.Attrs
}

// TerminalBlock renders a CLI mock. Body lines are rendered verbatim in a
// pre-wrapped mono body. Embed "\n" to break lines.
func TerminalBlock(cfg TerminalBlockConfig, lines ...render.HTML) render.HTML {
	if cfg.Label == "" {
		panic("ui: TerminalBlock requires Label")
	}
	cls := "fui-terminal-block"
	if cfg.Class != "" {
		cls += " " + cfg.Class
	}
	// Label is a string, so the head is always this component's own.
	head := html.Div(html.DivConfig{Class: "fui-terminal-block__head", ExtraAttrs: html.Attrs{"data-cui-internal": ""}},
		html.Span(html.TextConfig{
			Class:      "fui-terminal-block__dot",
			ExtraAttrs: html.Attrs{"aria-hidden": "true"},
		}),
		render.Text(cfg.Label),
	)
	// The body holds lines, the caller's slot; empty renders a body
	// with none of a caller's markup in it, so that body is this
	// component's own.
	var bodyOwn html.Attrs
	if len(lines) == 0 {
		bodyOwn = html.Attrs{"data-cui-internal": ""}
	}
	body := html.Div(html.DivConfig{Class: "fui-terminal-block__body", ExtraAttrs: bodyOwn}, lines...)
	return terminalBlockStyle.WrapHTML(
		html.Div(html.DivConfig{Class: cls, ID: cfg.ID,
			ExtraAttrs: html.SafeExtraAttrs(cfg.ExtraAttrs)}, head, body))
}

// TerminalOut wraps a line of dim, secondary output (echoed commands, noise).
func TerminalOut(s string) render.HTML {
	return html.Span(html.TextConfig{Class: "fui-terminal-block__out"}, render.Text(s))
}

// TerminalErr wraps a line of failure output ("✗ no route matched").
func TerminalErr(s string) render.HTML {
	return html.Span(html.TextConfig{Class: "fui-terminal-block__err"}, render.Text(s))
}

// TerminalOK wraps a line of success output ("→ installed …").
func TerminalOK(s string) render.HTML {
	return html.Span(html.TextConfig{Class: "fui-terminal-block__ok"}, render.Text(s))
}

var terminalBlockStyle = registry.RegisterStyle("ui-terminal-block", terminalBlockCSS)

func terminalBlockCSS(_ style.Theme) string {
	// Knobs: --ui-terminal-block-dot-size (7px) is the head's status
	// dot (beside the head-border and ok-color knobs below).
	return `[data-cui-comp="ui-terminal-block"] {
  border: var(--stroke-thin, 1px) solid var(--color-border, rgba(0,0,0,0.1));
  border-radius: var(--radii-md, 8px);
  background: var(--color-background, #fff);
  overflow: hidden;
  font-family: var(--font-mono, ui-monospace, monospace);
  font-size: var(--text-xs, 12px);
  margin-top: var(--spacing-md, 8px);
}
[data-cui-comp="ui-terminal-block"] .fui-terminal-block__head {
  display: flex;
  align-items: center;
  gap: var(--spacing-md, 8px);
  padding: calc(var(--spacing-sm, 4px) * 1.5) calc(var(--spacing-sm, 4px) * 3);
  border-bottom: var(--stroke-thin, 1px) solid var(--ui-terminal-block-head-border, var(--color-border, rgba(0,0,0,0.1)));
  font-size: var(--text-xs, 0.75rem);
  color: var(--color-text-subtle, #71717A);
}
[data-cui-comp="ui-terminal-block"] .fui-terminal-block__dot {
  width: var(--ui-terminal-block-dot-size, 7px);
  height: var(--ui-terminal-block-dot-size, 7px);
  border-radius: var(--radii-full, 9999px);
  background: var(--color-primary, currentColor);
}
[data-cui-comp="ui-terminal-block"] .fui-terminal-block__body {
  padding: calc(var(--spacing-sm, 4px) * 2.5) calc(var(--spacing-sm, 4px) * 3);
  line-height: calc(var(--leading-relaxed, 1.6) + 0.1);
  color: var(--color-text, #18181B);
  white-space: pre-wrap;
}
[data-cui-comp="ui-terminal-block"] .fui-terminal-block__out {
  color: var(--color-text-subtle, #71717A);
}
[data-cui-comp="ui-terminal-block"] .fui-terminal-block__ok {
  color: var(--ui-terminal-block-ok-color, var(--color-success, #16A34A));
}
[data-cui-comp="ui-terminal-block"] .fui-terminal-block__err {
  color: var(--ui-terminal-block-err-color, var(--color-danger, #DC2626));
}`
}
