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
	var css strings.Builder
	hash := style.ThemeHash(theme)
	for _, name := range registry.Scan(string(document)) {
		if entry, ok := registry.Lookup(name); ok {
			css.WriteString(entry.CSSForHash(theme, hash))
			css.WriteByte('\n')
		}
	}
	if css.Len() == 0 {
		return document
	}
	block := "<style>" + css.String() + "</style>"
	if strings.Contains(string(document), "</head>") {
		return render.HTML(strings.Replace(string(document), "</head>", block+"</head>", 1))
	}
	return render.Join(render.Raw(block), document)
}
