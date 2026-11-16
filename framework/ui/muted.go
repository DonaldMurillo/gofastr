package ui

import (
	"github.com/DonaldMurillo/gofastr/core-ui/registry"
	"github.com/DonaldMurillo/gofastr/core-ui/style"
	"github.com/DonaldMurillo/gofastr/core/render"
)

// Muted renders children in a subdued inline <span>.
func Muted(children ...render.HTML) render.HTML {
	return mutedStyle.WrapHTML(render.Tag("span",
		map[string]string{"class": "fui-muted"}, children...))
}

// EmptyValue is the muted em dash that stands for an empty field.
func EmptyValue() render.HTML {
	return Muted(render.Text("—"))
}

var mutedStyle = registry.RegisterStyle("ui-muted", mutedCSS)

func mutedCSS(_ style.Theme) string {
	return `[data-cui-comp="ui-muted"] { color: var(--color-text-muted, #52525B); }
`
}
