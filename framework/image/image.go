// Package image is a chainable image pipeline: decode → transform → encode,
// pure Go with only the standard library and golang.org/x/image as
// dependencies. The API is shaped after Bun.Image; the implementation is
// independent.
//
// Construction:
//
//	img, err := image.Decode(r)
//	img, err := image.DecodeBytes(data)
//	img, err := image.Open("path/to/file.jpg")
//	img, err := image.OpenFS(fsys, "name")
//
// Chain (each method returns a new *Image):
//
//	img.Resize(800, 600, image.Lanczos3).
//	    AutoOrient().
//	    Modulate(image.Modulation{Brightness: image.Float64(1.1)})
//
// Encode (terminal):
//
//	data, err := img.JPEG(image.JPEGOptions{Quality: 80}).Bytes()
//	err = img.PNG().Write(w)
//	durl, err := img.WebP().DataURL() // zero-value = lossless
//
// Placeholders:
//
//	durl, err := img.Placeholder()           // base64-encoded tiny JPEG
//	hash, err := img.BlurHash(4, 3)          // BlurHash string
package image

import (
	"bytes"
	"fmt"
	stdimage "image"
	"io"
	"io/fs"
	"os"
)

// DefaultMaxPixels matches Bun.Image's default decompression-bomb guard.
const DefaultMaxPixels int64 = 268_435_456

// Config holds knobs that propagate through a pipeline.
type Config struct {
	// MaxPixels caps decoded image area. Inputs whose reported width*height
	// exceed this return ErrDecompressionBomb at decode time. Zero means
	// DefaultMaxPixels.
	MaxPixels int64
}

// Image is the chainable pipeline value. Transformations return a new
// *Image, so the same source may be branched into independent pipelines.
type Image struct {
	img    stdimage.Image
	format Format
	orient int // EXIF orientation tag (1..8); 0 means unknown / not applicable
	cfg    Config
}

// Decode reads an image from r. The format is detected automatically.
func Decode(r io.Reader) (*Image, error) {
	return DecodeWithConfig(r, Config{})
}

// DecodeWithConfig is Decode with a non-default Config.
func DecodeWithConfig(r io.Reader, cfg Config) (*Image, error) {
	data, err := io.ReadAll(r)
	if err != nil {
		return nil, fmt.Errorf("image: read source: %w", err)
	}
	return decodeBytes(data, cfg)
}

// DecodeBytes decodes an in-memory image buffer.
func DecodeBytes(data []byte) (*Image, error) {
	return decodeBytes(data, Config{})
}

// DecodeBytesWithConfig is DecodeBytes with a non-default Config.
func DecodeBytesWithConfig(data []byte, cfg Config) (*Image, error) {
	return decodeBytes(data, cfg)
}

// Open reads an image from a filesystem path.
func Open(path string) (*Image, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("image: open %q: %w", path, err)
	}
	return decodeBytes(data, Config{})
}

// OpenFS reads an image from an fs.FS.
func OpenFS(fsys fs.FS, name string) (*Image, error) {
	data, err := fs.ReadFile(fsys, name)
	if err != nil {
		return nil, fmt.Errorf("image: open %q: %w", name, err)
	}
	return decodeBytes(data, Config{})
}

// FromImage wraps an existing image.Image without decoding. Use when you
// already have pixel data (e.g., generated, not loaded from a file).
func FromImage(img stdimage.Image, format Format) *Image {
	return &Image{img: img, format: format}
}

func decodeBytes(data []byte, cfg Config) (*Image, error) {
	if cfg.MaxPixels <= 0 {
		cfg.MaxPixels = DefaultMaxPixels
	}
	format := Sniff(data)
	if format == FormatUnknown {
		return nil, ErrInvalidInput
	}
	icfg, _, err := stdimage.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("image: decode config: %w", err)
	}
	if int64(icfg.Width)*int64(icfg.Height) > cfg.MaxPixels {
		return nil, ErrDecompressionBomb
	}
	img, _, err := stdimage.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("image: decode: %w", err)
	}
	orient := 0
	if format == FormatJPEG {
		orient = readJPEGOrientation(data)
	}
	return &Image{img: img, format: format, orient: orient, cfg: cfg}, nil
}

// GoImage returns the underlying image.Image. Useful for callers that need
// to pass the pixels into another library or do custom drawing.
func (i *Image) GoImage() stdimage.Image { return i.img }

// Format returns the source format. May be FormatUnknown when the Image
// was constructed via FromImage.
func (i *Image) Format() Format { return i.format }

// Bounds returns the current image bounds.
func (i *Image) Bounds() stdimage.Rectangle { return i.img.Bounds() }

// derive returns a new Image with the same metadata but a fresh underlying
// image. Used by chain methods so transformations don't mutate the source.
func (i *Image) derive(img stdimage.Image) *Image {
	return &Image{img: img, format: i.format, orient: i.orient, cfg: i.cfg}
}
