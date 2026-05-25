// Package vp8l implements a pure-Go VP8L (WebP lossless) encoder.
//
// The bitstream is decoded by golang.org/x/image/webp; this package
// is the matching encoder. Coverage today is Phase A — literal-only
// emission with canonical Huffman codes per channel and no transforms.
// Phase B (subtract-green) and Phase C (LZ77 + color cache) extend
// the encoder without changing this public API.
//
// This package is internal because the only intended entry point is
// framework/image.Image.WebP(WebPOptions{Lossless: true}).
package vp8l

import (
	"encoding/binary"
	"errors"
	"fmt"
	"image"
	"image/color"
	"io"
)

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
	// VP8L width and height are 14-bit unsigned, range 1..16384.
	if width > 16384 || height > 16384 {
		return fmt.Errorf("vp8l: image too large: %dx%d (max 16384x16384)", width, height)
	}

	bw := &bitWriter{}
	emitImage(bw, m, width, height, bounds)
	payload := bw.Bytes()

	// VP8L chunk header: 'VP8L', 4-byte LE chunk size, 1-byte signature,
	// 4 bytes for {W-1, H-1, alphaUsed, version}, then payload bits.
	const sigByte byte = 0x2F
	chunkBody := make([]byte, 0, 5+len(payload))
	chunkBody = append(chunkBody, sigByte)
	hdr := uint32(width-1) | uint32(height-1)<<14 | 1<<28 // alphaUsed=1, version=0
	var hdrBuf [4]byte
	binary.LittleEndian.PutUint32(hdrBuf[:], hdr)
	chunkBody = append(chunkBody, hdrBuf[:]...)
	chunkBody = append(chunkBody, payload...)

	// RIFF padding: chunks are padded to even sizes.
	pad := 0
	if len(chunkBody)%2 == 1 {
		pad = 1
	}

	totalRIFF := 4 + 4 + len(chunkBody) + pad // "WEBP" + "VP8L" + size + body
	_ = totalRIFF
	// "WEBP" form, then VP8L chunk inside.
	if _, err := w.Write([]byte("RIFF")); err != nil {
		return err
	}
	var sizeBuf [4]byte
	binary.LittleEndian.PutUint32(sizeBuf[:], uint32(4+8+len(chunkBody)+pad))
	if _, err := w.Write(sizeBuf[:]); err != nil {
		return err
	}
	if _, err := w.Write([]byte("WEBP")); err != nil {
		return err
	}
	if _, err := w.Write([]byte("VP8L")); err != nil {
		return err
	}
	binary.LittleEndian.PutUint32(sizeBuf[:], uint32(len(chunkBody)))
	if _, err := w.Write(sizeBuf[:]); err != nil {
		return err
	}
	if _, err := w.Write(chunkBody); err != nil {
		return err
	}
	if pad == 1 {
		if _, err := w.Write([]byte{0}); err != nil {
			return err
		}
	}
	return nil
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

// emitImage writes the VP8L payload (everything after the 5-byte
// "signature + dimensions" prefix) into bw.
func emitImage(bw *bitWriter, m image.Image, w, h int, bounds image.Rectangle) {
	// transform-present = 0
	bw.writeBits(0, 1)

	// Then: top-level Huffman codes preceded by:
	//   color-cache-bit (1 bit)
	//   meta-prefix-present (1 bit)
	bw.writeBits(0, 1) // colorCacheBit = 0 (no color cache in Phase A)
	bw.writeBits(0, 1) // metaPrefix = 0

	// Collect frequencies in one pass and emit codes; we keep the
	// pixel data in a flat slice to avoid re-fetching colours.
	pixels := make([][4]uint8, 0, w*h) // {G, R, B, A}
	gFreq := make([]int, 256+24)        // G alphabet: literals + length codes (Phase A leaves length codes at 0)
	rFreq := make([]int, 256)
	bFreq := make([]int, 256)
	aFreq := make([]int, 256)

	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			r, g, b, a := sampleRGBA8(m, bounds.Min.X+x, bounds.Min.Y+y)
			pixels = append(pixels, [4]uint8{g, r, b, a})
			gFreq[g]++
			rFreq[r]++
			bFreq[b]++
			aFreq[a]++
		}
	}

	gCodes, gLens := writeHuffmanTree(bw, codeLengths(gFreq))
	rCodes, rLens := writeHuffmanTree(bw, codeLengths(rFreq))
	bCodes, bLens := writeHuffmanTree(bw, codeLengths(bFreq))
	aCodes, aLens := writeHuffmanTree(bw, codeLengths(aFreq))
	_, _ = writeHuffmanTree(bw, make([]int, 40)) // distance alphabet — unused in Phase A

	for _, p := range pixels {
		g, r, bch, a := p[0], p[1], p[2], p[3]
		bw.writeBits(gCodes[g], uint(gLens[g]))
		bw.writeBits(rCodes[r], uint(rLens[r]))
		bw.writeBits(bCodes[bch], uint(bLens[bch]))
		bw.writeBits(aCodes[a], uint(aLens[a]))
	}
}
