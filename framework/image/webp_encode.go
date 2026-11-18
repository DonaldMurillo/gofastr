package image

import (
	stdimage "image"
	"io"

	"github.com/DonaldMurillo/gofastr/framework/image/internal/vp8l"
)

func encodeWebPLossless(w io.Writer, img stdimage.Image) error {
	return vp8l.Encode(w, img)
}
