package image

import (
	"image/color"
	"strings"
	"testing"
)

// The reference vector from https://blurha.sh — a 4×3-component hash.
const refHash = "LEHV6nWB2yk8pyo0adR*.7kCMdnj"

func TestDecodeBlurHashRefVector(t *testing.T) {
	img, err := DecodeBlurHash(refHash, BlurHashRenderConfig{Width: 32, Height: 32})
	if err != nil {
		t.Fatalf("DecodeBlurHash: %v", err)
	}
	b := img.img.Bounds()
	if b.Dx() != 32 || b.Dy() != 32 {
		t.Fatalf("dims = %dx%d, want 32x32", b.Dx(), b.Dy())
	}
	// Every pixel must be fully opaque and in range — a decoded blur has
	// no transparency.
	for y := 0; y < b.Dy(); y++ {
		for x := 0; x < b.Dx(); x++ {
			_, _, _, a := img.img.At(x, y).RGBA()
			if a != 0xffff {
				t.Fatalf("pixel (%d,%d) alpha = %d, want opaque", x, y, a)
			}
		}
	}
}

// The DC term of a BlurHash is the average color. Decoding must reproduce
// it as the mean of the output, which is the strongest single check that
// the inverse transform matches the forward one.
func TestDecodeBlurHashDCIsAverage(t *testing.T) {
	src := FromImage(solidRGBA(64, 64, color.RGBA{R: 12, G: 200, B: 90, A: 255}), FormatPNG)
	hash, err := src.BlurHash(4, 3)
	if err != nil {
		t.Fatalf("BlurHash: %v", err)
	}
	got, err := DecodeBlurHash(hash, BlurHashRenderConfig{Width: 8, Height: 8})
	if err != nil {
		t.Fatalf("DecodeBlurHash: %v", err)
	}
	// A solid source has zero AC energy, so every decoded pixel should be
	// the source color within base83 quantisation error.
	r, g, b, _ := got.img.At(4, 4).RGBA()
	assertNear(t, int(r>>8), 12, 4, "R")
	assertNear(t, int(g>>8), 200, 4, "G")
	assertNear(t, int(b>>8), 90, 4, "B")
}

func TestDecodeBlurHashRoundTripsGradient(t *testing.T) {
	src, err := NewGradient(64, 64, "#000000", "#FFFFFF")
	if err != nil {
		t.Fatalf("NewGradient: %v", err)
	}
	hash, err := src.BlurHash(4, 4)
	if err != nil {
		t.Fatalf("BlurHash: %v", err)
	}
	got, err := DecodeBlurHash(hash, BlurHashRenderConfig{Width: 16, Height: 16})
	if err != nil {
		t.Fatalf("DecodeBlurHash: %v", err)
	}
	// The gradient runs dark top-left → light bottom-right, so the decode
	// must preserve that ordering even though it loses detail.
	tl, _, _, _ := got.img.At(1, 1).RGBA()
	br, _, _, _ := got.img.At(14, 14).RGBA()
	if tl >= br {
		t.Fatalf("gradient direction lost: top-left=%d bottom-right=%d", tl>>8, br>>8)
	}
}

func TestDecodeBlurHashPunchIncreasesContrast(t *testing.T) {
	src, err := NewGradient(64, 64, "#202020", "#E0E0E0")
	if err != nil {
		t.Fatalf("NewGradient: %v", err)
	}
	hash, err := src.BlurHash(4, 4)
	if err != nil {
		t.Fatalf("BlurHash: %v", err)
	}
	spread := func(punch float64) int {
		img, err := DecodeBlurHash(hash, BlurHashRenderConfig{Width: 16, Height: 16, Punch: punch})
		if err != nil {
			t.Fatalf("DecodeBlurHash(punch=%v): %v", punch, err)
		}
		lo, _, _, _ := img.img.At(1, 1).RGBA()
		hi, _, _, _ := img.img.At(14, 14).RGBA()
		return int(hi>>8) - int(lo>>8)
	}
	plain, punched := spread(1), spread(3)
	if punched <= plain {
		t.Fatalf("punch=3 spread (%d) should exceed punch=1 spread (%d)", punched, plain)
	}
}

func TestDecodeBlurHashRejectsMalformed(t *testing.T) {
	// A hash arrives from a DB column an upload wrote — it is untrusted
	// input and every rejection must happen before any allocation.
	cases := []struct{ name, hash string }{
		{"empty", ""},
		{"too short", "LEH"},
		// refHash is 4x3 => 28 chars; truncating breaks the length invariant.
		{"truncated", refHash[:27]},
		{"overlong", refHash + "X"},
		{"invalid base83 char", strings.Replace(refHash, "L", "\\", 1)},
		{"non-ascii", "LEHV6nWB2yk8pyo0adR*.7kCMdn€"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := DecodeBlurHash(tc.hash, BlurHashRenderConfig{}); err == nil {
				t.Fatalf("expected error for %q", tc.hash)
			}
		})
	}
}

func TestDecodeBlurHashEnforcesDimCaps(t *testing.T) {
	if _, err := DecodeBlurHash(refHash, BlurHashRenderConfig{Width: MaxBlurHashRenderSize + 1}); err == nil {
		t.Fatal("expected error for oversized width")
	}
	if _, err := DecodeBlurHash(refHash, BlurHashRenderConfig{Height: MaxBlurHashRenderSize + 1}); err == nil {
		t.Fatal("expected error for oversized height")
	}
	if _, err := DecodeBlurHash(refHash, BlurHashRenderConfig{Width: -1}); err == nil {
		t.Fatal("expected error for negative width")
	}
}

func TestDecodeBlurHashDefaultsDims(t *testing.T) {
	img, err := DecodeBlurHash(refHash, BlurHashRenderConfig{})
	if err != nil {
		t.Fatalf("DecodeBlurHash: %v", err)
	}
	b := img.img.Bounds()
	if b.Dx() != DefaultBlurHashRenderSize || b.Dy() != DefaultBlurHashRenderSize {
		t.Fatalf("dims = %dx%d, want %d square", b.Dx(), b.Dy(), DefaultBlurHashRenderSize)
	}
}

// The decoded value must stay chainable with the rest of the pipeline —
// that is the whole reason it returns *Image rather than raw pixels.
func TestDecodeBlurHashComposesWithEncoders(t *testing.T) {
	img, err := DecodeBlurHash(refHash, BlurHashRenderConfig{Width: 16, Height: 16})
	if err != nil {
		t.Fatalf("DecodeBlurHash: %v", err)
	}
	if _, err := img.JPEG(JPEGOptions{Quality: 50}).Bytes(); err != nil {
		t.Fatalf("JPEG: %v", err)
	}
	if _, err := img.PNG().Bytes(); err != nil {
		t.Fatalf("PNG: %v", err)
	}
}

func assertNear(t *testing.T, got, want, tol int, label string) {
	t.Helper()
	d := got - want
	if d < 0 {
		d = -d
	}
	if d > tol {
		t.Errorf("%s = %d, want %d±%d", label, got, want, tol)
	}
}
