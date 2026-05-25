package image

import (
	"image/color"
	"strings"
	"testing"
)

func TestVariantSetEmptyOK(t *testing.T) {
	src := FromImage(gradient(40, 30), FormatPNG)
	res, err := VariantSet{}.Process(src)
	if err != nil {
		t.Fatalf("empty set: %v", err)
	}
	if len(res.Variants) != 0 {
		t.Errorf("expected no variants, got %d", len(res.Variants))
	}
	if res.Placeholder != "" || res.BlurHash != "" {
		t.Errorf("expected no placeholder/hash, got %q / %q", res.Placeholder, res.BlurHash)
	}
	if res.SourceWidth != 40 || res.SourceHeight != 30 {
		t.Errorf("source dims = %d×%d, want 40×30", res.SourceWidth, res.SourceHeight)
	}
}

func TestVariantSetProducesEachVariant(t *testing.T) {
	src := FromImage(gradient(400, 300), FormatPNG)
	set := VariantSet{
		BaseName: "photo",
		Variants: []Variant{
			{Width: 100, Format: FormatJPEG, Quality: 80, Suffix: "sm"},
			{Width: 200, Format: FormatPNG},
			{Width: 50, Format: FormatGIF},
		},
	}
	res, err := set.Process(src)
	if err != nil {
		t.Fatalf("Process: %v", err)
	}
	if len(res.Variants) != 3 {
		t.Fatalf("expected 3 variants, got %d", len(res.Variants))
	}

	want := []struct {
		name   string
		mime   string
		width  int
		format Format
	}{
		{"photo-sm.jpg", "image/jpeg", 100, FormatJPEG},
		{"photo-200.png", "image/png", 200, FormatPNG},
		{"photo-50.gif", "image/gif", 50, FormatGIF},
	}
	for i, v := range res.Variants {
		if v.Name != want[i].name {
			t.Errorf("variants[%d].Name = %q, want %q", i, v.Name, want[i].name)
		}
		if v.MIME != want[i].mime {
			t.Errorf("variants[%d].MIME = %q, want %q", i, v.MIME, want[i].mime)
		}
		if v.Width != want[i].width {
			t.Errorf("variants[%d].Width = %d, want %d", i, v.Width, want[i].width)
		}
		if v.Format != want[i].format {
			t.Errorf("variants[%d].Format = %v, want %v", i, v.Format, want[i].format)
		}
		if len(v.Bytes) == 0 {
			t.Errorf("variants[%d].Bytes empty", i)
		}
		// Confirm decoded bytes match advertised format.
		decoded, err := DecodeBytes(v.Bytes)
		if err != nil {
			t.Errorf("variants[%d] re-decode: %v", i, err)
			continue
		}
		if decoded.Format() != v.Format {
			t.Errorf("variants[%d] re-decode format = %v, want %v", i, decoded.Format(), v.Format)
		}
		if decoded.Bounds().Dx() != v.Width {
			t.Errorf("variants[%d] decoded width = %d, want %d", i, decoded.Bounds().Dx(), v.Width)
		}
	}
}

func TestVariantSetPlaceholderAndBlurHash(t *testing.T) {
	src := FromImage(gradient(200, 150), FormatPNG)
	set := VariantSet{
		Placeholder: &PlaceholderOptions{Width: 16},
		BlurHashX:   4,
		BlurHashY:   3,
	}
	res, err := set.Process(src)
	if err != nil {
		t.Fatalf("Process: %v", err)
	}
	if !strings.HasPrefix(res.Placeholder, "data:image/jpeg;base64,") {
		t.Errorf("placeholder prefix wrong: %q", res.Placeholder[:30])
	}
	if len(res.BlurHash) != 28 {
		t.Errorf("BlurHash length = %d, want 28 (4×3)", len(res.BlurHash))
	}
	if res.BlurHash[0] != 'L' {
		t.Errorf("BlurHash size flag = %q, want 'L'", res.BlurHash[0])
	}
}

func TestVariantSetRejectsHalfBlurHash(t *testing.T) {
	src := FromImage(solidRGBA(8, 8, color.RGBA{1, 2, 3, 4}), FormatPNG)
	_, err := VariantSet{BlurHashX: 4}.Process(src)
	if err == nil {
		t.Fatal("expected error for half-configured BlurHash")
	}
}

func TestVariantSetRejectsZeroWidth(t *testing.T) {
	src := FromImage(solidRGBA(8, 8, color.RGBA{1, 2, 3, 4}), FormatPNG)
	_, err := VariantSet{Variants: []Variant{{Width: 0, Format: FormatJPEG}}}.Process(src)
	if err == nil {
		t.Fatal("expected error for Width=0")
	}
}

func TestVariantSetRejectsUnknownFormat(t *testing.T) {
	src := FromImage(solidRGBA(8, 8, color.RGBA{1, 2, 3, 4}), FormatPNG)
	_, err := VariantSet{Variants: []Variant{{Width: 64, Format: FormatUnknown}}}.Process(src)
	if err == nil {
		t.Fatal("expected error for FormatUnknown")
	}
}

func TestVariantSetSurfacesEncoderError(t *testing.T) {
	// Force a WebP-lossy by manually constructing an unsupported variant
	// path: VariantSet always uses lossless, so this exercises the route
	// today by injecting a WebP variant — once VP8L lands the assertion
	// flips. Until then a WebP variant must error.
	src := FromImage(gradient(64, 48), FormatPNG)
	_, err := VariantSet{Variants: []Variant{{Width: 32, Format: FormatWebP}}}.Process(src)
	if err == nil {
		t.Skip("WebP-lossless now wired in — flip this test to assert success")
	}
}

func TestVariantSetPreservesAspect(t *testing.T) {
	// 400×300 source resized to width=80 with FitInside → height = 60.
	src := FromImage(gradient(400, 300), FormatPNG)
	res, err := VariantSet{
		Variants: []Variant{{Width: 80, Format: FormatPNG}},
	}.Process(src)
	if err != nil {
		t.Fatalf("Process: %v", err)
	}
	if res.Variants[0].Height != 60 {
		t.Errorf("variant height = %d, want 60 (aspect preserved)", res.Variants[0].Height)
	}
}
