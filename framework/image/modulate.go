package image

import (
	stdimage "image"
	"image/color"
	"math"
)

// Modulation tweaks brightness and saturation in one pass over the
// image. Fields are pointers so the zero value Modulation{} means
// "no change" and a literal zero (e.g. grayscale via Saturation=0)
// is unambiguous. Use the Float64 helper to construct values:
//
//	img.Modulate(image.Modulation{Saturation: image.Float64(0)}) // grayscale
//	img.Modulate(image.Modulation{Brightness: image.Float64(1.4)})
type Modulation struct {
	// Brightness multiplies each colour channel. 1.0 is identity;
	// values above 1 brighten; values below 1 darken; 0 produces
	// pure black. Channels are clamped to 0..255. nil = unchanged.
	Brightness *float64

	// Saturation interpolates between grayscale (0.0) and source
	// (1.0). Values above 1 over-saturate. nil = unchanged.
	Saturation *float64
}

// Float64 returns a pointer to v. Convenience for Modulation literals.
func Float64(v float64) *float64 { return &v }

// Modulate returns a new *Image with brightness and saturation applied.
// A Modulation with both fields nil leaves the source unchanged.
//
// NaN values are treated as nil (no change for that channel) — they
// usually indicate a config-parsing bug (e.g., int(NaN)=0 or a JSON
// null surfacing as NaN). +Inf/-Inf clamp via the channel limits as
// you'd expect: +Inf brightness → 255, -Inf → 0.
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
