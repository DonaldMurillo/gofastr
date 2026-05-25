package image

// Metadata summarises an image's identifying attributes.
type Metadata struct {
	Width       int
	Height      int
	Format      Format
	HasAlpha    bool
	Orientation int // EXIF orientation tag 1..8; 0 if absent or already applied
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
	}
}

func hasAlpha(i *Image) bool {
	switch m := i.img.ColorModel(); m {
	case nil:
		return false
	default:
		// Sample the corners and centre cheaply; if every alpha is 0xFFFF
		// we report no alpha. This is a conservative heuristic — exact
		// per-pixel sweep is too expensive for an attribute that callers
		// usually want as a hint.
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
	}
	return false
}
