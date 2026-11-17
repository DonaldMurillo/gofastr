package image

import (
	"fmt"
	stdimage "image"
	"math"
	"math/bits"

	"golang.org/x/image/draw"
)

// Filter selects a resampling kernel for Resize.
type Filter struct {
	name   string
	interp draw.Interpolator
}

// Name returns the filter's identifier.
func (f Filter) Name() string { return f.name }

// Resampling kernels. Lanczos3 is CatmullRom and Lanczos2 is BiLinear.
var (
	Nearest        = Filter{"nearest", draw.NearestNeighbor}
	BiLinear       = Filter{"bilinear", draw.BiLinear}
	ApproxBiLinear = Filter{"approx-bilinear", draw.ApproxBiLinear}
	CatmullRom     = Filter{"catmull-rom", draw.CatmullRom}
	Lanczos3       = CatmullRom
	Lanczos2       = BiLinear
)

// Fit controls how Resize maps the source into the target box.
type Fit int

const (
	// FitFill stretches the source to the target exactly. Default.
	FitFill Fit = iota
	// FitInside keeps the aspect ratio inside the target box.
	FitInside
	// FitOutside keeps the aspect ratio and covers the target box.
	FitOutside
)

// ResizeOption configures Resize.
type ResizeOption func(*resizeOpts)

type resizeOpts struct {
	filter         Filter
	fit            Fit
	withoutEnlarge bool
}

// WithFilter selects the resampling kernel, Lanczos3 by default.
func WithFilter(f Filter) ResizeOption { return func(o *resizeOpts) { o.filter = f } }

// WithFit selects the fit, FitFill by default.
func WithFit(f Fit) ResizeOption { return func(o *resizeOpts) { o.fit = f } }

// WithoutEnlargement skips a resize that would enlarge either axis.
func WithoutEnlargement() ResizeOption { return func(o *resizeOpts) { o.withoutEnlarge = true } }

// Resize scales to the target box, deriving a side of 0 or less from the
// other. It panics with ErrTargetTooLarge past MaxPixels.
func (i *Image) Resize(width, height int, opts ...ResizeOption) *Image {
	o := resizeOpts{filter: Lanczos3, fit: FitFill}
	for _, fn := range opts {
		fn(&o)
	}
	tw, th, keep, err := i.resizeBox(width, height, o)
	if err != nil {
		panic(err)
	}
	if keep {
		return i
	}
	dst := stdimage.NewRGBA(stdimage.Rect(0, 0, tw, th))
	o.filter.interp.Scale(dst, dst.Bounds(), i.img, i.img.Bounds(), draw.Src, nil)
	return i.derive(dst)
}

func (i *Image) checkResize(width, height int, opts ...ResizeOption) error {
	o := resizeOpts{filter: Lanczos3, fit: FitFill}
	for _, fn := range opts {
		fn(&o)
	}
	_, _, _, err := i.resizeBox(width, height, o)
	return err
}

func (i *Image) maxPixels() int64 {
	if i.cfg.MaxPixels > 0 {
		return i.cfg.MaxPixels
	}
	return DefaultMaxPixels
}

func (i *Image) resizeBox(width, height int, o resizeOpts) (tw, th int, keep bool, err error) {
	sb := i.img.Bounds()
	sw, sh := sb.Dx(), sb.Dy()
	if width <= 0 && height <= 0 {
		return 0, 0, true, nil
	}
	if sw == 0 || sh == 0 {
		return 0, 0, true, nil
	}
	tooLarge := func() (int, int, bool, error) {
		return 0, 0, false, fmt.Errorf("%w: %dx%d from %dx%d, MaxPixels %d", ErrTargetTooLarge, width, height, sw, sh, i.maxPixels())
	}
	var ok bool
	derived := width <= 0 || height <= 0
	if width <= 0 {
		if width, ok = mulDiv(sw, height, sh); !ok {
			return tooLarge()
		}
	} else if height <= 0 {
		if height, ok = mulDiv(sh, width, sw); !ok {
			return tooLarge()
		}
	}
	tw, th = width, height
	if !derived && (o.fit == FitInside || o.fit == FitOutside) {
		byWidth, fits := mulDiv(sh, width, sw)
		if o.fit == FitInside {
			if fits && byWidth <= height {
				th = byWidth
			} else if tw, ok = mulDiv(sw, height, sh); !ok {
				return tooLarge()
			}
		} else {
			if !fits {
				return tooLarge()
			}
			if byWidth >= height {
				th = byWidth
			} else if tw, ok = mulDiv(sw, height, sh); !ok {
				return tooLarge()
			}
		}
	}
	if o.withoutEnlarge && (tw > sw || th > sh) {
		return 0, 0, true, nil
	}
	tw, th = max(tw, 1), max(th, 1)
	if !areaWithin(tw, th, i.maxPixels()) {
		return tooLarge()
	}
	return tw, th, false, nil
}

