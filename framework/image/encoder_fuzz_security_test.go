package image

import (
	"bytes"
	"encoding/binary"
	stdimage "image"
	"image/png"
	"math"
	"math/rand"
	"testing"

	"golang.org/x/image/tiff"
)

// fuzzRandBytes returns deterministic pseudo-random bytes from a fixed
// source, so seed corpus entries are stable across runs.
func fuzzRandBytes(n int) []byte {
	rng := rand.New(rand.NewSource(0xC0FFEE))
	b := make([]byte, n)
	rng.Read(b)
	return b
}

// fuzzByteAt cycles b modulo its length; empty b reads as zero.
func fuzzByteAt(b []byte, i int) byte {
	if len(b) == 0 {
		return 0
	}
	return b[i%len(b)]
}

// FuzzEncodePipeline pins the encoder-side robustness contract: the
// six terminals (JPEG/PNG/GIF/BMP/TIFF/WebP) consume pixel data that
// decode already bounded (DecodeConfig-first, 64 MP cap upstream), so
// arbitrary in-cap pixel content, degenerate-but-legal geometry, and
// any caller option values must never panic or index out of bounds.
// Each terminal either returns an error or produces bytes that decode
// back to the source dimensions.
//
// This is the encode-side closer for the surface pass 1 deferred
// (vp8l huffman/lz77/predictor, geometry, modulate, webp_encode,
// Encoder internals). Attacker byte streams on the decode side are
// covered by fuzz_test.go and internal/vp8l/fuzz_test.go.
//
// Run: go test ./framework/image/ -run '^$' -fuzz FuzzEncodePipeline -fuzztime 30s
func FuzzEncodePipeline(f *testing.F) {
	// Case shapes: tiny, gradient, fully transparent, noise, thin
	// extremes within the cap, the 16384-side vp8l limit, one past it,
	// offset bounds.Min, and empty bounds.
	f.Add(uint8(1), uint8(1), uint8(0), uint8(0), uint8(0), []byte{0, 0, 0, 255})
	f.Add(uint8(2), uint8(2), uint8(0), uint8(0), uint8(0), []byte{
		255, 0, 0, 255, 0, 255, 0, 255,
		0, 0, 255, 255, 255, 255, 255, 128,
	})
	f.Add(uint8(16), uint8(16), uint8(1), uint8(0), uint8(0), []byte(nil))
	f.Add(uint8(8), uint8(8), uint8(2), uint8(0), uint8(0), bytes.Repeat([]byte{0xAB, 0xCD, 0xEF, 0}, 64))
	f.Add(uint8(255), uint8(255), uint8(3), uint8(0), uint8(0xFF), fuzzRandBytes(1600*4))
	f.Add(uint8(0), uint8(0), uint8(0), uint8(0x04), uint8(0), []byte(nil))      // 1x512 column
	f.Add(uint8(0), uint8(0), uint8(0), uint8(0x08), uint8(0), []byte(nil))      // 512x1 row
	f.Add(uint8(0), uint8(0), uint8(0), uint8(0x40), uint8(0), []byte(nil))      // 16384x1: vp8l side limit
	f.Add(uint8(0), uint8(0), uint8(0), uint8(0x80), uint8(0), []byte(nil))      // 16385x1: one past it
	f.Add(uint8(3), uint8(3), uint8(0), uint8(0x10), uint8(0), make([]byte, 36)) // offset bounds.Min
	f.Add(uint8(0), uint8(0), uint8(0), uint8(0x01), uint8(0), []byte(nil))      // empty bounds

	f.Fuzz(func(t *testing.T, wb, hb, flags, shape, opts byte, pixels []byte) {
		w := int(wb)%40 + 1
		h := int(hb)%40 + 1
		switch {
		case shape&0x01 != 0:
			w = 0
		case shape&0x02 != 0:
			h = 0
		case shape&0x04 != 0:
			w, h = 1, 512+int(hb)*8 // tall thin: row-boundary stress, cheap area
		case shape&0x08 != 0:
			w, h = 512+int(wb)*8, 1 // wide thin
		case shape&0x40 != 0:
			w, h = 16384, 1 // exact vp8l side limit
		case shape&0x80 != 0:
			w, h = 16385, 1 // past vp8l limit, within decode cap
		}
		mx, my := 0, 0
		if shape&0x10 != 0 {
			mx, my = 1+int(opts)%6, 1+int(flags)%6 // non-zero bounds.Min
		}

		// Synthesize pixel content. Modes: raw cycled bytes,
		// coordinate gradient, fully transparent, XOR noise.
		pix := make([]byte, w*h*4)
		mode := flags & 3
		for y := 0; y < h; y++ {
			for x := 0; x < w; x++ {
				o := (y*w + x) * 4
				var c [4]uint8
				switch mode {
				case 1:
					c = [4]uint8{uint8(x * 16), uint8(y * 16), uint8(x ^ y*8), 255}
				case 2:
					c = [4]uint8{fuzzByteAt(pixels, o), fuzzByteAt(pixels, o+1), fuzzByteAt(pixels, o+2), 0}
				case 3:
					c = [4]uint8{
						fuzzByteAt(pixels, o) ^ uint8(x*7+y*13),
						fuzzByteAt(pixels, o+1) ^ uint8(x*5+y*11),
						fuzzByteAt(pixels, o+2) ^ uint8(x*3+y*17),
						fuzzByteAt(pixels, o+3) ^ uint8(x+y*23),
					}
				default:
					c = [4]uint8{fuzzByteAt(pixels, o), fuzzByteAt(pixels, o+1), fuzzByteAt(pixels, o+2), fuzzByteAt(pixels, o+3)}
				}
				copy(pix[o:o+4], c[:])
			}
		}

		rect := stdimage.Rect(mx, my, mx+w, my+h)
		var src stdimage.Image
		if flags&0x04 != 0 {
			// Premultiplied source; bytes are copied verbatim even
			// when they violate premultiplication invariants — the
			// encoders must cope with anything in the pixel array.
			rgba := stdimage.NewRGBA(rect)
			copy(rgba.Pix, pix)
			src = rgba
		} else {
			nrgba := stdimage.NewNRGBA(rect)
			copy(nrgba.Pix, pix)
			src = nrgba
		}
		img := FromImage(src, FormatPNG)

		// Transform prelude: geometry + modulate feed the encoders.
		// Float bits from the corpus reach NaN/±Inf/huge, exercising
		// Modulate's documented clamp-and-ignore behavior.
		if flags&0x08 != 0 {
			var br, sa float64 = 0, 1
			if len(pixels) >= 8 {
				br = math.Float64frombits(binary.LittleEndian.Uint64(pixels[:8]))
			}
			if len(pixels) >= 16 {
				sa = math.Float64frombits(binary.LittleEndian.Uint64(pixels[8:16]))
			}
			img = img.Modulate(Modulation{Brightness: Float64(br), Saturation: Float64(sa)})
		}
		if flags&0x10 != 0 {
			img = img.Resize(1+int(opts)%36, 1+int(shape)%26)
		}
		if flags&0x20 != 0 {
			img = img.Rotate(int(opts) * 90)
		}
		if flags&0x40 != 0 {
			img = img.Flip().Flop()
		}

		want := img.Bounds()
		terminals := []struct {
			name  string
			build func() *Encoder
		}{
			{"jpeg", func() *Encoder { return img.JPEG(JPEGOptions{Quality: int(opts)%121 - 10}) }},
			{"png", func() *Encoder {
				lvl := png.CompressionLevel(int(opts)%4 - 3)
				if opts&0x80 != 0 {
					lvl = 9 // out of [-3,0]: construction-time error path
				}
				return img.PNG(PNGOptions{Compression: lvl})
			}},
			{"gif", func() *Encoder { return img.GIF(GIFOptions{NumColors: int(opts) % 300}) }},
			{"bmp", func() *Encoder { return img.BMP() }},
			{"tiff", func() *Encoder {
				return img.TIFF(TIFFOptions{Compression: tiff.CompressionType(int(opts) % 4), Predictor: opts&1 != 0})
			}},
			{"webp", func() *Encoder { return img.WebP(WebPOptions{Lossy: opts&0x40 != 0}) }},
		}
		for _, tc := range terminals {
			out, err := tc.build().Bytes()
			if err != nil {
				continue // error is a valid outcome (unsupported mode, empty vp8l, ...)
			}
			if want.Dx() == 0 || want.Dy() == 0 {
				// Empty bounds only enter via a buggy FromImage caller;
				// stdlib terminals emit stub bytes there. The contract
				// under fuzz is no-panic, not decodability.
				continue
			}
			dec, derr := DecodeBytes(out)
			if derr != nil {
				t.Errorf("%s: encoded %d bytes but output does not decode: %v", tc.name, len(out), derr)
				continue
			}
			if got := dec.Bounds(); got.Dx() != want.Dx() || got.Dy() != want.Dy() {
				t.Errorf("%s: decoded %dx%d, source bounds %dx%d", tc.name, got.Dx(), got.Dy(), want.Dx(), want.Dy())
			}
		}
	})
}
