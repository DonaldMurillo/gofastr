package image

import stdimage "image"

// Metadata summarises an image's identifying attributes.
type Metadata struct {
	Width    int
	Height   int
	Format   Format
	HasAlpha bool
	// Orientation is the pending EXIF orientation, 0 for none.
	Orientation int
	// FrameCount is the source's frame count; the pixels are frame one.
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
	switch m := i.img.(type) {
	case *stdimage.NRGBA:
		return scanAlphaRect(m.Pix, m.Stride, m.Rect.Dx(), m.Rect.Dy())
	case *stdimage.RGBA:
		return scanAlphaRect(m.Pix, m.Stride, m.Rect.Dx(), m.Rect.Dy())
	}
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
