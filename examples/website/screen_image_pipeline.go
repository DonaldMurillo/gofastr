package main

import (
	"encoding/base64"
	stdimage "image"
	"image/color"
	"strconv"

	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework/image"
	"github.com/DonaldMurillo/gofastr/framework/ui"
)

// ImagePipelineScreen showcases the framework/image package: every chain
// operation is applied to a synthetic gradient source at render time and
// the resulting data: URLs are embedded directly into the page. No binary
// assets are required, so e2e tests stay hermetic.
type ImagePipelineScreen struct{}

func (s *ImagePipelineScreen) ScreenTitle() string { return "Image pipeline" }
func (s *ImagePipelineScreen) ScreenDescription() string {
	return "Chainable image transformations — Resize, Rotate, Flip, Modulate, Placeholder, BlurHash — pure-Go, zero CGo."
}
func (s *ImagePipelineScreen) ScreenType() app.ScreenType { return app.ScreenPage }

func (s *ImagePipelineScreen) Render() render.HTML {
	src := buildDemoGradient(200, 120)
	pipeline := image.FromImage(src, image.FormatPNG)

	sourceURL := mustDataURL(pipeline.PNG())
	resizeURL := mustDataURL(pipeline.Resize(80, 48).PNG())
	rotateURL := mustDataURL(pipeline.Rotate(90).PNG())
	flipURL := mustDataURL(pipeline.Flip().PNG())
	flopURL := mustDataURL(pipeline.Flop().PNG())
	brightURL := mustDataURL(pipeline.Modulate(image.Modulation{Brightness: image.Float64(1.4)}).PNG())
	deSatURL := mustDataURL(pipeline.Modulate(image.Modulation{Saturation: image.Float64(0.2)}).PNG())
	placeholderURL, _ := pipeline.Placeholder()
	blurhash, _ := pipeline.Resize(32, 24).BlurHash(4, 3)

	imgTag := func(testID, alt, src string) render.HTML {
		return render.VoidTag("img", map[string]string{
			"data-test": testID,
			"alt":       alt,
			"src":       src,
			"loading":   "lazy",
		})
	}

	card := func(title, testID, src string) render.HTML {
		return render.Tag("figure", map[string]string{"class": "img-pipeline-card"},
			imgTag(testID, title, src),
			render.Tag("figcaption", nil, render.Text(title)),
		)
	}

	grid := render.Tag("div",
		map[string]string{
			"class":              "img-pipeline-grid",
			"data-test":          "img-pipeline-grid",
			"data-source-format": pipeline.Format().String(),
		},
		card("Source 200×120", "img-pipeline-source", sourceURL),
		card("Resize 80×48", "img-pipeline-resize", resizeURL),
		card("Rotate 90°", "img-pipeline-rotate", rotateURL),
		card("Flip (vertical)", "img-pipeline-flip", flipURL),
		card("Flop (horizontal)", "img-pipeline-flop", flopURL),
		card("Brightness 1.4", "img-pipeline-brightness", brightURL),
		card("Saturation 0.2", "img-pipeline-saturation", deSatURL),
		card("Placeholder (LQIP)", "img-pipeline-placeholder", placeholderURL),
	)

	hashBlock := render.Tag("div", map[string]string{"class": "img-pipeline-blurhash"},
		render.Tag("strong", nil, render.Text("BlurHash 4×3: ")),
		render.Tag("code", map[string]string{"data-test": "img-pipeline-blurhash"},
			render.Text(blurhash)),
	)

	variantsBlock, variantSummary := renderVariantsSection(pipeline)

	source := `import "github.com/DonaldMurillo/gofastr/framework/image"

img, err := image.Open("photo.jpg")
if err != nil { /* … */ }

thumb, _ := img.
    AutoOrient().
    Resize(800, 0, image.WithFit(image.FitInside)).
    JPEG(image.JPEGOptions{Quality: 80}).
    Bytes()

lqip, _   := img.Placeholder()                  // data:image/jpeg;base64,…
hash, _   := img.Resize(32, 0).BlurHash(4, 3)   // "LEHV6nWB2yk8…"`

	return render.Tag("div", nil,
		backLink(),
		primitiveLede("Image pipeline",
			"Resize, rotate, flip, modulate, placeholder, and BlurHash — chainable, pure-Go, zero CGo. Uses image/jpeg, image/png, golang.org/x/image, and a hand-written BlurHash encoder. WebP-lossy / HEIC / AVIF are intentionally unsupported."),
		html.Heading(html.HeadingConfig{Level: 2}, render.Text("All operations on one source")),
		grid,
		hashBlock,
		html.Heading(html.HeadingConfig{Level: 2}, render.Text("VariantSet → PipelineImage")),
		render.Tag("p", map[string]string{"class": "lede"}, render.Text(
			"One declarative call produces every size and format in one pass. The typed VariantResult feeds straight into ui.PipelineImage for a multi-format <picture> with placeholder fallback.")),
		variantsBlock,
		variantSummary,
		html.Heading(html.HeadingConfig{Level: 2}, render.Text("Source")),
		demoFrame(render.Text(""), source),
	)
}

