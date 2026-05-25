package image

import (
	stdimage "image"
	"io"

	"github.com/DonaldMurillo/gofastr/framework/image/internal/vp8l"
)

// encodeWebPLossless is the bridge from Image.WebP(Lossless:true) into
// the internal VP8L encoder. Pure Go, lossless byte-for-byte, no CGo.
func encodeWebPLossless(w io.Writer, img stdimage.Image) error {
	return vp8l.Encode(w, img)
}
