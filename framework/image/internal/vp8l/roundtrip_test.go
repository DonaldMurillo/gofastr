package vp8l

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"strings"
	"testing"

	"golang.org/x/image/webp"
)

// roundTripTo encodes src and decodes via x/image/webp.
func roundTripTo(t *testing.T, src image.Image) image.Image {
	t.Helper()
	var buf bytes.Buffer
	if err := Encode(&buf, src); err != nil {
		t.Fatalf("Encode: %v", err)
	}
	out, err := webp.Decode(&buf)
	if err != nil {
		t.Fatalf("webp.Decode: %v", err)
	}
	return out
}

func assertNRGBAEqual(t *testing.T, want, got image.Image) {
	t.Helper()
	wb, gb := want.Bounds(), got.Bounds()
	if wb.Dx() != gb.Dx() || wb.Dy() != gb.Dy() {
		t.Fatalf("bounds: %v vs %v", wb, gb)
	}
	for y := 0; y < wb.Dy(); y++ {
		for x := 0; x < wb.Dx(); x++ {
			w := color.NRGBAModel.Convert(want.At(wb.Min.X+x, wb.Min.Y+y)).(color.NRGBA)
			g := color.NRGBAModel.Convert(got.At(gb.Min.X+x, gb.Min.Y+y)).(color.NRGBA)
			if w != g {
				t.Fatalf("pixel (%d,%d): want %v got %v", x, y, w, g)
			}
		}
	}
}

func TestRoundTripTypeRGBA(t *testing.T) {
	src := image.NewRGBA(image.Rect(0, 0, 8, 6))
	for y := 0; y < 6; y++ {
		for x := 0; x < 8; x++ {
			src.SetRGBA(x, y, color.RGBA{R: uint8(x * 16), G: uint8(y * 32), B: 64, A: 128})
		}
	}
	out := roundTripTo(t, src)
	assertNRGBAEqual(t, src, out)
}

func TestRoundTripTypeNRGBA(t *testing.T) {
	src := image.NewNRGBA(image.Rect(0, 0, 8, 6))
	for y := 0; y < 6; y++ {
		for x := 0; x < 8; x++ {
			src.SetNRGBA(x, y, color.NRGBA{R: uint8(x * 32), G: uint8(y * 40), B: 200, A: uint8(x*y) % 255})
		}
	}
	out := roundTripTo(t, src)
	assertNRGBAEqual(t, src, out)
}

func TestRoundTripTypeGray(t *testing.T) {
	src := image.NewGray(image.Rect(0, 0, 8, 6))
	for y := 0; y < 6; y++ {
		for x := 0; x < 8; x++ {
			src.SetGray(x, y, color.Gray{Y: uint8(x*y) % 255})
		}
	}
	out := roundTripTo(t, src)
	assertNRGBAEqual(t, src, out)
}

func TestRoundTripTypePaletted(t *testing.T) {
	pal := color.Palette{
		color.RGBA{0, 0, 0, 255},
		color.RGBA{255, 0, 0, 255},
		color.RGBA{0, 255, 0, 255},
		color.RGBA{0, 0, 255, 255},
	}
	src := image.NewPaletted(image.Rect(0, 0, 8, 6), pal)
	for y := 0; y < 6; y++ {
		for x := 0; x < 8; x++ {
			src.SetColorIndex(x, y, uint8((x+y)%4))
		}
	}
	out := roundTripTo(t, src)
	assertNRGBAEqual(t, src, out)
}

func TestRoundTripTypeYCbCr(t *testing.T) {
	src := image.NewYCbCr(image.Rect(0, 0, 8, 6), image.YCbCrSubsampleRatio420)
	for i := range src.Y {
		src.Y[i] = uint8(i * 13)
	}
	for i := range src.Cb {
		src.Cb[i] = 128
		src.Cr[i] = 128
	}
	out := roundTripTo(t, src)
	assertNRGBAEqual(t, src, out)
}

func TestRoundTripTypeCMYK(t *testing.T) {
	src := image.NewCMYK(image.Rect(0, 0, 8, 6))
	for y := 0; y < 6; y++ {
		for x := 0; x < 8; x++ {
			src.SetCMYK(x, y, color.CMYK{C: uint8(x * 16), M: uint8(y * 32), Y: 100, K: 10})
		}
	}
	out := roundTripTo(t, src)
	assertNRGBAEqual(t, src, out)
}

// customRGBA64Image is a user-defined image returning RGBA64-model colors,
// exercising the non-NRGBA fast-path inside sampleRGBA8.
type customRGBA64Image struct{ w, h int }

func (m *customRGBA64Image) ColorModel() color.Model { return color.RGBA64Model }
func (m *customRGBA64Image) Bounds() image.Rectangle { return image.Rect(0, 0, m.w, m.h) }
func (m *customRGBA64Image) At(x, y int) color.Color {
	return color.RGBA64{
		R: uint16(x*8000) % 65535,
		G: uint16(y*8000) % 65535,
		B: 32000,
		A: 65535,
	}
}

