package image

import (
	stdimage "image"

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

// Resize returns a new *Image scaled to the target box. If height is 0 it is
// computed from width preserving aspect; same for width when height is 0.
// Width and height of 0 return the source unchanged.
func (i *Image) Resize(width, height int, opts ...ResizeOption) *Image {
	o := resizeOpts{filter: Lanczos3, fit: FitFill}
	for _, fn := range opts {
		fn(&o)
	}
	sb := i.img.Bounds()
	sw, sh := sb.Dx(), sb.Dy()
	if width <= 0 && height <= 0 {
		return i
	}
	if sw == 0 || sh == 0 {
		return i
	}
	if width <= 0 {
		width = sw * height / sh
	} else if height <= 0 {
		height = sh * width / sw
	}
	tw, th := width, height
	switch o.fit {
	case FitInside:
		sx := float64(tw) / float64(sw)
		sy := float64(th) / float64(sh)
		s := sx
		if sy < sx {
			s = sy
		}
		tw = int(float64(sw) * s)
		th = int(float64(sh) * s)
	case FitOutside:
		sx := float64(tw) / float64(sw)
		sy := float64(th) / float64(sh)
		s := sx
		if sy > sx {
			s = sy
		}
		tw = int(float64(sw) * s)
		th = int(float64(sh) * s)
	}
	if o.withoutEnlarge && (tw > sw || th > sh) {
		return i
	}
	if tw < 1 {
		tw = 1
	}
	if th < 1 {
		th = 1
	}
	dst := stdimage.NewRGBA(stdimage.Rect(0, 0, tw, th))
	o.filter.interp.Scale(dst, dst.Bounds(), i.img, sb, draw.Src, nil)
	return i.derive(dst)
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

// AutoOrient applies the EXIF orientation tag from decode time, if any,
// and clears it. Safe to call when no orientation is known.
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
