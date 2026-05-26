package image

import (
	"errors"
	"fmt"
	"math"
)

// base83Alphabet is the BlurHash character set, per the reference spec at
// https://blurha.sh.
const base83Alphabet = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz#$%*+,-.:;=?@[]^_{|}~"

// blurhashMaxSize caps the working dimension fed into the DCT. The
// hash quantises into a handful of cosine components anyway, so any
// resolution beyond ~64 px on the longest side is wasted work.
// Auto-resizing internally turns a 4096² source from a 2-second,
// 470 MB allocation into a millisecond-scale call.
const blurhashMaxSize = 64

// BlurHash returns a compact base83 placeholder string for the current
// image, following the spec at https://blurha.sh. xComp and yComp control
// the number of DCT components on each axis; both must be in 1..9.
// Typical values are 4×3 (landscape) or 3×4 (portrait).
//
// The algorithm scales with width × height × components, so callers
// processing large images should resize first:
//
//	hash, _ := img.Resize(32, 32, WithFit(FitInside)).BlurHash(4, 3)
func (i *Image) BlurHash(xComp, yComp int) (string, error) {
	if xComp < 1 || xComp > 9 || yComp < 1 || yComp > 9 {
		return "", errors.New("image: BlurHash components must be in 1..9")
	}
	b := i.img.Bounds()
	w, h := b.Dx(), b.Dy()
	if w == 0 || h == 0 {
		return "", errors.New("image: empty image")
	}
	if w*h < xComp*yComp {
		return "", fmt.Errorf("image: BlurHash needs at least %d×%d pixels for %d×%d components; got %d×%d",
			xComp, yComp, xComp, yComp, w, h)
	}

	// Auto-resize down to blurhashMaxSize on the longest side. The
	// hash output is identical to within rounding noise vs. the full-
	// resolution computation — BlurHash quantises into a tiny number
	// of cosine components, so the extra precision doesn't change
	// the result while the cost scales linearly with pixel count.
	if w > blurhashMaxSize || h > blurhashMaxSize {
		// Pass the cap on BOTH axes with FitInside so the longest side
		// is bounded by blurhashMaxSize regardless of aspect. Passing
		// (cap, 0) only caps width — for tall portraits the cosine
		// loop would still walk thousands of rows, and for extreme
		// aspect ratios FitInside would UPSCALE the short axis.
		// WithoutEnlargement keeps small inputs (one side under the
		// cap, the other over) from inflating.
		small := i.Resize(blurhashMaxSize, blurhashMaxSize, WithFit(FitInside), WithoutEnlargement())
		b = small.img.Bounds()
		w, h = b.Dx(), b.Dy()
		i = small
	}

	// Pre-convert pixels to linear RGB to avoid redoing the sRGB curve
	// inside the inner loop.
	linR := make([]float64, w*h)
	linG := make([]float64, w*h)
	linB := make([]float64, w*h)
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			r16, g16, b16, _ := i.img.At(b.Min.X+x, b.Min.Y+y).RGBA()
			idx := y*w + x
			linR[idx] = srgbToLinear(uint8(r16 >> 8))
			linG[idx] = srgbToLinear(uint8(g16 >> 8))
			linB[idx] = srgbToLinear(uint8(b16 >> 8))
		}
	}

	factors := make([][3]float64, xComp*yComp)
	norm := 1.0 / float64(w*h)

	for j := 0; j < yComp; j++ {
		for ic := 0; ic < xComp; ic++ {
			var rSum, gSum, bSum float64
			scale := 1.0
			if !(ic == 0 && j == 0) {
				scale = 2.0
			}
			for y := 0; y < h; y++ {
				cy := math.Cos(math.Pi * float64(j) * float64(y) / float64(h))
				for x := 0; x < w; x++ {
					cx := math.Cos(math.Pi * float64(ic) * float64(x) / float64(w))
					basis := cx * cy
					idx := y*w + x
					rSum += basis * linR[idx]
					gSum += basis * linG[idx]
					bSum += basis * linB[idx]
				}
			}
			factors[j*xComp+ic][0] = rSum * scale * norm
			factors[j*xComp+ic][1] = gSum * scale * norm
			factors[j*xComp+ic][2] = bSum * scale * norm
		}
	}

	var maxVal float64
	for k := 1; k < len(factors); k++ {
		for c := 0; c < 3; c++ {
			v := math.Abs(factors[k][c])
			if v > maxVal {
				maxVal = v
			}
		}
	}

	var quantMax int
	acScale := 1.0
	if len(factors) > 1 {
		if maxVal > 0 {
			q := math.Floor(maxVal*166 - 0.5)
			if q < 0 {
				q = 0
			}
			if q > 82 {
				q = 82
			}
			quantMax = int(q)
			acScale = (float64(quantMax) + 1) / 166.0
		}
	}

	out := make([]byte, 0, 2+4+(xComp*yComp-1)*2)
	sizeFlag := (xComp - 1) + (yComp-1)*9
	out = appendBase83(out, sizeFlag, 1)
	out = appendBase83(out, quantMax, 1)

	out = appendBase83(out, encodeBlurHashDC(factors[0]), 4)
	for k := 1; k < len(factors); k++ {
		out = appendBase83(out, encodeBlurHashAC(factors[k], acScale), 2)
	}
	return string(out), nil
}

// encodeBlurHashDC packs the linear-RGB DC factor into a 24-bit value.
func encodeBlurHashDC(f [3]float64) int {
	r := linearToSRGB(f[0])
	g := linearToSRGB(f[1])
	b := linearToSRGB(f[2])
	return (r << 16) | (g << 8) | b
}

// encodeBlurHashAC packs an AC factor into a 15-bit base-19 triple.
func encodeBlurHashAC(f [3]float64, maximumValue float64) int {
	if maximumValue <= 0 {
		return 9*19*19 + 9*19 + 9
	}
	qR := quantAC(f[0] / maximumValue)
	qG := quantAC(f[1] / maximumValue)
	qB := quantAC(f[2] / maximumValue)
	return qR*19*19 + qG*19 + qB
}

func quantAC(v float64) int {
	q := math.Floor(signPow(v, 0.5)*9 + 9.5)
	if q < 0 {
		q = 0
	}
	if q > 18 {
		q = 18
	}
	return int(q)
}

func signPow(v, exp float64) float64 {
	s := 1.0
	if v < 0 {
		s = -1.0
		v = -v
	}
	return s * math.Pow(v, exp)
}

func srgbToLinear(v uint8) float64 {
	x := float64(v) / 255.0
	if x <= 0.04045 {
		return x / 12.92
	}
	return math.Pow((x+0.055)/1.055, 2.4)
}

func linearToSRGB(v float64) int {
	x := v
	if x < 0 {
		x = 0
	}
	if x > 1 {
		x = 1
	}
	var y float64
	if x <= 0.0031308 {
		y = x * 12.92 * 255
	} else {
		y = (1.055*math.Pow(x, 1.0/2.4) - 0.055) * 255
	}
	return int(math.Floor(y + 0.5))
}

func appendBase83(dst []byte, value, length int) []byte {
	for i := 1; i <= length; i++ {
		digit := (value / pow83(length-i)) % 83
		dst = append(dst, base83Alphabet[digit])
	}
	return dst
}

func pow83(n int) int {
	p := 1
	for i := 0; i < n; i++ {
		p *= 83
	}
	return p
}
