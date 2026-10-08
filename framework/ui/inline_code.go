package ui

// ─── InlineCode ─────────────────────────────────────────────────────
//
// A short piece of code inside running text or a reference row: an
// operator, a field name, an example expression. It is a <code> on the
// soft surface in the mono font, sized to the text around it, and it
// wraps rather than widening its container. Blocks of code use
// CodeBlock.

import (
	"github.com/DonaldMurillo/gofastr/core-ui/registry"
	"github.com/DonaldMurillo/gofastr/core-ui/style"
	"github.com/DonaldMurillo/gofastr/core/render"
)

// InlineCode renders text as an inline <code>. The text is escaped.
func InlineCode(text string) render.HTML {
	return inlineCodeStyle.WrapHTML(render.Tag("code",
		map[string]string{"class": "fui-code"}, render.Text(text)))
}

var inlineCodeStyle = registry.RegisterStyle("ui-code", inlineCodeCSS)

func inlineCodeCSS(_ style.Theme) string {
	return `[data-cui-comp="ui-code"] {
  font-family: var(--font-mono, ui-monospace, "SF Mono", "Cascadia Mono", "Roboto Mono", monospace);
  font-size: 0.9em;
  padding: 0.05em 0.35em;
  border-radius: var(--radii-sm, 6px);
  background: var(--color-surface-soft, #f5f5f7);
  color: var(--color-text, #111);
  overflow-wrap: anywhere;
}
`
}
