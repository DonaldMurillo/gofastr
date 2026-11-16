package ui

import (
	"strings"

	"github.com/DonaldMurillo/gofastr/core-ui/registry"
	"github.com/DonaldMurillo/gofastr/core-ui/style"
	"github.com/DonaldMurillo/gofastr/core/render"
)

// WithComponentCSS puts ComponentCSS in a <style> before </head>, or before the
// document when it has no head. It adds no scripts.
func WithComponentCSS(document render.HTML, theme style.Theme) render.HTML {
	css := ComponentCSS(document, theme)
	if css == "" {
		return document
	}
	block := "<style>" + css + "</style>"
	if strings.Contains(string(document), "</head>") {
		return render.HTML(strings.Replace(string(document), "</head>", block+"</head>", 1))
	}
	return render.Join(render.Raw(block), document)
}

// ComponentCSS returns the sheets of the components document renders; the host
// still emits the theme's custom properties.
func ComponentCSS(document render.HTML, theme style.Theme) string {
	var css strings.Builder
	hash := style.ThemeHash(theme)
	for _, name := range registry.Scan(string(document)) {
		if entry, ok := registry.Lookup(name); ok {
			css.WriteString(entry.CSSForHash(theme, hash))
			css.WriteByte('\n')
		}
	}
	return css.String()
}
