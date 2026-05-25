package image

import "errors"

var (
	// ErrFormatUnsupported is returned by encoders for formats this package
	// cannot produce. Currently: WebP lossy, HEIC, AVIF.
	ErrFormatUnsupported = errors.New("image: format not supported for encoding")

	// ErrDecompressionBomb is returned when an input image's reported
	// dimensions exceed the configured MaxPixels guard.
	ErrDecompressionBomb = errors.New("image: decompression bomb guard tripped")

	// ErrInvalidInput is returned when a source cannot be identified as a
	// supported image format.
	ErrInvalidInput = errors.New("image: invalid input")
)
