package image

import (
	"fmt"
	stdimage "image"
	"image/color"
)

// NewGradient returns a width×height image filled with a diagonal
// (top-left → bottom-right) linear gradient between two #RRGGBB stops.
// Useful as a generated placeholder for surfaces that need an image but
// have none yet — app icons (uihost.WithAppIcon), covers, OG images —
// without committing binary assets.
func NewGradient(width, height int, fromHex, toHex string) (*Image, error) {
	if width <= 0 || height <= 0 {
		return nil, fmt.Errorf("image: NewGradient dimensions must be positive, got %dx%d", width, height)
	}
	from, err := parseHexColor(fromHex)
	if err != nil {
		return nil, err
	}
	to, err := parseHexColor(toHex)
	if err != nil {
		return nil, err
	}
	img := stdimage.NewNRGBA(stdimage.Rect(0, 0, width, height))
	// t runs 0→1 along the main diagonal.
	den := float64(width-1) + float64(height-1)
	if den == 0 {
		den = 1
	}
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			t := (float64(x) + float64(y)) / den
			img.SetNRGBA(x, y, color.NRGBA{
				R: lerpByte(from.R, to.R, t),
				G: lerpByte(from.G, to.G, t),
				B: lerpByte(from.B, to.B, t),
				A: 255,
			})
		}
	}
	return FromImage(img, FormatPNG), nil
}

func lerpByte(a, b uint8, t float64) uint8 {
	return uint8(float64(a) + (float64(b)-float64(a))*t + 0.5)
}

// parseHexColor parses a strict #RRGGBB value.
func parseHexColor(s string) (color.NRGBA, error) {
	if len(s) != 7 || s[0] != '#' {
		return color.NRGBA{}, fmt.Errorf("image: color must be #RRGGBB, got %q", s)
	}
	var r, g, b uint8
	if _, err := fmt.Sscanf(s[1:], "%02x%02x%02x", &r, &g, &b); err != nil {
		return color.NRGBA{}, fmt.Errorf("image: color must be #RRGGBB, got %q", s)
	}
	return color.NRGBA{R: r, G: g, B: b, A: 255}, nil
}
