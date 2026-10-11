package ui

import (
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
