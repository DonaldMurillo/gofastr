package ui

import (
	"strconv"
	"strings"

	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core/render"
)

// ─── OptimizedImage ─────────────────────────────────────────────────

// ImageFit selects the object-fit treatment.
type ImageFit string

const (
	ImageFitCover   ImageFit = "" // default
	ImageFitContain ImageFit = "contain"
	ImageFitFill    ImageFit = "fill"
)

// ImageAspect selects a CSS aspect-ratio class. Predefined buckets
// avoid inline styles (CSP-clean).
type ImageAspect string

const (
	ImageAspectAuto   ImageAspect = ""
	ImageAspectSquare ImageAspect = "1-1"
	ImageAspect4x3    ImageAspect = "4-3"
	ImageAspect16x9   ImageAspect = "16-9"
	ImageAspect21x9   ImageAspect = "21-9"
	ImageAspect3x4    ImageAspect = "3-4"
)

// ImageSource represents a single entry in an image's responsive
// source set.
type ImageSource struct {
	URL   string // image URL: required
	Width int    // intrinsic pixel width: required (becomes "<url> <width>w")
}

// OptimizedImageConfig configures a responsive, lazy-loaded image.
type OptimizedImageConfig struct {
	Src string // fallback / single-resolution URL (required)
	Alt string // alt text: required for non-decorative images

	// Width and Height are the intrinsic pixel dimensions of the
	// fallback Src. Setting them is mandatory to reserve layout space
	// and avoid Cumulative Layout Shift on first paint.
	Width  int
	Height int

	// Sources, when set, render alongside Src in a <picture> via
	// srcset="<url> <width>w, …". The browser picks the best size for
	// the viewport.
	Sources []ImageSource

	// Sizes is the CSS sizes attribute for the responsive source set.
	// Defaults to "100vw" when Sources is non-empty.
	Sizes string

	// Eager flips loading="lazy" → loading="eager". Use for
	// above-the-fold hero images.
	Eager bool

	// HighPriority sets fetchpriority="high" (above-the-fold critical
	// imagery). Mutually exclusive with Eager=false; setting both
	// keeps both behaviors.
	HighPriority bool

	// Fit selects the object-fit treatment (default cover).
	Fit ImageFit

	// Aspect locks the aspect ratio via a CSS class (CSP-clean,
	// no inline style). Setting Width + Height already establishes
	// the intrinsic ratio; Aspect is for forced ratios distinct from
	// the source.
	Aspect ImageAspect

	// Rounded toggles a token-driven border-radius treatment.
	Rounded bool

	// Placeholder, when set to an inline raster data: URI, renders a
	// low-fidelity image behind this one so something content-shaped is
	// visible before the real pixels arrive.
	//
	// Produce one with framework/image: BlurHashDataURL(hash, …) to render
	// a stored BlurHash, or Image.Placeholder() to build an LQIP straight
	// from the source. A bare BlurHash string is not accepted: it is not
	// an image until it is decoded.
	//
	// Values that are not usable inline images (a raw hash, a remote URL,
	// a non-raster data: URI) are dropped and the image renders without a
	// placeholder. See placeholderImage for why that degrades instead of
	// panicking.
	Placeholder string

	ID    string
	Class string

	// ExtraAttrs forwards additional attributes (data-* test hooks,
	// analytics markers, ARIA overrides) to the image's root wrapper
	// (the outer <span>, in both the single-source and <picture>
	// shapes). Keys the component owns are dropped: class and id
	// (use Class / ID) and data-fui-*.
	ExtraAttrs html.Attrs
}

