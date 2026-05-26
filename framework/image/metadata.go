package image

import stdimage "image"

// Metadata summarises an image's identifying attributes.
type Metadata struct {
	Width       int
	Height      int
	Format      Format
	HasAlpha    bool
	Orientation int // EXIF orientation tag 1..8; 0 if absent or already applied
	// FrameCount surfaces animated-image frame totals. The decoder
	// returns only the first frame; FrameCount > 1 lets callers detect
	// "I just dropped N-1 frames" instead of silently mishandling
	// animated GIFs. 0 or 1 indicates a still image.
	FrameCount int
}

// Metadata returns a snapshot of the current image's attributes.
func (i *Image) Metadata() Metadata {
	b := i.img.Bounds()
	return Metadata{
		Width:       b.Dx(),
		Height:      b.Dy(),
		Format:      i.format,
		HasAlpha:    hasAlpha(i),
		Orientation: i.orient,
		FrameCount:  i.frames,
	}
}

func hasAlpha(i *Image) bool {
	// Fast paths for the two concrete types the framework actually
	// produces. Walk per-row respecting Rect+Stride so a SubImage's
	// out-of-bounds bytes (and any row padding when Stride > 4*Dx)
	// don't pollute the result. Early exit on the first sub-opaque
	// alpha.
	switch m := i.img.(type) {
	case *stdimage.NRGBA:
		return scanAlphaRect(m.Pix, m.Stride, m.Rect.Dx(), m.Rect.Dy())
	case *stdimage.RGBA:
		return scanAlphaRect(m.Pix, m.Stride, m.Rect.Dx(), m.Rect.Dy())
	}
	// Generic fallback for any other image.Image: corner + centre
	// sampling. Cheap, conservative, can miss interior alpha.
	if i.img.ColorModel() == nil {
		return false
	}
	b := i.img.Bounds()
	sx := []int{b.Min.X, b.Max.X - 1, b.Min.X + b.Dx()/2}
	sy := []int{b.Min.Y, b.Max.Y - 1, b.Min.Y + b.Dy()/2}
	for _, y := range sy {
		for _, x := range sx {
			_, _, _, a := i.img.At(x, y).RGBA()
			if a != 0xFFFF {
				return true
			}
		}
	}
	return false
}

// scanAlphaRect walks the alpha byte of every pixel inside a w×h
// rectangle laid out at 4-bytes-per-pixel with the given row Stride.
// Honoring Stride and the visible row length is what makes SubImage
// inputs correct: the parent's Pix buffer extends past each visible
// row and may contain alpha bytes that belong to neighbouring pixels.
func scanAlphaRect(pix []byte, stride, w, h int) bool {
	rowBytes := w * 4
	for y := 0; y < h; y++ {
		base := y * stride
		end := base + rowBytes
		for i := base + 3; i < end; i += 4 {
			if pix[i] != 0xFF {
				return true
			}
		}
	}
	return false
}
