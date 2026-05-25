// Package vp8l implements a pure-Go VP8L (WebP lossless) encoder.
//
// The bitstream is decoded by golang.org/x/image/webp; this package
// is the matching encoder. Coverage today is Phase A — literal-only
// emission with canonical Huffman codes per channel and no transforms.
// Phase B (subtract-green) and Phase C (LZ77 + color cache) extend
// the encoder without changing this public API.
//
// This package is internal because the only intended entry point is
// framework/image.Image.WebP() (zero-value WebPOptions = lossless).
package vp8l

import (
	"encoding/binary"
	"errors"
	"fmt"
	"image"
	"image/color"
	"io"
	"sync/atomic"
)

// Encode writes m as a VP8L WebP to w. Only m's bounds are honoured;
// callers needing other-than-RGBA must convert first (which the
// framework/image package does automatically).
//
// Lossless guarantee: the decoded pixels equal the input pixels
// exactly when m's bounds are non-empty. An empty image returns an
// error rather than producing an empty WebP.
// candidatePredictorModes is the set of uniform predictor modes the
// encoder tries on every input. We pick the smallest output across
// the set — a cheap "try N strategies, ship the best" approach that
// avoids the bad-cost-metric tax of greedy per-block selection.
//
//	mode 1  (L)            — libwebp baseline; great for column-wise gradients
//	mode 2  (T)            — row-wise gradients
//	mode 11 (Select)       — picks between L and T per pixel
//	mode 12 (ClampAdd-Sub) — best for diagonal/smooth gradients
//	mode 13 (ClampAdd-Half)— variant that wins on photographic content
//
// Together this set covers most real-world inputs. Encoding cost is
// 5× a single pass — `Encode` short-circuits to 1 pass when a cheap
// uniformity probe confirms every mode would produce identical output.
var candidatePredictorModes = []int{1, 2, 11, 12, 13}

// lastEncodePasses records how many candidate-mode passes the most
// recent Encode actually ran. Used only for tests; atomic so the
// race detector doesn't fire when concurrent encodes update it.
// Not part of the public API.
var lastEncodePasses atomic.Int32

// Encode writes m as a VP8L WebP to w. Only m's bounds are honoured;
// callers needing other-than-RGBA must convert first (which the
// framework/image package does automatically).
//
// Lossless guarantee: the decoded pixels equal the input pixels
// exactly when m's bounds are non-empty. An empty image returns an
// error rather than producing an empty WebP.
func Encode(w io.Writer, m image.Image) error {
	bounds := m.Bounds()
	width := bounds.Dx()
	height := bounds.Dy()
	if width <= 0 || height <= 0 {
		return errors.New("vp8l: empty image")
	}
	if width > 16384 || height > 16384 {
		return fmt.Errorf("vp8l: image too large: %dx%d (max 16384x16384)", width, height)
	}

	// Short-circuit on uniform input: when every pixel is the same
	// argb, the predictor produces all-zero residuals regardless of
	// mode and all 5 passes converge to the same output. The probe
	// is O(w*h) but each pixel comparison is one int64, far cheaper
	// than a single encode pass.
	modes := candidatePredictorModes
	if isUniform(m, width, height, bounds) {
		modes = modes[:1]
	}

	var best []byte
	for _, mode := range modes {
		buf, err := encodeWithMode(m, width, height, bounds, mode)
		if err != nil {
			return err
		}
		if best == nil || len(buf) < len(best) {
			best = buf
		}
	}
	lastEncodePasses.Store(int32(len(modes)))
	_, err := w.Write(best)
	return err
}

// isUniform reports whether every pixel in m's bounds has the same
// 32-bit ARGB value. Used by Encode to short-circuit the multi-pass
// candidate sweep — no predictor mode produces a smaller stream for
// a uniform image, so running 5 of them is pure waste.
//
// Fast-path only the two concrete types the framework hands us in
// practice (`*image.NRGBA` and `*image.RGBA`). For everything else
// we return false up front rather than pay 16M interface-conversion
// allocs on a 4K image; we'll just run all 5 modes and accept the
// (rare) wasted work. The image package converts user images to
// NRGBA before reaching vp8l.Encode, so this loses no real coverage.
func isUniform(m image.Image, w, h int, bounds image.Rectangle) bool {
	if w == 0 || h == 0 {
		return true
	}
	switch src := m.(type) {
	case *image.NRGBA:
		return nrgbaUniform(src, w, h, bounds)
	case *image.RGBA:
		return rgbaUniform(src, w, h, bounds)
	}
	return false
}