func areaWithin(w, h int, limit int64) bool {
	hi, lo := bits.Mul64(uint64(w), uint64(h))
	return hi == 0 && lo <= uint64(limit)
}

func mulDiv(a, b, c int) (int, bool) {
	hi, lo := bits.Mul64(uint64(a), uint64(b))
	if hi >= uint64(c) {
		return 0, false
	}
	q, _ := bits.Div64(hi, lo, uint64(c))
	if q > math.MaxInt {
		return 0, false
	}
	return int(q), true
}

// Rotate rotates clockwise by degrees rounded down to a multiple of 90.
func (i *Image) Rotate(degrees int) *Image {
	n := ((degrees % 360) + 360) % 360
	n = (n / 90) * 90
	if n == 0 {
		return i
	}
	sb := i.img.Bounds()
	sw, sh := sb.Dx(), sb.Dy()
	var dst *stdimage.RGBA
	switch n {
	case 90:
		dst = stdimage.NewRGBA(stdimage.Rect(0, 0, sh, sw))
		for y := 0; y < sh; y++ {
			for x := 0; x < sw; x++ {
				dst.Set(sh-1-y, x, i.img.At(sb.Min.X+x, sb.Min.Y+y))
			}
		}
	case 180:
		dst = stdimage.NewRGBA(stdimage.Rect(0, 0, sw, sh))
		for y := 0; y < sh; y++ {
			for x := 0; x < sw; x++ {
				dst.Set(sw-1-x, sh-1-y, i.img.At(sb.Min.X+x, sb.Min.Y+y))
			}
		}
	case 270:
		dst = stdimage.NewRGBA(stdimage.Rect(0, 0, sh, sw))
		for y := 0; y < sh; y++ {
			for x := 0; x < sw; x++ {
				dst.Set(y, sw-1-x, i.img.At(sb.Min.X+x, sb.Min.Y+y))
			}
		}
	}
	return i.derive(dst)
}

// Flip returns a new *Image mirrored vertically (top↔bottom).
func (i *Image) Flip() *Image {
	sb := i.img.Bounds()
	sw, sh := sb.Dx(), sb.Dy()
	dst := stdimage.NewRGBA(stdimage.Rect(0, 0, sw, sh))
	for y := 0; y < sh; y++ {
		for x := 0; x < sw; x++ {
			dst.Set(x, sh-1-y, i.img.At(sb.Min.X+x, sb.Min.Y+y))
		}
	}
	return i.derive(dst)
}

// Flop returns a new *Image mirrored horizontally (left↔right).
func (i *Image) Flop() *Image {
	sb := i.img.Bounds()
	sw, sh := sb.Dx(), sb.Dy()
	dst := stdimage.NewRGBA(stdimage.Rect(0, 0, sw, sh))
	for y := 0; y < sh; y++ {
		for x := 0; x < sw; x++ {
			dst.Set(sw-1-x, y, i.img.At(sb.Min.X+x, sb.Min.Y+y))
		}
	}
	return i.derive(dst)
}

// AutoOrient applies and clears the EXIF orientation from decode.
func (i *Image) AutoOrient() *Image {
	if i.orient <= 1 {
		out := i.derive(i.img)
		out.orient = 0
		return out
	}
	var out *Image
	switch i.orient {
	case 2:
		out = i.Flop()
	case 3:
		out = i.Rotate(180)
	case 4:
		out = i.Flip()
	case 5:
		out = i.Rotate(90).Flop()
	case 6:
		out = i.Rotate(90)
	case 7:
		out = i.Rotate(270).Flop()
	case 8:
		out = i.Rotate(270)
	default:
		out = i.derive(i.img)
	}
	out.orient = 0
	return out
}
