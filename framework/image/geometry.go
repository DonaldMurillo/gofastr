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

// Name returns the filter's identifier (useful for logging).
func (f Filter) Name() string { return f.name }

// Resampling kernels. x/image/draw does not ship a Lanczos kernel, so
// Lanczos3 and Lanczos2 are aliases for the highest-quality interpolators
// available pure-Go (CatmullRom and BiLinear respectively). Names mirror
// Bun.Image so callers porting from a JS pipeline find what they expect.
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
	// FitFill stretches the source to fill the target exactly. May change
	// aspect ratio. Default.
	FitFill Fit = iota
	// FitInside fits the source within the target while preserving aspect
	// ratio; output may be smaller than the target on one axis.
	FitInside
	// FitOutside fills the target while preserving aspect ratio; output
	// may exceed the target on one axis.
	FitOutside
)

// ResizeOption configures Resize.
type ResizeOption func(*resizeOpts)

type resizeOpts struct {
	filter         Filter
	fit            Fit
	withoutEnlarge bool
}

// WithFilter selects the resampling kernel. Default is Lanczos3 (CatmullRom).
func WithFilter(f Filter) ResizeOption { return func(o *resizeOpts) { o.filter = f } }

// WithFit selects the fit strategy. Default is FitFill.
func WithFit(f Fit) ResizeOption { return func(o *resizeOpts) { o.fit = f } }

// WithoutEnlargement skips the resize when the target would enlarge the
// source on either axis (mirrors Sharp / Bun.Image semantics).
func WithoutEnlargement() ResizeOption { return func(o *resizeOpts) { o.withoutEnlarge = true } }

// Resize returns a new *Image scaled to the target box. If height is
// 0 or negative it is computed from width preserving aspect; same for
// width when height is 0 or negative. Width and height both 0 (or
// both negative) return the source unchanged. Use Sharp-style "skip
// if larger than source" via WithoutEnlargement().
//
// A target whose area exceeds the image's MaxPixels (Config.MaxPixels,
// else DefaultMaxPixels) panics with an error wrapping
// ErrTargetTooLarge, since Resize returns no error. VariantSet.Process,
// VariantSet.ProcessTo and Placeholder check first and return it.
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

// checkResize reports the error Resize would panic with for this target.
func (i *Image) checkResize(width, height int, opts ...ResizeOption) error {
	o := resizeOpts{filter: Lanczos3, fit: FitFill}
	for _, fn := range opts {
		fn(&o)
	}
	_, _, _, err := i.resizeBox(width, height, o)
	return err
}

// maxPixels is the area cap for this image's resize targets.
func (i *Image) maxPixels() int64 {
	if i.cfg.MaxPixels > 0 {
		return i.cfg.MaxPixels
	}
	return DefaultMaxPixels
}

// resizeBox is Resize's target size: keep reports that the source is
// returned unchanged. The aspect and fit arithmetic cannot overflow, and
// an area past maxPixels is ErrTargetTooLarge.
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
	if o.fit == FitInside || o.fit == FitOutside {
		sx := float64(tw) / float64(sw)
		sy := float64(th) / float64(sh)
		s := min(sx, sy)
		if o.fit == FitOutside {
			s = max(sx, sy)
		}
		fw, fh := float64(sw)*s, float64(sh)*s
		// A side past the cap is an area past it (the other side is at
		// least 1), and checking first keeps the int conversion in range.
		if limit := float64(i.maxPixels()); fw > limit || fh > limit {
			return tooLarge()
		}
		tw, th = int(fw), int(fh)
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

// areaWithin reports whether a w×h box (both positive) holds at most
// limit pixels, without overflowing.
func areaWithin(w, h int, limit int64) bool {
	hi, lo := bits.Mul64(uint64(w), uint64(h))
	return hi == 0 && lo <= uint64(limit)
}

// mulDiv is a*b/c for positive ints, false when the result overflows int.
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

// Rotate returns a new *Image rotated by 0/90/180/270 degrees clockwise.
// Other values are normalised modulo 360 and rounded down to the nearest 90.
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

// AutoOrient applies the EXIF orientation tag from decode time, if
// any, and clears it. Safe to call when no orientation is known. On
// the no-op path (orient ≤ 1, or no EXIF metadata) the returned
// *Image shares the underlying image.Image with the receiver. If a
// caller plans to mutate via GoImage(), they should clone first.
//
// EXIF tag values:
//
//	1: identity                 5: transpose (rotate 90 then flop)
//	2: flop                     6: rotate 90 CW
//	3: rotate 180               7: transverse (rotate 270 then flop)
//	4: flip                     8: rotate 270 CW
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
