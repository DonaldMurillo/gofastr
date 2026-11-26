package image

// PlaceholderOptions configures Placeholder.
type PlaceholderOptions struct {
	// Width is the placeholder width, 16 when zero.
	Width int

	// Quality is the JPEG quality (1..100), 40 when zero.
	Quality int
}

// Placeholder returns a small JPEG data: URL placeholder.
func (i *Image) Placeholder(opts ...PlaceholderOptions) (string, error) {
	o := PlaceholderOptions{Width: 16, Quality: 40}
	if len(opts) > 0 {
		if opts[0].Width > 0 {
			o.Width = opts[0].Width
		}
		if opts[0].Quality > 0 {
			o.Quality = opts[0].Quality
		}
	}
	if err := i.checkResize(o.Width, 0); err != nil {
		return "", err
	}
	return i.Resize(o.Width, 0).
		JPEG(JPEGOptions{Quality: o.Quality}).
		DataURL()
}
