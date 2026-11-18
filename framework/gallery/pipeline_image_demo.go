package gallery

import (
	stdimage "image"
	"image/color"
	"image/draw"
	"sync"

	"github.com/DonaldMurillo/gofastr/core/render"
	fwimage "github.com/DonaldMurillo/gofastr/framework/image"
	"github.com/DonaldMurillo/gofastr/framework/ui"
)

type pipelineDemo struct {
	image render.HTML
	hash  string
}

var (
	pipelineDemoOnce sync.Once
	pipelineDemoVal  pipelineDemo
)

func pipelineImageDemo() pipelineDemo {
	pipelineDemoOnce.Do(func() {
		pipelineDemoVal = buildPipelineImageDemo()
	})
	return pipelineDemoVal
}

// demoMockup draws a crude dashboard mock with crisp-edged blocks.
func demoMockup(w, h int) *fwimage.Image {
	canvas := stdimage.NewNRGBA(stdimage.Rect(0, 0, w, h))
	block := func(x0, y0, x1, y1 int, c color.NRGBA) {
		draw.Draw(canvas, stdimage.Rect(x0, y0, x1, y1), &stdimage.Uniform{C: c}, stdimage.Point{}, draw.Src)
	}
	var (
		page   = color.NRGBA{R: 0xFA, G: 0xF7, B: 0xF2, A: 0xFF}
		ink    = color.NRGBA{R: 0x24, G: 0x21, B: 0x1C, A: 0xFF}
		amber  = color.NRGBA{R: 0xF2, G: 0xB1, B: 0x4D, A: 0xFF}
		teal   = color.NRGBA{R: 0x0E, G: 0x7C, B: 0x86, A: 0xFF}
		indigo = color.NRGBA{R: 0x43, G: 0x38, B: 0xCA, A: 0xFF}
	)
	block(0, 0, w, h, page)
	block(0, 0, w, h/8, ink)                       // header bar
	block(w/24, h/5, w/2, h-h/6, indigo)           // chart panel
	block(w/2+w/24, h/5, w-w/24, h/2, amber)       // card one
	block(w/2+w/24, h/2+h/24, w-w/24, h-h/6, teal) // card two
	block(w/24, h-h/9, w/3, h-h/9+h/40, ink)       // footer rule
	return fwimage.FromImage(canvas, fwimage.FormatPNG)
}

func buildPipelineImageDemo() pipelineDemo {
	fail := func(msg string) pipelineDemo {
		return pipelineDemo{
			image: note(render.Text(msg)),
			hash:  "",
		}
	}
	src := demoMockup(480, 270)
	full, err := src.JPEG(fwimage.JPEGOptions{Quality: 78}).DataURL()
	if err != nil {
		return fail("Demo image could not be encoded.")
	}
	hash, err := src.BlurHash(4, 3)
	if err != nil {
		return fail("Demo BlurHash could not be computed.")
	}
	placeholder, err := fwimage.BlurHashDataURL(hash, fwimage.BlurHashRenderConfig{})
	if err != nil {
		return fail("Demo placeholder could not be decoded.")
	}

	return pipelineDemo{
		hash: hash,
		image: ui.Grid(ui.GridConfig{Min: "14rem"},
			ui.Stack(ui.StackConfig{Gap: ui.GapXS},
				ui.OptimizedImage(ui.OptimizedImageConfig{
					Src: placeholder, Alt: "The decoded BlurHash placeholder on its own.",
					Width: 480, Height: 270, Aspect: ui.ImageAspect16x9, Rounded: true,
				}),
				ui.Muted(render.Text("Placeholder, decoded from 28 characters")),
			),
			ui.Stack(ui.StackConfig{Gap: ui.GapXS},
				ui.PipelineImage(ui.PipelineImageConfig{
					Fallback: full, Alt: "A generated gradient standing in for a product photo.",
					Width: 480, Height: 270, Aspect: ui.ImageAspect16x9, Rounded: true,
					Placeholder: placeholder,
				}),
				ui.Muted(render.Text("Image with the placeholder behind it")),
			),
		),
	}
}
