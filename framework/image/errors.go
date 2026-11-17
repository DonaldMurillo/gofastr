package image

import "errors"

var (
	// ErrFormatUnsupported is returned for an output this package cannot encode.
	ErrFormatUnsupported = errors.New("image: format not supported for encoding")

	// ErrDecompressionBomb is returned when a source would cost more than a Config cap.
	ErrDecompressionBomb = errors.New("image: decompression bomb guard tripped")

	// ErrInvalidInput is returned for a source that is not a readable image.
	ErrInvalidInput = errors.New("image: invalid input")

	// ErrAnimatedSource is returned by a VariantSet with RejectAnimated.
	ErrAnimatedSource = errors.New("image: animated source rejected (set VariantSet.RejectAnimated=false to flatten to first frame)")

	// ErrSourceTooLarge is returned past Config.MaxSourceBytes.
	ErrSourceTooLarge = errors.New("image: source exceeds MaxSourceBytes")

	// ErrTargetTooLarge reports an output area past MaxPixels.
	ErrTargetTooLarge = errors.New("image: resize target exceeds MaxPixels")
)
