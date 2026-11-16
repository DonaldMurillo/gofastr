package ui

import (
	"sort"
	"strconv"
	"strings"

	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework/headless"
)

// PipelineSource is one entry in a typed responsive source set,
// typically produced by framework/image.VariantSet.
type PipelineSource struct {
	URL   string // image URL: required
	Width int    // intrinsic pixel width: required
	Type  string // MIME type: required (e.g. "image/webp", "image/jpeg")
}

// HeaderInfo is the subset of framework/image.VariantHeader that
// PipelineSourcesFromHeaders consumes.
type HeaderInfo struct {
	Name   string
	Width  int
	Height int
	MIME   string
}

// PipelineSourcesFromHeaders maps variant headers to PipelineImageConfig.Sources,
// urlFor turning a variant's Name into its URL. A header with no width,
// no MIME or no URL is skipped.
func PipelineSourcesFromHeaders(headers []HeaderInfo, urlFor func(name string) string) []PipelineSource {
	out := make([]PipelineSource, 0, len(headers))
	for _, h := range headers {
		if h.Width <= 0 || h.MIME == "" {
			continue
		}
		url := urlFor(h.Name)
		if url == "" {
			continue
		}
		out = append(out, PipelineSource{URL: url, Width: h.Width, Type: h.MIME})
	}
	return out
}

// PipelineImageConfig configures a multi-format <picture> with an
// optional placeholder (LQIP data URL or BlurHash string).
type PipelineImageConfig struct {
	// Fallback is the <img>'s src (required).
	Fallback string

	// Alt is required for non-decorative images.
	Alt string

	// Width and Height are Fallback's intrinsic dimensions; a zero one is omitted.
	Width, Height int

	// Sources render one <source> per Type, in the order each Type first appears.
	Sources []PipelineSource

	// Sizes is the CSS sizes attribute. Default "100vw".
	Sizes string

	// Placeholder is an inline raster data: URI painted behind the image
	// (framework/image.BlurHashDataURL or VariantResult.Placeholder); any
	// other value renders no placeholder.
	Placeholder string

	Eager        bool
	HighPriority bool
	Fit          ImageFit
	Aspect       ImageAspect
	Rounded      bool

	ID, Class string

	// ExtraAttrs lands on the outer <span>; class, id and data-cui-* are dropped.
	ExtraAttrs html.Attrs
}

// PipelineImage renders <picture> with one <source> per MIME type, an
// <img> fallback and an optional placeholder, on OptimizedImage's
// ui-image surface. It panics without Fallback or without Alt (unless
// Class carries fui-image--decorative).
func PipelineImage(cfg PipelineImageConfig) render.HTML {
	if cfg.Fallback == "" {
		panic("ui: PipelineImage requires Fallback")
	}
	if cfg.Alt == "" && !strings.Contains(cfg.Class, "fui-image--decorative") {
		panic("ui: PipelineImage requires Alt (or add fui-image--decorative to Class for intentional decorative images with alt=\"\")")
	}

	cfg.Fallback = imageFallbackURL(cfg.Fallback)
	if len(cfg.Sources) > 0 {
		filtered := make([]PipelineSource, 0, len(cfg.Sources))
		for _, s := range cfg.Sources {
			if safeImageURL(s.URL) != "" {
				filtered = append(filtered, s)
			}
		}
		cfg.Sources = filtered
	}

	lqip := placeholderImage(cfg.Placeholder)
	if lqip != "" {
		lqip = headless.Own(lqip)
	}
	cls := imageRootClass(cfg.Fit, cfg.Aspect, cfg.Rounded, lqip != "", cfg.Class)

	loading := "lazy"
	if cfg.Eager {
		loading = "eager"
	}
	sizes := cfg.Sizes
	if sizes == "" {
		sizes = "100vw"
	}

	imgAttrs := html.Attrs{
		"loading":  loading,
		"decoding": "async",
	}
	if cfg.Width > 0 {
		imgAttrs["width"] = strconv.Itoa(cfg.Width)
	}
	if cfg.Height > 0 {
		imgAttrs["height"] = strconv.Itoa(cfg.Height)
	}
	if cfg.HighPriority {
		imgAttrs["fetchpriority"] = "high"
	}

	img := html.Image(html.ImageConfig{
		Src:        cfg.Fallback,
		Alt:        cfg.Alt,
		Class:      "fui-image__img",
		ExtraAttrs: imgAttrs,
	})

	children := []render.HTML{}
	for _, group := range groupPipelineSources(cfg.Sources) {
		children = append(children, render.Tag("source", map[string]string{
			"type":   group.typ,
			"srcset": group.srcset,
			"sizes":  sizes,
		}))
	}
	children = append(children, img)
	picture := render.Tag("picture", map[string]string{"data-cui-internal": ""}, children...)

	return imageStyle.WrapHTML(html.Span(html.TextConfig{
		Class:      cls,
		ID:         cfg.ID,
		ExtraAttrs: html.SafeExtraAttrs(cfg.ExtraAttrs),
	}, lqip, picture))
}

type pipelineGroup struct {
	typ    string
	srcset string
}

// encodeSrcsetURL percent-escapes the commas and ASCII whitespace the
// srcset parser splits on, except a data: URI's first comma.
func encodeSrcsetURL(url string) string {
	if !strings.ContainsAny(url, ", \t\n\r") {
		return url
	}
	dataDelimiter := -1
	if len(url) >= len("data:") && strings.EqualFold(url[:len("data:")], "data:") {
		dataDelimiter = strings.IndexByte(url, ',')
	}
	var b strings.Builder
	b.Grow(len(url) + 8)
	for i := 0; i < len(url); i++ {
		switch url[i] {
		case ',':
			if i == dataDelimiter {
				b.WriteByte(',')
			} else {
				b.WriteString("%2C")
			}
		case ' ':
			b.WriteString("%20")
		case '\t':
			b.WriteString("%09")
		case '\n':
			b.WriteString("%0A")
		case '\r':
			b.WriteString("%0D")
		default:
			b.WriteByte(url[i])
		}
	}
	return b.String()
}

// groupPipelineSources buckets sources by MIME type in first-appearance
// order, widths ascending, dropping invalid and duplicate entries.
func groupPipelineSources(sources []PipelineSource) []pipelineGroup {
	if len(sources) == 0 {
		return nil
	}
	order := make([]string, 0, len(sources))
	byType := make(map[string][]PipelineSource, len(sources))
	type key struct {
		url   string
		width int
		typ   string
	}
	seen := make(map[key]struct{}, len(sources))
	for _, s := range sources {
		if s.URL == "" || s.Width <= 0 || s.Type == "" {
			continue
		}
		k := key{s.URL, s.Width, s.Type}
		if _, dup := seen[k]; dup {
			continue
		}
		seen[k] = struct{}{}
		if _, ok := byType[s.Type]; !ok {
			order = append(order, s.Type)
		}
		byType[s.Type] = append(byType[s.Type], s)
	}
	out := make([]pipelineGroup, 0, len(order))
	for _, t := range order {
		list := byType[t]
		sort.Slice(list, func(i, j int) bool { return list[i].Width < list[j].Width })
		parts := make([]string, 0, len(list))
		for _, s := range list {
			parts = append(parts, encodeSrcsetURL(s.URL)+" "+strconv.Itoa(s.Width)+"w")
		}
		out = append(out, pipelineGroup{typ: t, srcset: strings.Join(parts, ", ")})
	}
	return out
}
