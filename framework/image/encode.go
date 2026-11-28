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

// Encoder is a pipeline's output format; its codec runs once, on the
// first of Bytes, Write, Base64 or DataURL.
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

// Write encodes the image to w.
func (e *Encoder) Write(w io.Writer) error {
	if err := e.ensureEncoded(); err != nil {
		return err
	}
	n, err := w.Write(e.cached)
	if err == nil && n != len(e.cached) {
		return io.ErrShortWrite
	}
	return err
}

// Bytes returns the encoded image in a slice of the caller's own.
func (e *Encoder) Bytes() ([]byte, error) {
	if err := e.ensureEncoded(); err != nil {
		return nil, err
	}
	out := make([]byte, len(e.cached))
	copy(out, e.cached)
	return out, nil
}

func (e *Encoder) ensureEncoded() error {
	if e.err != nil {
		return e.err
	}
	e.once.Do(func() {
		var buf bytes.Buffer
		err := e.encode(&buf, e.img.img)
		e.cached = buf.Bytes()
		e.cachedErr = err
	})
	return e.cachedErr
}

// Base64 returns the encoded image as base64.
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
	o.Quality = clampInt(o.Quality, 80, 1, 100)
	return &Encoder{
		img:    i,
		format: FormatJPEG,
		encode: func(w io.Writer, img stdimage.Image) error {
			return jpeg.Encode(w, img, &jpeg.Options{Quality: o.Quality})
		},
	}
}

func clampInt(v, def, lo, hi int) int {
	if v == 0 {
		v = def
	}
	if v < lo {
		v = lo
	}
	if v > hi {
		v = hi
	}
	return v
}

// PNGOptions configures PNG encoding.
type PNGOptions struct {
	// Compression is a png.CompressionLevel, -3..0.
	Compression png.CompressionLevel
}

// PNG selects PNG output.
func (i *Image) PNG(opts ...PNGOptions) *Encoder {
	o := PNGOptions{}
	if len(opts) > 0 {
		o = opts[0]
	}
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
	o.NumColors = clampInt(o.NumColors, 256, 1, 256)
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

// WebPOptions configures WebP encoding. The zero value is lossless.
type WebPOptions struct {
	// Lossy is not implemented and returns ErrFormatUnsupported.
	Lossy bool
}

// WebP selects lossless WebP output.
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
