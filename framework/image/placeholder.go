package image

// PlaceholderOptions configures Placeholder.
type PlaceholderOptions struct {
	// Width is the target placeholder width in pixels. Height is computed
	// from the source aspect ratio. Default 16.
	Width int

	// Quality is the JPEG quality for the placeholder (1..100). Default 40.
	Quality int
}

// Placeholder returns a small base64 data: URL suitable for use as an
// LQIP (low-quality image placeholder). The default produces ~500-byte
// JPEGs that decode instantly and can be inlined into HTML.
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
	return i.Resize(o.Width, 0, WithFit(FitInside)).
		JPEG(JPEGOptions{Quality: o.Quality}).
		DataURL()
}
