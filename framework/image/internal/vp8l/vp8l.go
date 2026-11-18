// Package vp8l is the pure-Go VP8L (lossless WebP) encoder behind
// framework/image's WebP output.
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

var candidatePredictorModes = []int{1, 2, 11, 12, 13}

var lastEncodePasses atomic.Int32

// Encode writes m to w as a lossless VP8L WebP. It errors on an empty
// image or a side over 16384.
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

func isUniform(m image.Image, w, h int, bounds image.Rectangle) bool {
	if w == 0 || h == 0 {
		return true
	}
	switch src := m.(type) {
	case *image.NRGBA:
		return uniformPix(src.Pix, src.PixOffset, w, h, bounds)
	case *image.RGBA:
		return uniformPix(src.Pix, src.PixOffset, w, h, bounds)
	}
	return false
}

func uniformPix(pix []uint8, pixOffset func(x, y int) int, w, h int, bounds image.Rectangle) bool {
	off0 := pixOffset(bounds.Min.X, bounds.Min.Y)
	r0, g0, b0, a0 := pix[off0], pix[off0+1], pix[off0+2], pix[off0+3]
	for y := range h {
		off := pixOffset(bounds.Min.X, bounds.Min.Y+y)
		for x := range w {
			i := off + x*4
			if pix[i] != r0 || pix[i+1] != g0 || pix[i+2] != b0 || pix[i+3] != a0 {
				return false
			}
		}
	}
	return true
}

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

func sampleRGBA8(m image.Image, x, y int) (r, g, b, a uint8) {
	if nrgba, ok := m.(*image.NRGBA); ok {
		off := nrgba.PixOffset(x, y)
		p := nrgba.Pix[off : off+4 : off+4]
		return p[0], p[1], p[2], p[3]
	}
	nc := color.NRGBAModel.Convert(m.At(x, y)).(color.NRGBA)
	return nc.R, nc.G, nc.B, nc.A
}

const (
	transformPredictor     = 0
	transformCrossColor    = 1
	transformSubtractGreen = 2
	transformColorIndex    = 3
)

const (
	nLiteralCodes        = 256
	nLengthCodes         = 24
	nDistanceCodes       = 40
	colorCacheMultiplier = 0x1e35a7bd
	cacheBitsPhaseC      = 8 // 256-entry cache; sweet spot for typical inputs
)

func packARGB(r, g, b, a uint8) uint32 {
	return uint32(a)<<24 | uint32(r)<<16 | uint32(g)<<8 | uint32(b)
}

func cacheHash(argb uint32, cacheBits uint) uint32 {
	return (argb * colorCacheMultiplier) >> (32 - cacheBits)
}

const predictorBits = 3

func emitImage(bw *bitWriter, m image.Image, w, h int, bounds image.Rectangle, mode int) {
	pixels := make([][4]uint8, 0, w*h)
	for y := range h {
		for x := range w {
			r, g, b, a := sampleRGBA8(m, bounds.Min.X+x, bounds.Min.Y+y)
			pixels = append(pixels, [4]uint8{g, r, b, a})
		}
	}

	modes := chooseBlockModes(pixels, w, h, predictorBits, mode)
	bw.writeBits(1, 1)                       // transform-present
	bw.writeBits(transformPredictor, 2)      // transform type
	bw.writeBits(uint32(predictorBits)-2, 3) // 3-bit field; decoder adds 2
	subW := (w + (1 << predictorBits) - 1) >> predictorBits
	subH := (h + (1 << predictorBits) - 1) >> predictorBits
	subPixels := buildPredictorSubImageFromModes(subW, subH, modes)
	applyPredictorAdaptive(pixels, w, h, predictorBits, modes, subW)
	emitPayload(bw, subPixels, subW, subH, false)

	bw.writeBits(1, 1)                      // transform-present
	bw.writeBits(transformSubtractGreen, 2) // transform type
	applySubtractGreen(pixels)

	bw.writeBits(0, 1) // no more transforms

	emitPayload(bw, pixels, w, h, true)
}

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

func buildPredictorSubImageFromModes(subW, subH int, modes []int) [][4]uint8 {
	out := make([][4]uint8, subW*subH)
	for i, m := range modes {
		out[i] = [4]uint8{uint8(m), 0, 0, 0}
	}
	return out
}

func applyPredictorAdaptive(pixels [][4]uint8, w, h, bits int, modes []int, subW int) {
	src := make([][4]uint8, len(pixels))
	copy(src, pixels)

	pixels[0][0] = src[0][0]
	pixels[0][1] = src[0][1]
	pixels[0][2] = src[0][2]
	pixels[0][3] = src[0][3] - 0xFF

	for x := 1; x < w; x++ {
		predictorResidual(&pixels[x], src[x], src[x-1])
	}

	for y := 1; y < h; y++ {
		rowOff := y * w
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
					TR = src[y*w]
				}
				TL := src[off-w-1]
				pred = predictMode(mode, L, T, TR, TL)
			}
			predictorResidual(&pixels[off], src[off], pred)
		}
	}
}

func predictorResidual(dst *[4]uint8, src, pred [4]uint8) {
	dst[0] = src[0] - pred[0]
	dst[1] = src[1] - pred[1]
	dst[2] = src[2] - pred[2]
	dst[3] = src[3] - pred[3]
}

func applySubtractGreen(pixels [][4]uint8) {
	for i := range pixels {
		g := pixels[i][0]
		pixels[i][1] -= g
		pixels[i][2] -= g
	}
}
