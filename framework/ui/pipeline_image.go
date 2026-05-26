package ui

import (
	"sort"
	"strconv"
	"strings"

	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core/render"
)

// PipelineSource is one entry in a typed responsive source set,
// typically produced by framework/image.VariantSet.
type PipelineSource struct {
	URL   string // image URL — required
	Width int    // intrinsic pixel width — required
	Type  string // MIME type — required (e.g. "image/webp", "image/jpeg")
}

// HeaderInfo is the subset of framework/image.VariantHeader that
// PipelineSourcesFromHeaders consumes. Decoupling from the concrete
// VariantHeader type lets framework/ui avoid an upward dependency on
// framework/image. Callers using framework/image can adapt their
// []VariantHeader with a one-line loop or by writing a typed
// adapter helper in their own code.
//
// Note: Format from VariantHeader is intentionally omitted — MIME
// is the discriminator the <source type="..."> attribute actually
// needs, and a parallel `Format string` field would drift in type
// vs VariantHeader.Format (image.Format enum).
type HeaderInfo struct {
	Name   string
	Width  int
	Height int
	MIME   string
}

// PipelineSourcesFromHeaders bridges framework/image's variant pipeline
// to a typed PipelineSource slice. Given a URL function that maps a
// variant's Name to its public URL (e.g. through a storage backend),
// build the slice that goes into PipelineImageConfig.Sources without
// re-deriving MIME or width from filenames.
//
// Empty headers are skipped (Width==0 or URL=="").
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
	// Fallback is the <img>'s src — required, used by browsers that
	// can't pick from Sources. Typically a mid-size JPEG / PNG.
	Fallback string

	// Alt — required for non-decorative images.
	Alt string

	// Width and Height are the intrinsic dimensions of Fallback.
	// Setting them is mandatory to avoid CLS.
	Width, Height int

	// Sources is the typed responsive set; one <source> element is
	// emitted per distinct Type, grouping every PipelineSource with
	// that type into a single srcset.
	//
	// Groups are emitted in the order their Type first appears, so
	// putting the modern format (WebP) before the legacy one makes
	// older browsers fall through to the Fallback <img>.
	Sources []PipelineSource

	// Sizes is the CSS sizes attribute. Default "100vw".
	Sizes string

	// Placeholder accepts either a data: URL (LQIP) or a BlurHash
	// string. The component sets data-placeholder for data: URLs and
	// data-blurhash for anything else. Consumers wire those attributes
	// to a CSS background or a JS hydrator as they see fit.
	Placeholder string

	Eager        bool
	HighPriority bool
	Fit          ImageFit
	Aspect       ImageAspect
	Rounded      bool

	ID, Class string
}

// PipelineImage renders <picture> with one <source> per MIME type, plus
// a CLS-safe <img> fallback and an optional placeholder. Built to
// consume framework/image.VariantSet output directly: take the
// VariantResult.Variants slice, map each entry to a PipelineSource,
// pass the BlurHash or Placeholder as the placeholder field.
//
// Shares the ui-image visual surface with OptimizedImage; the
// distinction is multi-Type srcset support, intended for output of
// the framework's image pipeline where the same source has been
// encoded as both modern (WebP) and legacy (JPEG/PNG) variants.
func PipelineImage(cfg PipelineImageConfig) render.HTML {
	// Programmer errors (empty Fallback URL, missing Alt on a non-
	// decorative image) still panic — these are bugs in the caller's
	// code, not data-shape issues. Missing intrinsic dimensions are
	// a different story: user-generated content frequently lacks
	// them (old DB rows, malformed uploads), and crashing the render
	// path on that data turns into a 500. Instead we omit the width/
	// height attributes when zero, accepting the CLS cost as a
	// degraded-but-functional fallback.
	if cfg.Fallback == "" {
		panic("ui: PipelineImage requires Fallback")
	}
	if cfg.Alt == "" && !strings.Contains(cfg.Class, "ui-image--decorative") {
		panic("ui: PipelineImage requires Alt (or add ui-image--decorative to Class for intentional decorative images with alt=\"\")")
	}

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
	if cfg.Class != "" {
		cls += " " + cfg.Class
	}

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
	if cfg.Placeholder != "" {
		if strings.HasPrefix(cfg.Placeholder, "data:") {
			imgAttrs["data-placeholder"] = cfg.Placeholder
		} else {
			imgAttrs["data-blurhash"] = cfg.Placeholder
		}
	}

	img := html.Image(html.ImageConfig{
		Src:        cfg.Fallback,
		Alt:        cfg.Alt,
		Class:      "ui-image__img",
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
	picture := render.Tag("picture", nil, children...)

	return imageStyle.WrapHTML(html.Span(html.TextConfig{
		Class: cls, ID: cfg.ID,
	}, picture))
}

type pipelineGroup struct {
	typ    string
	srcset string
}

// encodeSrcsetURL percent-escapes the four characters that the HTML
// srcset parser uses as separators: ',' splits candidates, and any
// ASCII whitespace splits URL from descriptor. Storage URLs that
// contain raw commas (presigned URLs, keys with comma-separated
// segments) would otherwise be split into multiple malformed
// candidates and the wrong (or no) image would be fetched.
func encodeSrcsetURL(url string) string {
	if !strings.ContainsAny(url, ", \t\n\r") {
		return url
	}
	var b strings.Builder
	b.Grow(len(url) + 8)
	for i := 0; i < len(url); i++ {
		switch url[i] {
		case ',':
			b.WriteString("%2C")
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

// groupPipelineSources buckets PipelineSources by MIME type, preserving
// the input ordering of types' first appearance, and sorts widths
// within each bucket ascending for predictable srcset output.
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
