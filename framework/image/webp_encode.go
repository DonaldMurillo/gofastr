package image

import (
	"fmt"
	stdimage "image"
	"io"
)

// encodeWebPLossless is the entry point for VP8L (lossless WebP) encoding.
// The body lives behind an internal/webp subpackage that is added in a
// follow-up commit; until then any caller of Image.WebP receives an
// ErrFormatUnsupported on terminal calls.
func encodeWebPLossless(w io.Writer, img stdimage.Image) error {
	return fmt.Errorf("%w: lossless WebP encoder not yet wired in", ErrFormatUnsupported)
}
