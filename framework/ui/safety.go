package ui

import (
	"regexp"
	"strings"

	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core-ui/urlsafe"
	"github.com/DonaldMurillo/gofastr/core/textsafe"
)

// safeImageURL cleans URLs for <img src> and image srcsets, which
// accept an inline raster data: URI on top of the subresource policy:
// a generated image or a low-fidelity placeholder is a legitimate thing
// to inline, and the media types urlsafe.ImageSource admits cannot
// carry script.
func safeImageURL(u string) string {
	return urlsafe.Clean(u, urlsafe.ImageSource)
}

// safeCSSLength matches one plain CSS length: an optional sign, a
// decimal number and a length unit, or a single var(--token)
// reference. It gates the few config fields whose value must land
// inside an inline style attribute (Carousel.VirtualPlaceholderHeight,
// Workbench.RailWidth): a `;`-separated declaration list or a url() in
// a request-derived config value would otherwise ship verbatim as live
// page CSS — render.Attr HTML-escapes the value, but CSS injection
// needs no HTML metacharacters. Same shape as battery/print's
// safeLength, widened to the viewport units and var() the screen
// components take.
var safeCSSLength = regexp.MustCompile(`^(?:[+-]?(?:[0-9]+(?:\.[0-9]+)?|\.[0-9]+)(?:px|rem|em|%|vw|vh|dvh|svh|lvh|vmin|vmax|ch|ex|pt|pc|mm|cm|in|q)|var\(--[a-zA-Z0-9-]+\))$`)

// cssLengthOr returns v when it is one plain CSS length, else fallback.
// A "" fallback means "drop the style attribute": the component's CSS
// default applies, the same degrade the URL sinks give a rejected href.
func cssLengthOr(v, fallback string) string {
	if safeCSSLength.MatchString(v) {
		return v
	}
	return fallback
}

// scrubAttrs filters an html.Attrs map, removing keys that look like
// inline event handlers (the on* family) or whose values contain
// control bytes. Returns a fresh Attrs so the caller's map is not
// mutated. Nil input yields nil.
//
// ExtraAttrs is a legitimate escape hatch: it lets callers add
// data-* / aria-* / hx-* / dir / lang etc. without a typed knob per
// case. The escape hatch stops at on-event handlers because those
// turn the escape hatch into a stored-XSS primitive when the host
// surface is dynamic (an article body, a search result, a CMS field).
func scrubAttrs(in html.Attrs) html.Attrs {
	if len(in) == 0 {
		return in
	}
	out := make(html.Attrs, len(in))
	for k, v := range in {
		lk := strings.ToLower(k)
		if strings.HasPrefix(lk, "on") {
			continue
		}
		// Attribute names with control bytes are always wrong.
		if textsafe.HasControlBytes(k) || textsafe.HasControlBytes(v) {
			continue
		}
		out[k] = v
	}
	return out
}
