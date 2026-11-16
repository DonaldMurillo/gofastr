package ui

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

	// ExtraAttrs lands on the root <div>; class, id and data-cui-* are dropped.
	ExtraAttrs html.Attrs
}

// TerminalBlock renders a presentational CLI mock: a labelled head over a
// pre-wrapped mono body of lines, verbatim, toned with TerminalOut,
// TerminalOK and TerminalErr. It panics without Label.
func TerminalBlock(cfg TerminalBlockConfig, lines ...render.HTML) render.HTML {
	if cfg.Label == "" {
		panic("ui: TerminalBlock requires Label")
	}
	cls := "fui-terminal-block"
	if cfg.Class != "" {
		cls += " " + cfg.Class
	}
	head := html.Div(html.DivConfig{Class: "fui-terminal-block__head", ExtraAttrs: html.Attrs{"data-cui-internal": ""}},
		html.Span(html.TextConfig{
			Class:      "fui-terminal-block__dot",
			ExtraAttrs: html.Attrs{"aria-hidden": "true"},
		}),
		render.Text(cfg.Label),
	)
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
	return `[data-cui-comp="ui-terminal-block"] {
  border: var(--stroke-thin, 1px) solid var(--color-border, #E4E4E7);
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
  border-bottom: var(--stroke-thin, 1px) solid var(--ui-terminal-block-head-border, var(--color-border, #E4E4E7));
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
  color: var(--color-text, #09090B);
  white-space: pre-wrap;
}
[data-cui-comp="ui-terminal-block"] .fui-terminal-block__out {
  color: var(--color-text-subtle, #71717A);
}
[data-cui-comp="ui-terminal-block"] .fui-terminal-block__ok {
  color: var(--ui-terminal-block-ok-color, var(--color-success, #166534));
}
[data-cui-comp="ui-terminal-block"] .fui-terminal-block__err {
  color: var(--ui-terminal-block-err-color, var(--color-danger, #B91C1C));
}`
}