// OptimizedImage renders a responsive, lazy-loaded image with
// width/height reservations to eliminate Cumulative Layout Shift.
//
// Anti-CLS rule: callers MUST provide Width and Height (intrinsic
// pixel dimensions of Src). Omitting either panics: the framework
// will not silently emit a layout-shifting image.
func OptimizedImage(cfg OptimizedImageConfig) render.HTML {
	if cfg.Src == "" {
		panic("ui: OptimizedImage requires Src")
	}
	// Drop unsafe schemes on the fallback src and on every Sources URL;
	// see framework/ui/safety.go. When the src is unsafe we replace it
	// with the framework's tiny placeholder URL, a 1×1 transparent
	// stub the runtime serves. The browser renders nothing, and the
	// surrounding layout is preserved. Preferable to silently shipping
	// a `javascript:` URL into <img src>.
	//
	// safeImageURL, not safeResourceURL: an inline raster data: URI is a
	// legitimate image source, and rejecting it here produced an image that
	// looked broken rather than one that looked blocked.
	if safe := safeImageURL(cfg.Src); safe != "" {
		cfg.Src = safe
	} else {
		cfg.Src = "/__gofastr/blank.png"
	}
	if len(cfg.Sources) > 0 {
		filtered := cfg.Sources[:0]
		for _, s := range cfg.Sources {
			if safeImageURL(s.URL) != "" {
				filtered = append(filtered, s)
			}
		}
		cfg.Sources = filtered
	}
	if cfg.Alt == "" && !strings.Contains(cfg.Class, "ui-image--decorative") {
		// Decorative images must opt in explicitly to skip alt: Alt=""
		// is otherwise treated as missing, not "intentionally empty".
		panic("ui: OptimizedImage requires Alt (or add ui-image--decorative to Class for intentional decorative images with alt=\"\")")
	}
	if cfg.Width <= 0 || cfg.Height <= 0 {
		panic("ui: OptimizedImage requires Width and Height > 0 to prevent CLS")
	}

	lqip := placeholderImage(cfg.Placeholder)

	cls := "ui-image"
	if cfg.Fit != ImageFitCover {
		cls += " ui-image--fit-" + string(cfg.Fit)
	}
	if cfg.Aspect != ImageAspectAuto {
		cls += " ui-image--aspect-" + string(cfg.Aspect)
	}
	if cfg.Rounded {
		cls += " ui-image--rounded"
	}
	if lqip != "" {
		cls += " ui-image--placeheld"
	}
	if cfg.Class != "" {
		cls += " " + cfg.Class
	}

	loading := "lazy"
	if cfg.Eager {
		loading = "eager"
	}

	imgAttrs := html.Attrs{
		"width":    strconv.Itoa(cfg.Width),
		"height":   strconv.Itoa(cfg.Height),
		"loading":  loading,
		"decoding": "async",
	}
	if cfg.HighPriority {
		imgAttrs["fetchpriority"] = "high"
	}

	imgCfg := html.ImageConfig{
		Src:        cfg.Src,
		Alt:        cfg.Alt,
		Class:      "ui-image__img",
		ExtraAttrs: imgAttrs,
	}

	// Single-source path: just <img>.
	if len(cfg.Sources) == 0 {
		return imageStyle.WrapHTML(html.Span(html.TextConfig{
			Class: cls, ID: cfg.ID,
			ExtraAttrs: html.SafeExtraAttrs(cfg.ExtraAttrs),
		}, lqip, html.Image(imgCfg)))
	}

	// Multi-source <picture> wrapper.
	srcset := buildSrcset(cfg.Sources)
	sizes := cfg.Sizes
	if sizes == "" {
		sizes = "100vw"
	}
	source := render.Tag("source", map[string]string{
		"srcset": srcset,
		"sizes":  sizes,
	})
	picture := render.Tag("picture", nil, source, html.Image(imgCfg))
	return imageStyle.WrapHTML(html.Span(html.TextConfig{
		Class: cls, ID: cfg.ID,
		ExtraAttrs: html.SafeExtraAttrs(cfg.ExtraAttrs),
	}, lqip, picture))
}

func buildSrcset(sources []ImageSource) string {
	parts := make([]string, 0, len(sources))
	for _, s := range sources {
		if s.URL == "" || s.Width <= 0 {
			continue
		}
		// encodeSrcsetURL, not raw concatenation: a comma or space in the
		// URL (presigned links, keys with comma-separated segments) would
		// otherwise split one candidate into several malformed ones.
		parts = append(parts, encodeSrcsetURL(s.URL)+" "+strconv.Itoa(s.Width)+"w")
	}
	return strings.Join(parts, ", ")
}