func nrgbaUniform(m *image.NRGBA, w, h int, bounds image.Rectangle) bool {
	off0 := m.PixOffset(bounds.Min.X, bounds.Min.Y)
	r0, g0, b0, a0 := m.Pix[off0], m.Pix[off0+1], m.Pix[off0+2], m.Pix[off0+3]
	for y := 0; y < h; y++ {
		off := m.PixOffset(bounds.Min.X, bounds.Min.Y+y)
		for x := 0; x < w; x++ {
			i := off + x*4
			if m.Pix[i] != r0 || m.Pix[i+1] != g0 || m.Pix[i+2] != b0 || m.Pix[i+3] != a0 {
				return false
			}
		}
	}
	return true
}

func rgbaUniform(m *image.RGBA, w, h int, bounds image.Rectangle) bool {
	off0 := m.PixOffset(bounds.Min.X, bounds.Min.Y)
	r0, g0, b0, a0 := m.Pix[off0], m.Pix[off0+1], m.Pix[off0+2], m.Pix[off0+3]
	for y := 0; y < h; y++ {
		off := m.PixOffset(bounds.Min.X, bounds.Min.Y+y)
		for x := 0; x < w; x++ {
			i := off + x*4
			if m.Pix[i] != r0 || m.Pix[i+1] != g0 || m.Pix[i+2] != b0 || m.Pix[i+3] != a0 {
				return false
			}
		}
	}
	return true
}

// encodeWithMode produces the full RIFF + WEBP + VP8L byte stream
// using the given uniform predictor mode. Called once per candidate
// in candidatePredictorModes.
func encodeWithMode(m image.Image, width, height int, bounds image.Rectangle, mode int) ([]byte, error) {
	bw := &bitWriter{}
	emitImage(bw, m, width, height, bounds, mode)
	payload := bw.Bytes()

	const sigByte byte = 0x2F
	chunkBody := make([]byte, 0, 5+len(payload))
	chunkBody = append(chunkBody, sigByte)
	hdr := uint32(width-1) | uint32(height-1)<<14 | 1<<28 // alphaUsed=1, version=0
	var hdrBuf [4]byte
	binary.LittleEndian.PutUint32(hdrBuf[:], hdr)
	chunkBody = append(chunkBody, hdrBuf[:]...)
	chunkBody = append(chunkBody, payload...)

	pad := 0
	if len(chunkBody)%2 == 1 {
		pad = 1
	}

	out := make([]byte, 0, 12+len(chunkBody)+pad)
	out = append(out, []byte("RIFF")...)
	var sizeBuf [4]byte
	binary.LittleEndian.PutUint32(sizeBuf[:], uint32(4+8+len(chunkBody)+pad))
	out = append(out, sizeBuf[:]...)
	out = append(out, []byte("WEBP")...)
	out = append(out, []byte("VP8L")...)
	binary.LittleEndian.PutUint32(sizeBuf[:], uint32(len(chunkBody)))
	out = append(out, sizeBuf[:]...)
	out = append(out, chunkBody...)
	if pad == 1 {
		out = append(out, 0)
	}
	return out, nil
}

// sampleRGBA8 returns the 8-bit straight (non-premultiplied) RGBA
// components for the pixel at (x, y) in m. Sampling routes through
// color.NRGBAModel so premultiplied sources (image.RGBA) are correctly
// unpremultiplied on the way in. Output is interpreted byte-for-byte
// by the decoder, which produces image.NRGBA.
func sampleRGBA8(m image.Image, x, y int) (r, g, b, a uint8) {
	if nrgba, ok := m.(*image.NRGBA); ok {
		off := nrgba.PixOffset(x, y)
		p := nrgba.Pix[off : off+4 : off+4]
		return p[0], p[1], p[2], p[3]
	}
	nc := color.NRGBAModel.Convert(m.At(x, y)).(color.NRGBA)
	return nc.R, nc.G, nc.B, nc.A
}

// VP8L transform-type codes per spec § 3.
const (
	transformPredictor     = 0
	transformCrossColor    = 1
	transformSubtractGreen = 2
	transformColorIndex    = 3
)

