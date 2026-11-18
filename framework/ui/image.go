package ui

import (
	"strconv"
	"strings"

	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework/headless"
)

// ImageFit selects the object-fit treatment.
type ImageFit string

const (
	ImageFitCover   ImageFit = "" // default
	ImageFitContain ImageFit = "contain"
	ImageFitFill    ImageFit = "fill"
)

// ImageAspect selects a CSS aspect-ratio class.
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

	// Width and Height are Src's intrinsic pixel dimensions, both required.
	Width  int
	Height int

	// Sources, when set, render a <picture> with srcset="<url> <width>w, …".
	Sources []ImageSource

	// Sizes is the sizes attribute for Sources. Default "100vw".
	Sizes string

	// Eager sets loading="eager" for above-the-fold images.
	Eager bool

	// HighPriority sets fetchpriority="high" and loads eagerly: a lazy
	// image is not fetched until layout, so the hint would be wasted.
	HighPriority bool

	// LowPriority sets fetchpriority="low" for images that matter less
	// than their neighbours (carousel slides after the first, below-the-
	// fold decoration). Ignored when HighPriority is set.
	LowPriority bool

	// Fit selects the object-fit treatment (default cover).
	Fit ImageFit

	// Aspect forces an aspect ratio distinct from Width and Height.
	Aspect ImageAspect

	// Rounded toggles a token-driven border-radius treatment.
	Rounded bool

	// Placeholder is an inline raster data: URI painted behind the image
	// while it loads (framework/image.BlurHashDataURL or
	// Image.Placeholder); any other value renders no placeholder.
	Placeholder string

	ID    string
	Class string

	// ExtraAttrs lands on the outer <span>; class, id and data-cui-* are dropped.
	ExtraAttrs html.Attrs
}

// OptimizedImage renders a responsive, lazy-loaded image with
// width/height reservations. It panics without Src, without Alt (unless
// Class carries fui-image--decorative), or without Width and Height.
func OptimizedImage(cfg OptimizedImageConfig) render.HTML {
	if cfg.Src == "" {
		panic("ui: OptimizedImage requires Src")
	}
	cfg.Src = imageFallbackURL(cfg.Src)
	if len(cfg.Sources) > 0 {
		filtered := make([]ImageSource, 0, len(cfg.Sources))
		for _, s := range cfg.Sources {
			if safeImageURL(s.URL) != "" {
				filtered = append(filtered, s)
			}
		}
		cfg.Sources = filtered
	}
	if cfg.Alt == "" && !strings.Contains(cfg.Class, "fui-image--decorative") {
		panic("ui: OptimizedImage requires Alt (or add fui-image--decorative to Class for intentional decorative images with alt=\"\")")
	}
	if cfg.Width <= 0 || cfg.Height <= 0 {
		panic("ui: OptimizedImage requires Width and Height > 0 to prevent CLS")
	}

	lqip := placeholderImage(cfg.Placeholder)
	if lqip != "" {
		lqip = headless.Own(lqip)
	}
	cls := imageRootClass(cfg.Fit, cfg.Aspect, cfg.Rounded, lqip != "", cfg.Class)

	imgAttrs := html.Attrs{
		"width":    strconv.Itoa(cfg.Width),
		"height":   strconv.Itoa(cfg.Height),
		"decoding": "async",
	}
	setImagePriority(imgAttrs, cfg.Eager, cfg.HighPriority, cfg.LowPriority)

	imgCfg := html.ImageConfig{
		Src:        cfg.Src,
		Alt:        cfg.Alt,
		Class:      "fui-image__img",
		ExtraAttrs: imgAttrs,
	}

	if len(cfg.Sources) == 0 {
		single := imgCfg
		single.ExtraAttrs = html.MergeAttrs(imgAttrs, html.Attrs{"data-cui-internal": ""})
		return imageStyle.WrapHTML(html.Span(html.TextConfig{
			Class: cls, ID: cfg.ID,
			ExtraAttrs: html.SafeExtraAttrs(cfg.ExtraAttrs),
		}, lqip, html.Image(single)))
	}

	srcset := buildSrcset(cfg.Sources)
	sizes := cfg.Sizes
	if sizes == "" {
		sizes = "100vw"
	}
	source := render.Tag("source", map[string]string{
		"srcset": srcset,
		"sizes":  sizes,
	})
	picture := render.Tag("picture", map[string]string{"data-cui-internal": ""}, source, html.Image(imgCfg))
	return imageStyle.WrapHTML(html.Span(html.TextConfig{
		Class: cls, ID: cfg.ID,
		ExtraAttrs: html.SafeExtraAttrs(cfg.ExtraAttrs),
	}, lqip, picture))
}

// imageFallbackURL is u through the image policy, or the blank stub
// when the policy refuses it.
func imageFallbackURL(u string) string {
	if safe := safeImageURL(u); safe != "" {
		return safe
	}
	return "/__gofastr/blank.png"
}

// imageRootClass is the fui-image root's class list, shared by
// OptimizedImage and PipelineImage.
func imageRootClass(fit ImageFit, aspect ImageAspect, rounded, placeheld bool, extra string) string {
	cls := "fui-image"
	if fit != ImageFitCover {
		cls += " fui-image--fit-" + string(fit)
	}
	if aspect != ImageAspectAuto {
		cls += " fui-image--aspect-" + string(aspect)
	}
	if rounded {
		cls += " fui-image--rounded"
	}
	if placeheld {
		cls += " fui-image--placeheld"
	}
	if extra != "" {
		cls += " " + extra
	}
	return cls
}

func buildSrcset(sources []ImageSource) string {
	parts := make([]string, 0, len(sources))
	for _, s := range sources {
		if s.URL == "" || s.Width <= 0 {
			continue
		}
		parts = append(parts, encodeSrcsetURL(s.URL)+" "+strconv.Itoa(s.Width)+"w")
	}
	return strings.Join(parts, ", ")
}

// setImagePriority writes loading and fetchpriority. HighPriority implies
// eager, since fetchpriority="high" on a lazy image is a contradiction.
func setImagePriority(attrs html.Attrs, eager, high, low bool) {
	switch {
	case high:
		attrs["loading"] = "eager"
		attrs["fetchpriority"] = "high"
	case eager:
		attrs["loading"] = "eager"
	default:
		attrs["loading"] = "lazy"
	}
	if low && !high {
		attrs["fetchpriority"] = "low"
	}
}
