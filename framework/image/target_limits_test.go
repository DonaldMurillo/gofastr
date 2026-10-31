package image

import (
	"errors"
	stdimage "image"
	"image/color"
	"io"
	"math"
	"strings"
	"testing"
)

// Two variants composing one file name overwrote each other in storage
// while the result listed both (2026-10-27 F7).
func TestVariantSetRefusesDuplicateNames(t *testing.T) {
	src := FromImage(gradient(40, 30), FormatPNG)
	for name, vs := range map[string][]Variant{
		"same suffix":          {{Width: 10, Suffix: "x", Format: FormatJPEG}, {Width: 20, Suffix: "x", Format: FormatJPEG}},
		"suffix equals width":  {{Width: 10, Format: FormatPNG}, {Width: 20, Suffix: "10", Format: FormatPNG}},
		"same width no suffix": {{Width: 10, Format: FormatGIF}, {Width: 10, Format: FormatGIF, Quality: 50}},
	} {
		t.Run(name, func(t *testing.T) {
			if err := CheckVariantNames(vs); err == nil || !strings.Contains(err.Error(), "Variants[0]") || !strings.Contains(err.Error(), "Variants[1]") {
				t.Fatalf("CheckVariantNames: %v, want both indices named", err)
			}
			if _, err := (VariantSet{Variants: vs}).Process(src); err == nil {
				t.Fatal("Process accepted colliding variants")
			}
			if _, err := (VariantSet{Variants: vs}).ProcessTo(src, discardSink); err == nil {
				t.Fatal("ProcessTo accepted colliding variants")
			}
		})
	}
	// Same suffix, different format: different extensions, no collision.
	ok := []Variant{{Width: 10, Suffix: "x", Format: FormatJPEG}, {Width: 10, Suffix: "x", Format: FormatPNG}}
	if err := CheckVariantNames(ok); err != nil {
		t.Fatalf("distinct extensions: %v", err)
	}
}

// A resize target past MaxPixels is an error on the paths that return
// one and a named panic from Resize, not a runtime allocation panic
// (2026-10-27 F8).
func TestResizeTargetPastMaxPixels(t *testing.T) {
	src := FromImage(solidRGBA(8, 8, color.RGBA{1, 2, 3, 255}), FormatPNG)
	huge := []Variant{{Width: math.MaxInt / 2, Format: FormatPNG}}
	if _, err := (VariantSet{Variants: huge, AllowUpscale: true}).Process(src); !errors.Is(err, ErrTargetTooLarge) {
		t.Fatalf("Process: %v, want ErrTargetTooLarge", err)
	}
	if _, err := (VariantSet{Variants: huge, AllowUpscale: true}).ProcessTo(src, discardSink); !errors.Is(err, ErrTargetTooLarge) {
		t.Fatalf("ProcessTo: %v, want ErrTargetTooLarge", err)
	}
	if _, err := src.Placeholder(PlaceholderOptions{Width: 1 << 20}); !errors.Is(err, ErrTargetTooLarge) {
		t.Fatalf("Placeholder: %v, want ErrTargetTooLarge", err)
	}
	// Just under the cap on a small Config still works; just over fails.
	small := src.withMaxPixels(100)
	wide := FromImage(solidRGBA(16, 1, color.RGBA{1, 2, 3, 255}), FormatPNG)
	if got := small.Resize(10, 10).Bounds().Dx(); got != 10 {
		t.Fatalf("10x10 under a 100-pixel cap: width %d", got)
	}
	for name, fn := range map[string]func(){
		"over cap":          func() { small.Resize(11, 10) },
		"aspect overflow":   func() { src.Resize(math.MaxInt, 0) },
		"fit outside huge":  func() { src.Resize(math.MaxInt/4, 1, WithFit(FitOutside)) },
		"area past 64 bits": func() { src.Resize(1<<33, 1<<33) },
		// 16*MaxInt as a float is past the int range: amd64 wraps the
		// conversion to MinInt, which would clamp to 1 and pass the cap.
		"fit side past int": func() { wide.Resize(1, math.MaxInt, WithFit(FitOutside)) },
	} {
		t.Run(name, func(t *testing.T) {
			defer func() {
				err, _ := recover().(error)
				if !errors.Is(err, ErrTargetTooLarge) {
					t.Fatalf("panic %v, want ErrTargetTooLarge", err)
				}
			}()
			fn()
		})
	}
}

// A derived side past the int range is refused even when the other side
// is small: wrapped negative it would clamp to 1 and pass the area check.
func TestDerivedSidePastIntRange(t *testing.T) {
	tall := FromImage(boundsOnly{stdimage.Rect(0, 0, 1, 1<<62)}, FormatPNG)
	wide := FromImage(boundsOnly{stdimage.Rect(0, 0, 1<<62, 1)}, FormatPNG)
	for name, err := range map[string]error{
		"height past int":     tall.checkResize(3, 0),
		"height past 64 bits": tall.checkResize(4, 0),
		"width past 64 bits":  wide.checkResize(0, 4),
	} {
		if !errors.Is(err, ErrTargetTooLarge) {
			t.Errorf("%s: %v, want ErrTargetTooLarge", name, err)
		}
	}
}

// boundsOnly is an image with bounds and no pixels, for size arithmetic.
type boundsOnly struct{ r stdimage.Rectangle }

func (b boundsOnly) ColorModel() color.Model    { return color.RGBAModel }
func (b boundsOnly) Bounds() stdimage.Rectangle { return b.r }
func (b boundsOnly) At(int, int) color.Color    { return color.RGBA{} }

// NewGradient allocated whatever it was asked for; past DefaultMaxPixels
// it is the same error a resize gets.
func TestGradientPastMaxPixels(t *testing.T) {
	// The overflowing case runs first: with no cap it panics at once,
	// where 2^40 pixels would try to allocate 4 TiB.
	if _, err := NewGradient(math.MaxInt, 2, "#000000", "#FFFFFF"); !errors.Is(err, ErrTargetTooLarge) {
		t.Fatalf("MaxInt x 2: %v, want ErrTargetTooLarge", err)
	}
	if _, err := NewGradient(8193, 8192, "#000000", "#FFFFFF"); !errors.Is(err, ErrTargetTooLarge) {
		t.Fatalf("one column past 64 MP: %v, want ErrTargetTooLarge", err)
	}
}

func discardSink(VariantHeader, io.Reader) error { return nil }

// withMaxPixels is i with its pixel cap set to n.
func (i *Image) withMaxPixels(n int64) *Image {
	c := *i
	c.cfg.MaxPixels = n
	return &c
}
