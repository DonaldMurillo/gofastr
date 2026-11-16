package ui

import (
	"github.com/DonaldMurillo/gofastr/core-ui/registry"
	"github.com/DonaldMurillo/gofastr/core-ui/style"
	"github.com/DonaldMurillo/gofastr/core/render"
)

// InlineCode renders escaped text as an inline <code>; blocks use CodeBlock.
func InlineCode(text string) render.HTML {
	return inlineCodeStyle.WrapHTML(render.Tag("code",
		map[string]string{"class": "fui-code"}, render.Text(text)))
}

// InlineCodeDanger is InlineCode in the danger tone, for an error message.
func InlineCodeDanger(text string) render.HTML {
	return inlineCodeStyle.WrapHTML(render.Tag("code",
		map[string]string{"class": "fui-code fui-code--danger"}, render.Text(text)))
}

var inlineCodeStyle = registry.RegisterStyle("ui-code", inlineCodeCSS)

func inlineCodeCSS(_ style.Theme) string {
	return `[data-cui-comp="ui-code"] {
  font-family: var(--font-mono, ui-monospace, "SF Mono", "Cascadia Mono", "Roboto Mono", monospace);
  font-size: 0.9em;
  padding: 0.05em 0.35em;
  border-radius: var(--radii-sm, 6px);
  background: var(--color-surface-soft, #F4F4F5);
  color: var(--color-text, #09090B);
  overflow-wrap: anywhere;
}
/* The danger tone mixes the hue toward the text colour, as the danger
   badge does, so it keeps AA contrast on either scheme. */
[data-cui-comp="ui-code"].fui-code--danger {
  background: color-mix(in oklab, var(--color-danger, #B91C1C) 10%, var(--color-surface, #fff) 90%);
  color: color-mix(in oklab, var(--color-danger, #B91C1C) 55%, var(--color-text, #09090B) 45%);
}
`
}
