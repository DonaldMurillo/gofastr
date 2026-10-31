// Package brand supplies the approved GoFastr logo and app icon.
package brand

import _ "embed"

//go:embed gofastr-logo.svg
var logoSVG string

//go:embed gofastr-icon.png
var iconPNG string

// SVG returns the trusted, outlined logo for inline use. Its currentColor fill
// inherits the host's text color; the enclosing link supplies its accessible name.
func SVG() string { return logoSVG }

// IconPNG returns an independent copy of the 512-pixel app icon. The solid
// charcoal tile keeps the reversed symbol visible on light and dark surfaces.
func IconPNG() []byte { return []byte(iconPNG) }
