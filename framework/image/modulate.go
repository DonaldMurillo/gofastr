package image

import (
	stdimage "image"
	"image/color"
	"math"
)

// Modulation sets brightness and saturation; a nil field is unchanged.
type Modulation struct {
	// Brightness multiplies each channel; 1 is identity.
	Brightness *float64

	// Saturation runs from grayscale (0) through the source (1).
	Saturation *float64
}

// Float64 returns a pointer to v.
func Float64(v float64) *float64 { return &v }

// Modulate applies m; a nil or NaN field leaves its channel unchanged.
func (i *Image) Modulate(m Modulation) *Image {
	if (m.Brightness == nil || math.IsNaN(*m.Brightness)) &&
		(m.Saturation == nil || math.IsNaN(*m.Saturation)) {
		return i
	}
	b, s := 1.0, 1.0
	if m.Brightness != nil && !math.IsNaN(*m.Brightness) {
		b = *m.Brightness
	}
	if m.Saturation != nil && !math.IsNaN(*m.Saturation) {
		s = *m.Saturation
	}
	if b == 1 && s == 1 {
		return i
	}

	sb := i.img.Bounds()
	w, h := sb.Dx(), sb.Dy()
	dst := stdimage.NewRGBA(stdimage.Rect(0, 0, w, h))

	switch src := i.img.(type) {
	case *stdimage.NRGBA:
		modulateNRGBA(dst, src, sb, b, s)
		return i.derive(dst)
	case *stdimage.RGBA:
		modulateRGBA(dst, src, sb, b, s)
		return i.derive(dst)
	case *stdimage.YCbCr:
		modulateYCbCr(dst, src, sb, b, s)
		return i.derive(dst)
	}

	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			r16, g16, b16, a16 := i.img.At(sb.Min.X+x, sb.Min.Y+y).RGBA()
			r, g, bl, a := unpremul16(r16, g16, b16, a16)
			r, g, bl = applyModulation(r, g, bl, b, s)
			dst.SetRGBA(x, y, premulRGBA(r, g, bl, a))
		}
	}
	return i.derive(dst)
}

func modulateNRGBA(dst *stdimage.RGBA, src *stdimage.NRGBA, sb stdimage.Rectangle, brightness, saturation float64) {
	w, h := sb.Dx(), sb.Dy()
	for y := 0; y < h; y++ {
		sRow := src.PixOffset(sb.Min.X, sb.Min.Y+y)
		dRow := dst.PixOffset(0, y)
		for x := 0; x < w; x++ {
			si := sRow + x*4
			r, g, bl, a := src.Pix[si], src.Pix[si+1], src.Pix[si+2], src.Pix[si+3]
			r, g, bl = applyModulation(r, g, bl, brightness, saturation)
			di := dRow + x*4
			if a == 0xFF {
				dst.Pix[di+0] = r
				dst.Pix[di+1] = g
				dst.Pix[di+2] = bl
				dst.Pix[di+3] = a
			} else {
				dst.Pix[di+0] = uint8(uint32(r) * uint32(a) / 255)
				dst.Pix[di+1] = uint8(uint32(g) * uint32(a) / 255)
				dst.Pix[di+2] = uint8(uint32(bl) * uint32(a) / 255)
				dst.Pix[di+3] = a
			}
		}
	}
}

func modulateYCbCr(dst *stdimage.RGBA, src *stdimage.YCbCr, sb stdimage.Rectangle, brightness, saturation float64) {
	w, h := sb.Dx(), sb.Dy()
	for y := 0; y < h; y++ {
		dRow := dst.PixOffset(0, y)
		for x := 0; x < w; x++ {
			yi := src.YOffset(sb.Min.X+x, sb.Min.Y+y)
			ci := src.COffset(sb.Min.X+x, sb.Min.Y+y)
			r, g, bl := color.YCbCrToRGB(src.Y[yi], src.Cb[ci], src.Cr[ci])
			r, g, bl = applyModulation(r, g, bl, brightness, saturation)
			di := dRow + x*4
			dst.Pix[di+0] = r
			dst.Pix[di+1] = g
			dst.Pix[di+2] = bl
			dst.Pix[di+3] = 0xFF
		}
	}
}

func modulateRGBA(dst *stdimage.RGBA, src *stdimage.RGBA, sb stdimage.Rectangle, brightness, saturation float64) {
	w, h := sb.Dx(), sb.Dy()
	for y := 0; y < h; y++ {
		sRow := src.PixOffset(sb.Min.X, sb.Min.Y+y)
		dRow := dst.PixOffset(0, y)
		for x := 0; x < w; x++ {
			si := sRow + x*4
			r, g, bl, a := src.Pix[si], src.Pix[si+1], src.Pix[si+2], src.Pix[si+3]
			if a > 0 && a < 0xFF {
				r = uint8(uint32(r) * 0xFF / uint32(a))
				g = uint8(uint32(g) * 0xFF / uint32(a))
				bl = uint8(uint32(bl) * 0xFF / uint32(a))
			}
			r, g, bl = applyModulation(r, g, bl, brightness, saturation)
			di := dRow + x*4
			if a == 0xFF {
				dst.Pix[di+0] = r
				dst.Pix[di+1] = g
				dst.Pix[di+2] = bl
				dst.Pix[di+3] = a
			} else if a == 0 {
				dst.Pix[di+0] = 0
				dst.Pix[di+1] = 0
				dst.Pix[di+2] = 0
				dst.Pix[di+3] = 0
			} else {
				dst.Pix[di+0] = uint8(uint32(r) * uint32(a) / 255)
				dst.Pix[di+1] = uint8(uint32(g) * uint32(a) / 255)
				dst.Pix[di+2] = uint8(uint32(bl) * uint32(a) / 255)
				dst.Pix[di+3] = a
			}
		}
	}
}

func applyModulation(r, g, b uint8, brightness, saturation float64) (uint8, uint8, uint8) {
	fr := float64(r) * brightness
	fg := float64(g) * brightness
	fb := float64(b) * brightness
	if saturation != 1 {
		gray := 0.299*fr + 0.587*fg + 0.114*fb
		fr = gray + (fr-gray)*saturation
		fg = gray + (fg-gray)*saturation
		fb = gray + (fb-gray)*saturation
	}
	return clamp8(fr), clamp8(fg), clamp8(fb)
}

func clamp8(v float64) uint8 {
	if math.IsNaN(v) {
		return 0
	}
	switch {
	case v < 0:
		return 0
	case v > 255:
		return 255
	}
	return uint8(v)
}

func unpremul16(r16, g16, b16, a16 uint32) (uint8, uint8, uint8, uint8) {
	if a16 == 0 {
		return 0, 0, 0, 0
	}
	r := uint8(uint64(r16) * 0xff / uint64(a16))
	g := uint8(uint64(g16) * 0xff / uint64(a16))
	b := uint8(uint64(b16) * 0xff / uint64(a16))
	return r, g, b, uint8(a16 >> 8)
}

func premulRGBA(r, g, b, a uint8) color.RGBA {
	if a < 255 {
		r = uint8(uint32(r) * uint32(a) / 255)
		g = uint8(uint32(g) * uint32(a) / 255)
		b = uint8(uint32(b) * uint32(a) / 255)
	}
	return color.RGBA{R: r, G: g, B: b, A: a}
}
