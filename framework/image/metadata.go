package image

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
	if m, ok := i.img.(interface{ Opaque() bool }); ok {
		return !m.Opaque()
	}
	b := i.img.Bounds()
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			_, _, _, a := i.img.At(x, y).RGBA()
			if a != 0xFFFF {
				return true
			}
		}
	}
	return false
}
