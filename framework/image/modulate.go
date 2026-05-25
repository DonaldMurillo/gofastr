package image

import (
	stdimage "image"
	"image/color"
)

// Modulation tweaks brightness and saturation in one pass over the image.
// The zero value is identity (no change). Set Brightness and/or Saturation
// to apply.
type Modulation struct {
	// Brightness multiplies each colour channel. 1.0 is identity; values
	// above 1 brighten; values below 1 darken. Channels are clamped to
	// the 0..255 output range.
	Brightness float64

	// Saturation interpolates between grayscale (0.0) and source (1.0).
	// Values above 1 over-saturate.
	Saturation float64
}

// Modulate returns a new *Image with brightness and saturation applied.
// A zero Modulation leaves the source unchanged.
func (i *Image) Modulate(m Modulation) *Image {
	b := m.Brightness
	s := m.Saturation
	if b == 0 {
		b = 1
	}
	if s == 0 {
		s = 1
	}
	if b == 1 && s == 1 {
		return i
	}

	sb := i.img.Bounds()
	dst := stdimage.NewRGBA(stdimage.Rect(0, 0, sb.Dx(), sb.Dy()))
	for y := 0; y < sb.Dy(); y++ {
		for x := 0; x < sb.Dx(); x++ {
			r16, g16, b16, a16 := i.img.At(sb.Min.X+x, sb.Min.Y+y).RGBA()
			r, g, bl, a := unpremul16(r16, g16, b16, a16)
			r, g, bl = applyModulation(r, g, bl, b, s)
			dst.SetRGBA(x, y, premulRGBA(r, g, bl, a))
		}
	}
	return i.derive(dst)
}

func applyModulation(r, g, b uint8, brightness, saturation float64) (uint8, uint8, uint8) {
	fr := float64(r) * brightness
	fg := float64(g) * brightness
	fb := float64(b) * brightness
	if saturation != 1 {
		// Rec. 601 luma weights — same coefficients sharp/Bun use.
		gray := 0.299*fr + 0.587*fg + 0.114*fb
		fr = gray + (fr-gray)*saturation
		fg = gray + (fg-gray)*saturation
		fb = gray + (fb-gray)*saturation
	}
	return clamp8(fr), clamp8(fg), clamp8(fb)
}

func clamp8(v float64) uint8 {
	switch {
	case v < 0:
		return 0
	case v > 255:
		return 255
	}
	return uint8(v)
}

// unpremul16 takes the 16-bit alpha-premultiplied result of color.RGBA()
// and returns straight 8-bit components for per-channel math.
func unpremul16(r16, g16, b16, a16 uint32) (uint8, uint8, uint8, uint8) {
	if a16 == 0 {
		return 0, 0, 0, 0
	}
	r := uint8(uint64(r16) * 0xff / uint64(a16))
	g := uint8(uint64(g16) * 0xff / uint64(a16))
	b := uint8(uint64(b16) * 0xff / uint64(a16))
	return r, g, b, uint8(a16 >> 8)
}

// premulRGBA returns an alpha-premultiplied color.RGBA from straight values.
func premulRGBA(r, g, b, a uint8) color.RGBA {
	if a < 255 {
		r = uint8(uint32(r) * uint32(a) / 255)
		g = uint8(uint32(g) * uint32(a) / 255)
		b = uint8(uint32(b) * uint32(a) / 255)
	}
	return color.RGBA{R: r, G: g, B: b, A: a}
}
