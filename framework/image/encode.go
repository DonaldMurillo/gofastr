package image

import (
	"bytes"
	"encoding/base64"
	"fmt"
	stdimage "image"
	"image/gif"
	"image/jpeg"
	"image/png"
	"io"
	"sync"

	"golang.org/x/image/bmp"
	"golang.org/x/image/tiff"
)

// Encoder is a configured terminal of an image pipeline. It captures the
// chosen output format and any per-format options. Call Bytes, Write,
// Base64, or DataURL to materialise the encoded image. Repeat calls
// against the same Encoder reuse the first encode's bytes instead of
// re-running the codec — even across multiple goroutines.
//
// Note: the cache is keyed by *Encoder identity, not by source pixels.
// Mutating the underlying image.Image via Image.GoImage() after the
// first terminal call will not invalidate the cache; the second call
// returns the original encoded bytes. Build a fresh Encoder after
// any direct pixel mutation.
type Encoder struct {
	img    *Image
	format Format
	encode func(io.Writer, stdimage.Image) error
	err    error

	once      sync.Once
	cached    []byte
	cachedErr error
}

// Format returns the Encoder's output format.
func (e *Encoder) Format() Format { return e.format }

// MIME returns the Content-Type for the chosen format.
func (e *Encoder) MIME() string { return e.format.MIME() }

// Write encodes the image to w. Goroutine-safe; the underlying codec
// runs at most once across all terminal calls on this Encoder.
func (e *Encoder) Write(w io.Writer) error {
	data, err := e.Bytes()
	if err != nil {
		return err
	}
	_, err = w.Write(data)
	return err
}

// Bytes returns the encoded image as a byte slice. Goroutine-safe;
// repeat calls (and concurrent calls) reuse the first encode's output
// instead of re-running the codec.
func (e *Encoder) Bytes() ([]byte, error) {
	if e.err != nil {
		return nil, e.err
	}
	e.once.Do(func() {
		var buf bytes.Buffer
		err := e.encode(&buf, e.img.img)
		e.cached = buf.Bytes()
		e.cachedErr = err
	})
	return e.cached, e.cachedErr
}

// Base64 returns the encoded image as a raw base64 string (no MIME prefix).
func (e *Encoder) Base64() (string, error) {
	data, err := e.Bytes()
	if err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(data), nil
}

// DataURL returns a data: URL with the format's MIME type.
func (e *Encoder) DataURL() (string, error) {
	data, err := e.Bytes()
	if err != nil {
		return "", err
	}
	return "data:" + e.MIME() + ";base64," + base64.StdEncoding.EncodeToString(data), nil
}

// JPEGOptions configures JPEG encoding.
type JPEGOptions struct {
	// Quality is the JPEG quality 1..100. 0 defaults to 80.
	Quality int
}

// JPEG selects JPEG output.
func (i *Image) JPEG(opts ...JPEGOptions) *Encoder {
	o := JPEGOptions{}
	if len(opts) > 0 {
		o = opts[0]
	}
	if o.Quality == 0 {
		o.Quality = 80
	}
	if o.Quality < 1 {
		o.Quality = 1
	}
	if o.Quality > 100 {
		o.Quality = 100
	}
	return &Encoder{
		img:    i,
		format: FormatJPEG,
		encode: func(w io.Writer, img stdimage.Image) error {
			return jpeg.Encode(w, img, &jpeg.Options{Quality: o.Quality})
		},
	}
}

// PNGOptions configures PNG encoding.
type PNGOptions struct {
	// Compression maps to image/png.CompressionLevel. Zero is
	// png.DefaultCompression.
	Compression png.CompressionLevel
}

// PNG selects PNG output.
func (i *Image) PNG(opts ...PNGOptions) *Encoder {
	o := PNGOptions{}
	if len(opts) > 0 {
		o = opts[0]
	}
	// image/png defines CompressionLevel in [-3, 0]; values outside
	// that range are silently treated as DefaultCompression by
	// stdlib, which hides config bugs. Reject up front.
	if o.Compression < -3 || o.Compression > 0 {
		return &Encoder{
			img:    i,
			format: FormatPNG,
			err:    fmt.Errorf("image: PNGOptions.Compression %d out of valid range [-3, 0]", o.Compression),
		}
	}
	enc := png.Encoder{CompressionLevel: o.Compression}
	return &Encoder{
		img:    i,
		format: FormatPNG,
		encode: func(w io.Writer, img stdimage.Image) error { return enc.Encode(w, img) },
	}
}

// GIFOptions configures GIF encoding.
type GIFOptions struct {
	// NumColors is the palette size 1..256. 0 defaults to 256.
	NumColors int
}

// GIF selects GIF output.
func (i *Image) GIF(opts ...GIFOptions) *Encoder {
	o := GIFOptions{}
	if len(opts) > 0 {
		o = opts[0]
	}
	if o.NumColors == 0 {
		o.NumColors = 256
	}
	if o.NumColors < 1 {
		o.NumColors = 1
	}
	if o.NumColors > 256 {
		o.NumColors = 256
	}
	return &Encoder{
		img:    i,
		format: FormatGIF,
		encode: func(w io.Writer, img stdimage.Image) error {
			return gif.Encode(w, img, &gif.Options{NumColors: o.NumColors})
		},
	}
}

// BMP selects BMP output.
func (i *Image) BMP() *Encoder {
	return &Encoder{
		img:    i,
		format: FormatBMP,
		encode: func(w io.Writer, img stdimage.Image) error { return bmp.Encode(w, img) },
	}
}

// TIFFOptions configures TIFF encoding.
type TIFFOptions struct {
	Compression tiff.CompressionType
	Predictor   bool
}

// TIFF selects TIFF output.
func (i *Image) TIFF(opts ...TIFFOptions) *Encoder {
	o := TIFFOptions{}
	if len(opts) > 0 {
		o = opts[0]
	}
	return &Encoder{
		img:    i,
		format: FormatTIFF,
		encode: func(w io.Writer, img stdimage.Image) error {
			return tiff.Encode(w, img, &tiff.Options{Compression: o.Compression, Predictor: o.Predictor})
		},
	}
}

// WebPOptions configures WebP encoding. The zero value selects
// lossless (VP8L), the only mode currently implemented.
type WebPOptions struct {
	// Lossy requests VP8 lossy WebP. Not implemented — setting this
	// to true returns ErrFormatUnsupported on terminal calls. The
	// zero value (false) selects lossless.
	Lossy bool
}

// WebP selects WebP output. The default mode is lossless; setting
// WebPOptions.Lossy = true returns ErrFormatUnsupported.
func (i *Image) WebP(opts ...WebPOptions) *Encoder {
	var o WebPOptions
	if len(opts) > 0 {
		o = opts[0]
	}
	if o.Lossy {
		return &Encoder{
			img:    i,
			format: FormatWebP,
			err:    fmt.Errorf("%w: lossy WebP is not implemented", ErrFormatUnsupported),
		}
	}
	return &Encoder{
		img:    i,
		format: FormatWebP,
		encode: encodeWebPLossless,
	}
}
