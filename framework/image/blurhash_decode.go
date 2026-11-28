package image

import (
	"fmt"
	stdimage "image"
	"image/color"
	"math"
)

// DefaultBlurHashRenderSize is DecodeBlurHash's default square edge.
const DefaultBlurHashRenderSize = 20

// MaxBlurHashRenderSize caps each DecodeBlurHash output axis.
const MaxBlurHashRenderSize = 128

// BlurHashRenderConfig configures BlurHash decoding.
type BlurHashRenderConfig struct {
	// Width and Height are the output size, 0 for the default.
	Width, Height int

	// Punch scales the detail components, 0 meaning 1.
	Punch float64

	// Format is BlurHashDataURL's encoding, JPEG unless FormatPNG.
	Format Format

	// Quality is BlurHashDataURL's JPEG quality, 0 meaning 50.
	Quality int
}

// DecodeBlurHash renders a BlurHash string, erroring on a malformed hash
// or size.
func DecodeBlurHash(hash string, cfg BlurHashRenderConfig) (*Image, error) {
	w, h := cfg.Width, cfg.Height
	if w == 0 {
		w = DefaultBlurHashRenderSize
	}
	if h == 0 {
		h = DefaultBlurHashRenderSize
	}
	if w < 1 || h < 1 {
		return nil, fmt.Errorf("image: BlurHash render dimensions must be positive, got %dx%d", w, h)
	}
	if w > MaxBlurHashRenderSize || h > MaxBlurHashRenderSize {
		return nil, fmt.Errorf("image: BlurHash render dimensions %dx%d exceed MaxBlurHashRenderSize=%d",
			w, h, MaxBlurHashRenderSize)
	}

	comps, err := decodeBlurHashComponents(hash, cfg.Punch)
	if err != nil {
		return nil, err
	}

	cosX := make([]float64, w*comps.numX)
	for x := 0; x < w; x++ {
		for i := 0; i < comps.numX; i++ {
			cosX[x*comps.numX+i] = math.Cos(math.Pi * float64(x) * float64(i) / float64(w))
		}
	}
	cosY := make([]float64, h*comps.numY)
	for y := 0; y < h; y++ {
		for j := 0; j < comps.numY; j++ {
			cosY[y*comps.numY+j] = math.Cos(math.Pi * float64(y) * float64(j) / float64(h))
		}
	}

	out := stdimage.NewNRGBA(stdimage.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			var r, g, b float64
			for j := 0; j < comps.numY; j++ {
				cy := cosY[y*comps.numY+j]
				for i := 0; i < comps.numX; i++ {
					basis := cosX[x*comps.numX+i] * cy
					c := comps.factors[j*comps.numX+i]
					r += c[0] * basis
					g += c[1] * basis
					b += c[2] * basis
				}
			}
			out.SetNRGBA(x, y, color.NRGBA{
				R: uint8(linearToSRGB(r)),
				G: uint8(linearToSRGB(g)),
				B: uint8(linearToSRGB(b)),
				A: 255,
			})
		}
	}
	return FromImage(out, FormatPNG), nil
}

type blurHashComponents struct {
	numX, numY int
	factors    [][3]float64
}

func decodeBlurHashComponents(hash string, punch float64) (blurHashComponents, error) {
	var zero blurHashComponents
	if len(hash) < 6 {
		return zero, fmt.Errorf("image: BlurHash too short (%d chars, minimum 6)", len(hash))
	}
	if len(hash) > 4+2*9*9 {
		return zero, fmt.Errorf("image: BlurHash too long (%d chars, maximum 166)", len(hash))
	}
	digits, err := decodeBase83(hash)
	if err != nil {
		return zero, err
	}

	sizeFlag := digits[0]
	if sizeFlag >= 9*9 {
		return zero, fmt.Errorf("image: BlurHash component header %d is out of range", sizeFlag)
	}
	numX := sizeFlag%9 + 1
	numY := sizeFlag/9 + 1
	want := 4 + 2*numX*numY
	if len(hash) != want {
		return zero, fmt.Errorf("image: BlurHash length %d does not match its %dx%d component header (want %d)",
			len(hash), numX, numY, want)
	}

	quantMax := digits[1]
	maxValue := float64(quantMax+1) / 166.0
	if punch <= 0 {
		punch = 1.0
	}

	factors := make([][3]float64, numX*numY)
	dc := base83Value(digits[2:6])
	if dc >= 1<<24 {
		return zero, fmt.Errorf("image: BlurHash DC component is out of range")
	}
	factors[0] = [3]float64{
		srgbToLinear(uint8(dc >> 16)),
		srgbToLinear(uint8((dc >> 8) & 255)),
		srgbToLinear(uint8(dc & 255)),
	}
	for k := 1; k < len(factors); k++ {
		ac := base83Value(digits[4+k*2 : 6+k*2])
		if ac >= 19*19*19 {
			return zero, fmt.Errorf("image: BlurHash AC component %d is out of range", k)
		}
		factors[k] = decodeBlurHashAC(ac, maxValue*punch)
	}
	return blurHashComponents{numX: numX, numY: numY, factors: factors}, nil
}

func base83Value(digits []int) int {
	v := 0
	for _, d := range digits {
		v = v*83 + d
	}
	return v
}

func decodeBlurHashAC(value int, maximumValue float64) [3]float64 {
	q := 19 * 19
	return [3]float64{
		signPow((float64(value/q)-9)/9, 2.0) * maximumValue,
		signPow((float64((value/19)%19)-9)/9, 2.0) * maximumValue,
		signPow((float64(value%19)-9)/9, 2.0) * maximumValue,
	}
}

func decodeBase83(s string) ([]int, error) {
	out := make([]int, len(s))
	for i := 0; i < len(s); i++ {
		d := base83Digit(s[i])
		if d < 0 {
			return nil, fmt.Errorf("image: BlurHash contains invalid base83 byte %q at offset %d", s[i], i)
		}
		out[i] = d
	}
	return out, nil
}

func base83Digit(c byte) int {
	for i := 0; i < len(base83Alphabet); i++ {
		if base83Alphabet[i] == c {
			return i
		}
	}
	return -1
}