func TestRoundTripTypeCustomImage(t *testing.T) {
	src := &customRGBA64Image{w: 8, h: 6}
	_, err := func() (image.Image, error) {
		var buf bytes.Buffer
		if err := Encode(&buf, src); err != nil {
			return nil, err
		}
		return webp.Decode(&buf)
	}()
	if err != nil {
		t.Fatalf("custom image encode/decode: %v", err)
	}
}

// ---- extreme aspect ratios ----

func TestRoundTripAR16384x1(t *testing.T) {
	src := image.NewNRGBA(image.Rect(0, 0, 16384, 1))
	for x := 0; x < 16384; x++ {
		src.SetNRGBA(x, 0, color.NRGBA{R: uint8(x), G: uint8(x >> 4), B: uint8(x >> 8), A: 255})
	}
	out := roundTripTo(t, src)
	assertNRGBAEqual(t, src, out)
}

func TestRoundTripAR1x16384(t *testing.T) {
	src := image.NewNRGBA(image.Rect(0, 0, 1, 16384))
	for y := 0; y < 16384; y++ {
		src.SetNRGBA(0, y, color.NRGBA{R: uint8(y), G: uint8(y >> 4), B: uint8(y >> 8), A: 255})
	}
	out := roundTripTo(t, src)
	assertNRGBAEqual(t, src, out)
}

func TestRoundTripAR16385x1Rejected(t *testing.T) {
	src := image.NewNRGBA(image.Rect(0, 0, 16385, 1))
	var buf bytes.Buffer
	err := Encode(&buf, src)
	if err == nil {
		t.Fatal("expected error for 16385x1")
	}
	if !strings.Contains(err.Error(), "too large") {
		t.Fatalf("expected 'too large' error, got: %v", err)
	}
}

// ---- alpha variations ----

func TestRoundTripAlphaAllZero(t *testing.T) {
	src := image.NewNRGBA(image.Rect(0, 0, 32, 32))
	for i := 0; i < len(src.Pix); i += 4 {
		src.Pix[i+0] = uint8(i % 255)
		src.Pix[i+1] = uint8(i % 200)
		src.Pix[i+2] = uint8(i % 150)
		src.Pix[i+3] = 0
	}
	out := roundTripTo(t, src)
	assertNRGBAEqual(t, src, out)
}

func TestRoundTripAlphaVaryGrayMid(t *testing.T) {
	src := image.NewNRGBA(image.Rect(0, 0, 32, 32))
	for y := 0; y < 32; y++ {
		for x := 0; x < 32; x++ {
			src.SetNRGBA(x, y, color.NRGBA{R: 128, G: 128, B: 128, A: uint8((x*8 + y*8) % 255)})
		}
	}
	out := roundTripTo(t, src)
	assertNRGBAEqual(t, src, out)
}

// ---- wraparound boundary ----

func TestRoundTripWrapAroundBoundary(t *testing.T) {
	src := image.NewNRGBA(image.Rect(0, 0, 64, 64))
	for y := 0; y < 64; y++ {
		for x := 0; x < 64; x++ {
			var r uint8
			switch x % 3 {
			case 0:
				r = 255
			case 1:
				r = 0
			case 2:
				r = 255
			}
			src.SetNRGBA(x, y, color.NRGBA{
				R: r,
				G: uint8((y % 2) * 255),
				B: uint8(((x + y) % 2) * 255),
				A: 255,
			})
		}
	}
	out := roundTripTo(t, src)
	assertNRGBAEqual(t, src, out)
}

// TestStressPremultipliedZeroAlphaOddRGB tests the foot-gun of image.RGBA
// where alpha=0 but RGB!=0 is technically invalid (premultiplied invariant
// requires RGB <= A). sampleRGBA8 routes RGBA through color.NRGBAModel.Convert.
func TestRoundTripPremultipliedZeroAlphaOddRGB(t *testing.T) {
	src := image.NewRGBA(image.Rect(0, 0, 4, 1))
	// Manually poke pixel bytes — A=0 but R=128, an invalid premul state.
	src.Pix[0] = 128
	src.Pix[1] = 0
	src.Pix[2] = 0
	src.Pix[3] = 0
	src.Pix[4] = 0
	src.Pix[5] = 128
	src.Pix[6] = 0
	src.Pix[7] = 0
	src.Pix[8] = 0
	src.Pix[9] = 0
	src.Pix[10] = 128
	src.Pix[11] = 0
	src.Pix[12] = 64
	src.Pix[13] = 64
	src.Pix[14] = 64
	src.Pix[15] = 128
	out := roundTripTo(t, src)
	// What we want is the *invariant* that NRGBAModel.Convert of input
	// equals NRGBAModel.Convert of output.
	assertNRGBAEqual(t, src, out)
	_ = fmt.Sprintf
}
