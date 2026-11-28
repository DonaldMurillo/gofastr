package image

import (
	"errors"
	stdimage "image"
	"image/color"
	"io"
	"strings"
	"testing"
)

func TestFitPreservesExactLimitingWidth(t *testing.T) {
	src := FromImage(stdimage.NewRGBA(stdimage.Rect(0, 0, 7, 7)), FormatPNG)
	for _, tc := range []struct {
		fit    Fit
		height int
	}{{FitInside, 100}, {FitOutside, 40}} {
		got := src.Resize(61, tc.height, WithFit(tc.fit)).Bounds()
		if got.Dx() != 61 || got.Dy() != 61 {
			t.Errorf("fit %v: got %v, want 61x61", tc.fit, got)
		}
	}
}

func TestMetadataFindsInteriorAlphaInNRGBA64(t *testing.T) {
	src := stdimage.NewNRGBA64(stdimage.Rect(0, 0, 6, 6))
	for y := range 6 {
		for x := range 6 {
			src.SetNRGBA64(x, y, color.NRGBA64{A: 0xffff})
		}
	}
	src.SetNRGBA64(1, 2, color.NRGBA64{A: 0x8000})
	if !FromImage(src, FormatPNG).Metadata().HasAlpha {
		t.Error("missed an interior translucent pixel")
	}
}

type shortImageWriter struct{}

func (shortImageWriter) Write(p []byte) (int, error) { return len(p) - 1, nil }

func TestEncoderReportsShortWrite(t *testing.T) {
	src := FromImage(stdimage.NewRGBA(stdimage.Rect(0, 0, 2, 2)), FormatPNG)
	if err := src.PNG().Write(shortImageWriter{}); !errors.Is(err, io.ErrShortWrite) {
		t.Errorf("got %v, want io.ErrShortWrite", err)
	}
}

func TestDecodeBlurHashRejectsOutOfRangeFields(t *testing.T) {
	wrap := func(value, digits int) string { return string(appendBase83(nil, value, digits)) }
	for _, hash := range []string{
		wrap(81, 1) + strings.Repeat("0", 4+2*10-1),
		"00" + wrap(1<<24, 4),
		"100000" + wrap(19*19*19, 2),
	} {
		if _, err := DecodeBlurHash(hash, BlurHashRenderConfig{}); err == nil {
			t.Errorf("accepted malformed blurhash %q", hash)
		}
	}
}

func TestProcessToChecksAllVariantOptionsBeforeSink(t *testing.T) {
	src := FromImage(stdimage.NewRGBA(stdimage.Rect(0, 0, 2, 2)), FormatPNG)
	for _, set := range []VariantSet{
		{Variants: []Variant{{Width: 1, Format: FormatPNG}, {Width: 0, Format: FormatPNG}}},
		{Variants: []Variant{{Width: 1, Format: FormatPNG}, {Width: 2, Format: Format(99)}}},
		{Variants: []Variant{{Width: 1, Format: FormatPNG}}, BlurHashX: -1, BlurHashY: -1},
	} {
		deliveries := 0
		_, err := set.ProcessTo(src, func(VariantHeader, io.Reader) error { deliveries++; return nil })
		if err == nil || deliveries != 0 {
			t.Errorf("got (%v, %d deliveries), want validation error before the first delivery", err, deliveries)
		}
	}
}
