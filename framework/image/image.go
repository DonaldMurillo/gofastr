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
	"path/filepath"
	"strings"
)

// DefaultMaxPixels caps decoded image area at 64 MP — an 8192×8192
// square. Tightened from Bun.Image's 268 MP default after round-4
// found a 45-byte PNG declaring 16383×16383 (~268 M pixels) passed
// the guard and triggered ~1 GiB of stdimage allocation. Callers
// who need bigger inputs configure Config.MaxPixels explicitly.
const DefaultMaxPixels int64 = 64 * 1024 * 1024

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
	frames int // animated frame count; 0 or 1 = still image
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

// Open reads an image from a filesystem path. Rejects paths containing
// ".." segments as a defense-in-depth measure — callers handling
// user-supplied paths must validate before this layer, but the
// framework boundary catches the obvious traversal patterns rather
// than blindly handing them to os.ReadFile.
//
// For paths rooted in an embed.FS or a constrained directory tree,
// prefer OpenFS — fs.FS implementations reject traversal natively.
func Open(path string) (*Image, error) {
	if pathTraverses(path) {
		return nil, fmt.Errorf("image: open %q: path contains traversal (..)", path)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("image: open %q: %w", path, err)
	}
	return decodeBytes(data, Config{})
}

// pathTraverses returns true if path contains a ".." segment after
// cleaning. Cleaning collapses redundant separators and resolves
// in-place "..", so a Cleaned path with ".." in any segment is
// trying to escape its starting directory.
func pathTraverses(p string) bool {
	cleaned := filepath.Clean(p)
	for _, segment := range strings.Split(cleaned, string(filepath.Separator)) {
		if segment == ".." {
			return true
		}
	}
	return false
}

// OpenFS reads an image from an fs.FS.
func OpenFS(fsys fs.FS, name string) (*Image, error) {
	data, err := fs.ReadFile(fsys, name)
	if err != nil {
		return nil, fmt.Errorf("image: open %q: %w", name, err)
	}
	return decodeBytes(data, Config{})
}

// FromImage wraps an existing image.Image without decoding. Use when
// you already have pixel data (e.g., generated, not loaded from a
// file).
//
// The returned *Image carries Orientation = 0 and FrameCount = 0;
// AutoOrient() is a no-op against it. If you need EXIF orientation
// handling or animated-source detection, route the bytes through
// Decode / Open / OpenFS instead.
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
	frames := 0
	if format == FormatGIF {
		frames = countGIFFrames(data)
	}
	return &Image{img: img, format: format, orient: orient, frames: frames, cfg: cfg}, nil
}

// countGIFFrames returns the frame count of a GIF byte stream without
// materialising any pixel buffer. It walks the GIF block structure
// (Image Descriptors, Extensions, sub-block chains) and counts 0x2C
// markers. The previous gif.DecodeAll approach allocated O(frames ×
// pixels) — a 1024×1024×1000-frame GIF cost ~4 GB of heap before
// returning. The byte-walker is O(bytes) heap-free.
func countGIFFrames(data []byte) int {
	const (
		blockImageDescriptor = 0x2C
		blockExtension       = 0x21
		blockTrailer         = 0x3B
	)
	if len(data) < 13 || !bytes.HasPrefix(data, []byte("GIF")) {
		return 1
	}
	// Logical Screen Descriptor's packed byte at offset 10.
	p := 13
	if data[10]&0x80 != 0 {
		// Global Color Table: 3 bytes × 2^(N+1) entries.
		p += 3 << ((data[10] & 0x07) + 1)
	}
	frames := 0
	for p < len(data) {
		switch data[p] {
		case blockImageDescriptor:
			frames++
			if p+10 > len(data) {
				return maxFrameCount(frames, 1)
			}
			lcPacked := data[p+9]
			p += 10
			if lcPacked&0x80 != 0 {
				p += 3 << ((lcPacked & 0x07) + 1)
			}
			if p >= len(data) {
				return maxFrameCount(frames, 1)
			}
			p++ // LZW minimum code size
			p = skipSubBlocks(data, p)
		case blockExtension:
			if p+2 >= len(data) {
				return maxFrameCount(frames, 1)
			}
			p += 2 // marker + label
			p = skipSubBlocks(data, p)
		case blockTrailer:
			return maxFrameCount(frames, 1)
		default:
			return maxFrameCount(frames, 1)
		}
	}
	return maxFrameCount(frames, 1)
}

// skipSubBlocks advances p past a chain of GIF sub-blocks (length byte
// then payload, terminated by a zero length byte). Returns the index
// just past the terminator, or len(data) on truncation.
func skipSubBlocks(data []byte, p int) int {
	for p < len(data) {
		n := int(data[p])
		p++
		if n == 0 {
			return p
		}
		p += n
	}
	return p
}

func maxFrameCount(a, b int) int {
	if a > b {
		return a
	}
	return b
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
// Every metadata field that callers can observe via Metadata() must be
// carried forward — historically `frames` was dropped here, which
// silently neutralised VariantSet{RejectAnimated: true} after any chain
// step (including the documented avatar recipe's AutoOrient).
func (i *Image) derive(img stdimage.Image) *Image {
	return &Image{
		img:    img,
		format: i.format,
		orient: i.orient,
		frames: i.frames,
		cfg:    i.cfg,
	}
}