// VP8L alphabet sizes & color-cache parameters.
const (
	nLiteralCodes        = 256
	nLengthCodes         = 24
	nDistanceCodes       = 40
	colorCacheMultiplier = 0x1e35a7bd
	cacheBitsPhaseC      = 8 // 256-entry cache; sweet spot for typical inputs
)

// packARGB packs (R,G,B,A) into the 32-bit value the color cache hashes
// against, per the VP8L spec — A in the high byte, then R, G, B.
func packARGB(r, g, b, a uint8) uint32 {
	return uint32(a)<<24 | uint32(r)<<16 | uint32(g)<<8 | uint32(b)
}

// cacheHash returns the cache slot for argb given the cacheBits parameter.
func cacheHash(argb uint32, cacheBits uint) uint32 {
	return (argb * colorCacheMultiplier) >> (32 - cacheBits)
}

// predictorBits is the spec's "bits" field for the predictor transform
// (after the +2 the decoder adds). Block size = 1<<predictorBits.
// Bits=3 → 8-pixel blocks: the libwebp default, fine enough that
// per-block mode selection catches local variation without exploding
// the sub-image. Paired with the switch-margin stickiness in
// chooseBlockModes so smooth content keeps a uniform sub-image.
const predictorBits = 3

// emitImage writes the top-level VP8L payload (everything after the
// 5-byte signature + dimensions prefix). Sub-image emission for the
// predictor transform goes through emitPayload directly. mode picks
// the uniform predictor mode used across every block of the sub-image.
func emitImage(bw *bitWriter, m image.Image, w, h int, bounds image.Rectangle, mode int) {
	pixels := make([][4]uint8, 0, w*h)
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			r, g, b, a := sampleRGBA8(m, bounds.Min.X+x, bounds.Min.Y+y)
			pixels = append(pixels, [4]uint8{g, r, b, a})
		}
	}

	// Phase E: predictor transform with adaptive per-block mode
	// selection. For each block, every mode 0..13 is evaluated and the
	// one with minimum L1 residual cost is chosen. Big win for smooth
	// content (gradients, photos) where local pixel correlation varies.
	// First row uses mode 1 (L), first column uses mode 2 (T), and
	// pixel (0,0) uses mode 0 (opaque black) — these are wired into
	// the decoder regardless of sub-image mode.
	modes := chooseBlockModes(pixels, w, h, predictorBits, mode)
	bw.writeBits(1, 1)                       // transform-present
	bw.writeBits(transformPredictor, 2)      // transform type
	bw.writeBits(uint32(predictorBits)-2, 3) // 3-bit field; decoder adds 2
	subW := (w + (1<<predictorBits) - 1) >> predictorBits
	subH := (h + (1<<predictorBits) - 1) >> predictorBits
	subPixels := buildPredictorSubImageFromModes(subW, subH, modes)
	applyPredictorAdaptive(pixels, w, h, predictorBits, modes, subW)
	emitPayload(bw, subPixels, subW, subH, false)

	// Phase B: subtract-green, applied AFTER predictor so the
	// residual-space green channel is decorrelated out of R and B.
	bw.writeBits(1, 1)                      // transform-present
	bw.writeBits(transformSubtractGreen, 2) // transform type
	applySubtractGreen(pixels)

	bw.writeBits(0, 1) // no more transforms

	emitPayload(bw, pixels, w, h, true)
}

// emitPayload writes the color-cache parameters, optional meta-prefix,
// five Huffman trees, and LZ77/cache/literal pixel stream for the
// given pixel buffer. Used for both the top-level image and predictor
// sub-images (the latter with topLevel=false → no meta-prefix bit).
func emitPayload(bw *bitWriter, pixels [][4]uint8, w, h int, topLevel bool) {
	const cacheBits = cacheBitsPhaseC

	bw.writeBits(1, 1)                 // useColorCache = 1
	bw.writeBits(uint32(cacheBits), 4) // ccBits
	if topLevel {
		bw.writeBits(0, 1) // metaPrefix = 0
	}

	stream, gFreq, rFreq, bFreq, aFreq, dFreq := buildStream(pixels, cacheBits)

	gCodes, gLens := writeHuffmanTree(bw, codeLengths(gFreq))
	rCodes, rLens := writeHuffmanTree(bw, codeLengths(rFreq))
	bCodes, bLens := writeHuffmanTree(bw, codeLengths(bFreq))
	aCodes, aLens := writeHuffmanTree(bw, codeLengths(aFreq))
	dCodes, dLens := writeHuffmanTree(bw, codeLengths(dFreq))

	for _, s := range stream {
		switch s.kind {
		case symLiteral:
			bw.writeBits(gCodes[s.g], uint(gLens[s.g]))
			bw.writeBits(rCodes[s.r], uint(rLens[s.r]))
			bw.writeBits(bCodes[s.b], uint(bLens[s.b]))
			bw.writeBits(aCodes[s.a], uint(aLens[s.a]))
		case symCacheHit:
			sym := nLiteralCodes + nLengthCodes + int(s.cacheIdx)
			bw.writeBits(gCodes[sym], uint(gLens[sym]))
		case symBackref:
			gSym := nLiteralCodes + int(s.lenSym)
			bw.writeBits(gCodes[gSym], uint(gLens[gSym]))
			if s.extraLenBits > 0 {
				bw.writeBits(s.extraLen, uint(s.extraLenBits))
			}
			bw.writeBits(dCodes[s.distSym], uint(dLens[s.distSym]))
			if s.extraDistBits > 0 {
				bw.writeBits(s.extraDist, uint(s.extraDistBits))
			}
		}
	}
	_, _ = w, h
}