// renderVariantsSection runs VariantSet on the synthetic gradient,
// embeds the resulting variant bytes as data: URLs, and wires them
// into a ui.PipelineImage. The returned summary lists each variant's
// name / MIME / size so the e2e test can assert against it.
func renderVariantsSection(src *image.Image) (render.HTML, render.HTML) {
	set := image.VariantSet{
		BaseName: "demo",
		Variants: []image.Variant{
			{Width: 80, Format: image.FormatWebP, Suffix: "sm"},
			{Width: 160, Format: image.FormatWebP, Suffix: "md"},
			{Width: 80, Format: image.FormatJPEG, Quality: 80, Suffix: "sm"},
			{Width: 160, Format: image.FormatJPEG, Quality: 82, Suffix: "md"},
		},
		Placeholder: &image.PlaceholderOptions{Width: 16},
		BlurHashX:   4, BlurHashY: 3,
	}
	result, err := set.Process(src)
	if err != nil {
		return render.Tag("p", nil, render.Text("VariantSet error: "+err.Error())), render.Text("")
	}

	sources := make([]ui.PipelineSource, 0, len(result.Variants))
	var fallback string
	for _, v := range result.Variants {
		url := "data:" + v.MIME + ";base64," + base64.StdEncoding.EncodeToString(v.Bytes)
		sources = append(sources, ui.PipelineSource{URL: url, Width: v.Width, Type: v.MIME})
		if v.Format == image.FormatJPEG && v.Width == 160 {
			fallback = url
		}
	}

	picture := ui.PipelineImage(ui.PipelineImageConfig{
		Fallback:    fallback,
		Alt:         "Pipeline-generated gradient",
		Width:       160, Height: 96,
		Sources:     sources,
		Placeholder: result.Placeholder,
		Class:       "img-pipeline-variant-output",
	})

	rows := make([]render.HTML, 0, len(result.Variants))
	for _, v := range result.Variants {
		rows = append(rows, render.Tag("li",
			map[string]string{"data-test": "img-pipeline-variant-" + v.Name},
			render.Text(v.Name+" — "+v.MIME+" — "+strconv.Itoa(v.Width)+"×"+strconv.Itoa(v.Height)+" — "+strconv.Itoa(len(v.Bytes))+"B"),
		))
	}
	summary := render.Tag("ul",
		map[string]string{"class": "img-pipeline-variant-list", "data-test": "img-pipeline-variant-list"},
		rows...)

	return render.Tag("div",
		map[string]string{"class": "img-pipeline-variant", "data-test": "img-pipeline-variant"},
		picture), summary
}


// buildDemoGradient produces a deterministic RGBA test image. Keeping it
// synthetic means the demo page renders identically regardless of what
// assets ship with the repo and the e2e test can rely on byte-for-byte
// stability.
func buildDemoGradient(w, h int) *stdimage.RGBA {
	img := stdimage.NewRGBA(stdimage.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.SetRGBA(x, y, color.RGBA{
				R: uint8(x * 255 / (w - 1)),
				G: uint8(y * 255 / (h - 1)),
				B: uint8(((x + y) * 255) / (w + h - 2)),
				A: 255,
			})
		}
	}
	return img
}

// mustDataURL encodes the pipeline and returns its data: URL, swallowing
// errors (the inputs here are statically constructed and cannot fail).
func mustDataURL(enc *image.Encoder) string {
	durl, err := enc.DataURL()
	if err != nil {
		return ""
	}
	return durl
}
