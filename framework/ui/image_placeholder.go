package ui

import (
	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core-ui/urlsafe"
	"github.com/DonaldMurillo/gofastr/core/render"
)

// placeholderImage renders the low-fidelity image that sits behind a real
// one while it loads, or the empty string when durl is not a usable inline
// image.
//
// Why an element and not a CSS background: a placeholder is per-instance
// data, and this project cannot put per-instance values into CSS — inline
// `style="…"` is blocked by the default Content-Security-Policy and by the
// core-ui/check/noinlinestyles linter, and a data URL cannot be enumerated
// into a class the way carousel column counts are. An `<img>` carrying the
// data URI needs no dynamic CSS, no `attr()` (which browsers only support
// for `content`), and no JavaScript. The stylesheet stacks it behind the
// real image with static rules; see imageCSS.
//
// A bad value degrades to no placeholder rather than panicking: unlike Src
// or Alt, which are caller-code bugs, a placeholder is data — read from a
// column some older upload wrote, or produced by another client — and
// user-generated data must not be able to take a page down. This mirrors
// PipelineImage's handling of missing intrinsic dimensions.
func placeholderImage(durl string) render.HTML {
	if !placeholderUsable(durl) {
		return ""
	}
	return html.Image(html.ImageConfig{
		Src:   durl,
		Alt:   "", // decorative — html.Image adds role="presentation"
		Class: "ui-image__lqip",
		ExtraAttrs: html.Attrs{
			"aria-hidden": "true",
			// A data URI costs no request, so deferring or async-decoding it
			// could only make the placeholder appear after the image it
			// stands in for — which is the one thing it must never do.
			"decoding": "sync",
		},
	})
}

// placeholderUsable reports whether durl may be rendered as a placeholder:
// an inline raster data: URI and nothing else. In particular a bare
// BlurHash string is not usable — decode it first with
// framework/image.BlurHashDataURL.
//
// Remote URLs are refused on purpose even though they are safe to render:
// a placeholder exists to paint before the network settles, so one that
// needs its own request defeats the point.
func placeholderUsable(durl string) bool {
	if durl == "" {
		return false
	}
	if !hasDataScheme(durl) {
		return false
	}
	// urlsafe.ImageSource carries the media-type allow-list, so
	// data:text/html and data:image/svg+xml are rejected here.
	return urlsafe.OK(durl, urlsafe.ImageSource)
}

// hasDataScheme reports whether s begins with the data: scheme,
// case-insensitively. Used to tell "this is meant to be an inline image"
// from "this is a remote URL or a raw hash", so the two get different
// treatment even though both are simply dropped today.
func hasDataScheme(s string) bool {
	const p = "data:"
	if len(s) < len(p) {
		return false
	}
	for i := 0; i < len(p); i++ {
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
