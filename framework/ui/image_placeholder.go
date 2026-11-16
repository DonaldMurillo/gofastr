package ui

import (
	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core-ui/urlsafe"
	"github.com/DonaldMurillo/gofastr/core/render"
)

// placeholderImage renders the low-fidelity image that sits behind a real
// one while it loads, or "" when durl is not a usable inline image.
func placeholderImage(durl string) render.HTML {
	if !placeholderUsable(durl) {
		return ""
	}
	return html.Image(html.ImageConfig{
		Src:   durl,
		Alt:   "", // decorative: html.Image adds role="presentation"
		Class: "fui-image__lqip",
		ExtraAttrs: html.Attrs{
			"aria-hidden": "true",
			"decoding":    "sync",
		},
	})
}

// placeholderUsable reports whether durl is an inline raster data: URI.
func placeholderUsable(durl string) bool {
	if durl == "" {
		return false
	}
	if !hasDataScheme(durl) {
		return false
	}
	return urlsafe.OK(durl, urlsafe.ImageSource)
}

// hasDataScheme reports whether s begins with the data: scheme, case-insensitively.
func hasDataScheme(s string) bool {
	const p = "data:"
	if len(s) < len(p) {
		return false
	}
	for i := range len(p) {
		c := s[i]
		if c >= 'A' && c <= 'Z' {
			c += 'a' - 'A'
		}
		if c != p[i] {
			return false
		}
	}
	return true
}
