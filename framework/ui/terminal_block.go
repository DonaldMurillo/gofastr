package ui

// TerminalBlock — a presentational CLI/terminal mock: a labelled header with
// a status dot over a mono, pre-wrapped body. For docs and marketing pages
// that show commands and their output ("$ go install …" → "→ installed").
//
// It is NOT an interactive terminal — there is no input, no execution. Pair
// the body lines with the tone helpers TerminalOut (dim output) and
// TerminalOK (success); plain command text goes in as render.Text.

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
}

// TerminalBlock renders a CLI mock. Body lines are rendered verbatim in a
// pre-wrapped mono body — embed "\n" to break lines.
func TerminalBlock(cfg TerminalBlockConfig, lines ...render.HTML) render.HTML {
	if cfg.Label == "" {
		panic("ui: TerminalBlock requires Label")
	}
	cls := "ui-terminal-block"
	if cfg.Class != "" {
		cls += " " + cfg.Class
	}
	head := html.Div(html.DivConfig{Class: "ui-terminal-block__head"},
		html.Span(html.TextConfig{
			Class:      "ui-terminal-block__dot",
			ExtraAttrs: html.Attrs{"aria-hidden": "true"},
		}),
		render.Text(cfg.Label),
	)
	body := html.Div(html.DivConfig{Class: "ui-terminal-block__body"}, lines...)
	return terminalBlockStyle.WrapHTML(
		html.Div(html.DivConfig{Class: cls, ID: cfg.ID}, head, body))
}

// TerminalOut wraps a line of dim, secondary output (echoed commands, noise).
func TerminalOut(s string) render.HTML {
	return html.Span(html.TextConfig{Class: "ui-terminal-block__out"}, render.Text(s))
}

// TerminalOK wraps a line of success output ("→ installed …").
func TerminalOK(s string) render.HTML {
	return html.Span(html.TextConfig{Class: "ui-terminal-block__ok"}, render.Text(s))
}

var terminalBlockStyle = registry.RegisterStyle("ui-terminal-block", terminalBlockCSS)

func terminalBlockCSS(_ style.Theme) string {
	return `[data-fui-comp="ui-terminal-block"] {
  border: 1px solid var(--color-border, rgba(0,0,0,0.1));
  border-radius: var(--radius-md, 6px);
  background: var(--color-background, #fff);
  overflow: hidden;
  font-family: var(--font-mono, ui-monospace, monospace);
  font-size: var(--text-xs, 12px);
  margin-top: var(--spacing-md, 12px);
}
[data-fui-comp="ui-terminal-block"] .ui-terminal-block__head {
  display: flex;
  align-items: center;
  gap: var(--spacing-md, 8px);
  padding: 6px 12px;
  border-bottom: 1px solid var(--ui-terminal-block-head-border, var(--color-border, rgba(0,0,0,0.1)));
  font-size: var(--text-xs, 11px);
  color: var(--color-text-subtle, #71717A);
}
[data-fui-comp="ui-terminal-block"] .ui-terminal-block__dot {
  width: 6px;
  height: 6px;
  border-radius: 999px;
  background: var(--color-primary, currentColor);
  box-shadow: 0 0 6px var(--color-primary, currentColor);
}
[data-fui-comp="ui-terminal-block"] .ui-terminal-block__body {
  padding: 10px 12px;
  line-height: 1.7;
  color: var(--color-text, #18181B);
  white-space: pre-wrap;
}
[data-fui-comp="ui-terminal-block"] .ui-terminal-block__out {
  color: var(--color-text-subtle, #71717A);
}
[data-fui-comp="ui-terminal-block"] .ui-terminal-block__ok {
  color: var(--ui-terminal-block-ok-color, var(--color-success, #16A34A));
}`
}