// buildPredictorSubImageFromModes returns a subW×subH sub-image where
// each pixel's G channel holds the chosen predictor mode for that
// block. R, B, A are zero. The encoder's internal pixel layout is
// {G, R, B, A}.
func buildPredictorSubImageFromModes(subW, subH int, modes []int) [][4]uint8 {
	out := make([][4]uint8, subW*subH)
	for i, m := range modes {
		out[i] = [4]uint8{uint8(m), 0, 0, 0}
	}
	return out
}

// applyPredictorAdaptive computes residuals using the per-block mode
// from `modes`. Boundary pixels (x=0 or y=0) are hardcoded by the
// spec regardless of sub-image content. The encoder reads from a
// parallel copy so neighbours aren't observed in their residual form.
func applyPredictorAdaptive(pixels [][4]uint8, w, h, bits int, modes []int, subW int) {
	src := make([][4]uint8, len(pixels))
	copy(src, pixels)

	// (0, 0): mode 0 (opaque black) → pred = (0, 0, 0, 0xFF).
	pixels[0][0] = src[0][0]
	pixels[0][1] = src[0][1]
	pixels[0][2] = src[0][2]
	pixels[0][3] = src[0][3] - 0xFF

	// First row (y=0, x≥1): mode 1 (L).
	for x := 1; x < w; x++ {
		predictorResidual(&pixels[x], src[x], src[x-1])
	}

	for y := 1; y < h; y++ {
		rowOff := y * w
		// First column (x=0): mode 2 (T).
		predictorResidual(&pixels[rowOff], src[rowOff], src[rowOff-w])

		for x := 1; x < w; x++ {
			off := rowOff + x
			mode := modes[(y>>bits)*subW+(x>>bits)]
			var pred [4]uint8
			switch mode {
			case 0:
				pred = [4]uint8{0, 0, 0, 0xFF}
			case 1:
				pred = src[off-1]
			case 2:
				pred = src[off-w]
			default:
				L := src[off-1]
				T := src[off-w]
				var TR [4]uint8
				if x+1 < w {
					TR = src[off-w+1]
				} else {
					// Spec quirk: rightmost-column TR wraps to the
					// first pixel of the current row (decoder's
					// pix[top+4] advances past the end of the previous
					// row into the start of this one).
					TR = src[y*w]
				}
				TL := src[off-w-1]
				pred = predictMode(mode, L, T, TR, TL)
			}
			predictorResidual(&pixels[off], src[off], pred)
		}
	}
}

// predictorResidual sets dst = src - pred (mod 256, per byte).
func predictorResidual(dst *[4]uint8, src, pred [4]uint8) {
	dst[0] = src[0] - pred[0]
	dst[1] = src[1] - pred[1]
	dst[2] = src[2] - pred[2]
	dst[3] = src[3] - pred[3]
}

// applySubtractGreen replaces (G, R, B, A) with (G, R-G, B-G, A) in
// place. The decoder reverses this by re-adding G after channel decode.
// Subtracting modulo 256 (i.e., uint8 wraparound) is what the spec
// requires and matches the decoder's reverse operation.
func applySubtractGreen(pixels [][4]uint8) {
	for i := range pixels {
		g := pixels[i][0]
		pixels[i][1] -= g
		pixels[i][2] -= g
	}
}
