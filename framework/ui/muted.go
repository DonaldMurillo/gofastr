package ui

// ─── Muted ──────────────────────────────────────────────────────────
//
// Subdued inline text for secondary values: empty-value placeholders in
// tables and detail views, de-emphasized counts, "not set" markers.
// Colors via the --color-text-muted token so every theme recolors it.

import (
	"github.com/DonaldMurillo/gofastr/core-ui/registry"
	"github.com/DonaldMurillo/gofastr/core-ui/style"
	"github.com/DonaldMurillo/gofastr/core/render"
)

// Muted renders children in a subdued inline <span>.
func Muted(children ...render.HTML) render.HTML {
	return mutedStyle.WrapHTML(render.Tag("span",
		map[string]string{"class": "ui-muted"}, children...))
}

// EmptyValue is the canonical "no value here" placeholder — a muted em
// dash. Tables and detail views render it for null/empty fields so
// emptiness reads as deliberate rather than broken.
func EmptyValue() render.HTML {
	return Muted(render.Text("—"))
}

var mutedStyle = registry.RegisterStyle("ui-muted", mutedCSS)

func mutedCSS(_ style.Theme) string {
	return `[data-fui-comp="ui-muted"] { color: var(--color-text-muted, #64748b); }
`
}
