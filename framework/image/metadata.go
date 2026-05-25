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
	// produces. Read Pix[3::4] directly so the per-pixel interface
	// boxing of the generic At() loop disappears. Early exit on the
	// first sub-opaque alpha.
	switch m := i.img.(type) {
	case *stdimage.NRGBA:
		return scanAlphaPix(m.Pix, 4)
	case *stdimage.RGBA:
		return scanAlphaPix(m.Pix, 4)
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

// scanAlphaPix walks every alpha byte in a 4-byte-per-pixel buffer.
// Returns true on the first non-0xFF value.
func scanAlphaPix(pix []byte, stride int) bool {
	for i := 3; i < len(pix); i += stride {
		if pix[i] != 0xFF {
			return true
		}
	}
	return false
}
