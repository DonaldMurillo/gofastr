package ui

import (
	"strings"

	"github.com/DonaldMurillo/gofastr/core-ui/registry"
	"github.com/DonaldMurillo/gofastr/core-ui/style"
	"github.com/DonaldMurillo/gofastr/core/render"
)

// WithComponentCSS includes the styles used by a standalone HTML document.
// It lets a plugin demo render the standard controls without mounting a
// second UIHost or duplicating their CSS. It adds no scripts or behaviors;
// UIHost remains the normal host for interactive framework applications.
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

// ComponentCSS collects only the component styles used in document. Static
// writers can save it in an external stylesheet under a strict style policy.
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
