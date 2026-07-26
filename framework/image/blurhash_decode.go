package image

import (
	"fmt"
	stdimage "image"
	"image/color"
	"math"
)

// DefaultBlurHashRenderSize is the square edge DecodeBlurHash produces
// when Width/Height are left zero.
//
// 20 px is chosen against the information content of the hash, not by
// eye: a hash holds at most 9×9 cosine components, so 20 px still samples
// the busiest possible hash about twice per component and a typical 4×3
// one about five times. Rendering larger cannot recover detail the hash
// never carried — it only costs bytes inlined into every HTML response.
const DefaultBlurHashRenderSize = 20

// MaxBlurHashRenderSize caps each output axis. Decoding costs
// width × height × components, and the hash itself is untrusted input
// (it comes out of a database column some upload wrote), so the output
// size a caller may request is bounded rather than trusted.
const MaxBlurHashRenderSize = 128

// BlurHashRenderConfig configures BlurHash decoding.
type BlurHashRenderConfig struct {
	// Width and Height are the output pixel dimensions. Zero means
	// DefaultBlurHashRenderSize; neither may exceed MaxBlurHashRenderSize.
	//
	// These need not match the real image's aspect ratio — the output is
	// stretched to fill whatever box the UI paints it into, and a blur has
	// no detail for the distortion to spoil.
	Width, Height int

	// Punch scales the AC (detail) components, raising contrast above the
	// washed-out look a raw decode produces. Zero means 1.0 (no change);
	// the reference implementations use the same knob.
	Punch float64

	// Format selects the placeholder encoding used by BlurHashDataURL.
	// Zero means FormatJPEG, which is the smallest option for the smooth
	// gradients a decoded blur consists of. FormatPNG is available for
	// callers who want lossless output for flat-color content.
	Format Format

	// Quality is the JPEG quality for BlurHashDataURL (1..100). Zero means
	// defaultBlurHashQuality. Ignored when Format is FormatPNG.
	Quality int
}

// DecodeBlurHash renders a BlurHash string back into an image, returning
// it as a pipeline value so it composes with the encoders:
//
//	durl, err := image.DecodeBlurHash(hash, image.BlurHashRenderConfig{}).
//	    JPEG(image.JPEGOptions{Quality: 50}).DataURL()
//
// Most callers want BlurHashDataURL instead, which does exactly that and
// memoises the result.
//
// The hash is treated as untrusted: length, alphabet, and component count
// are all validated before any pixel buffer is allocated.
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

	// Precompute the cosine bases per axis. Without this the inner loop
	// recomputes math.Cos width × height × components times; a 32×32
	// output at 4×3 components is ~12k redundant Cos calls.
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

// blurHashComponents is the parsed form of a hash: the component counts
// plus the linear-RGB DCT factors, with Punch already applied.
type blurHashComponents struct {
	numX, numY int
	factors    [][3]float64
}

func decodeBlurHashComponents(hash string, punch float64) (blurHashComponents, error) {
	var zero blurHashComponents
	// The minimum valid hash is 1×1 components: 1 size + 1 quant + 4 DC.
	if len(hash) < 6 {
		return zero, fmt.Errorf("image: BlurHash too short (%d chars, minimum 6)", len(hash))
	}
	digits, err := decodeBase83(hash)
	if err != nil {
		return zero, err
	}

	sizeFlag := digits[0]
	numX := sizeFlag%9 + 1
	numY := sizeFlag/9 + 1
	// Mirrors the encoder's layout at blurhash.go: 1 size + 1 quant +
	// 4 DC + 2 per AC term.
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
	// The DC term is a 24-bit RGB triple written as four positional base83
	// digits (see appendBase83 in blurhash.go) — not a bit-packed field.
	dc := base83Value(digits[2:6])
	factors[0] = [3]float64{
		srgbToLinear(uint8(dc >> 16)),
		srgbToLinear(uint8((dc >> 8) & 255)),
		srgbToLinear(uint8(dc & 255)),
	}
	for k := 1; k < len(factors); k++ {
		factors[k] = decodeBlurHashAC(base83Value(digits[4+k*2:6+k*2]), maxValue*punch)
	}
	return blurHashComponents{numX: numX, numY: numY, factors: factors}, nil
}

// base83Value folds positional base83 digits into their integer value,
// inverse of appendBase83 in blurhash.go.
func base83Value(digits []int) int {
	v := 0
	for _, d := range digits {
		v = v*83 + d
	}
	return v
}

// decodeBlurHashAC unpacks one 15-bit base-19 triple back into a linear-RGB
// AC factor. Inverse of encodeBlurHashAC in blurhash.go.
func decodeBlurHashAC(value int, maximumValue float64) [3]float64 {
	q := 19 * 19
	return [3]float64{
		signPow((float64(value/q)-9)/9, 2.0) * maximumValue,
		signPow((float64((value/19)%19)-9)/9, 2.0) * maximumValue,
		signPow((float64(value%19)-9)/9, 2.0) * maximumValue,
	}
}

// decodeBase83 converts every character of s to its base83 digit,
// rejecting anything outside the alphabet. Decoding the whole string up
// front means an invalid character is caught before any allocation sized
// from the hash's own header.
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

// base83Digit returns the base83 value of c, or -1 when c is not in the
// alphabet. Indexing a byte at a time is deliberate: a multi-byte UTF-8
// rune can never be a valid base83 digit, and each of its bytes fails
// this lookup, so non-ASCII input is rejected without a decode pass.
func base83Digit(c byte) int {
	for i := 0; i < len(base83Alphabet); i++ {
		if base83Alphabet[i] == c {
			return i
		}
	}
	return -1
}
