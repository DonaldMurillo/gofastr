package image

// Blank imports register decoders with stdlib's image package so
// image.Decode and image.DecodeConfig recognise every format the
// pipeline accepts as input. WebP is decode-only — encoding is
// limited to lossless via the local webp subpackage.
import (
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"

	_ "golang.org/x/image/bmp"
	_ "golang.org/x/image/tiff"
	_ "golang.org/x/image/webp"
)
