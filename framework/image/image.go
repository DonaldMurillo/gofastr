// Package image is a pure-Go chainable pipeline: Decode, transform,
// encode, and derive placeholders and renditions (VariantSet).
package image

import (
	"bytes"
	"fmt"
	stdimage "image"
	"io"
	"io/fs"
	"math"
	"os"
	"path/filepath"
	"strings"

	"github.com/DonaldMurillo/gofastr/framework/internal/jpegcost"
)

// DefaultMaxPixels caps decoded image area at 64 MP.
const DefaultMaxPixels int64 = 64 * 1024 * 1024

// DefaultMaxSourceBytes caps the source bytes decode buffers, 64 MiB.
const DefaultMaxSourceBytes int64 = 64 << 20

// DefaultMaxJPEGScans caps the scans a JPEG may carry.
const DefaultMaxJPEGScans = 64

// DefaultMaxJPEGScanPixels is the default JPEG scans-times-pixels budget.
const DefaultMaxJPEGScanPixels = jpegScanWalksAtPixelCap * DefaultMaxPixels

const jpegScanWalksAtPixelCap = 10

// Config holds knobs that propagate through a pipeline.
type Config struct {
	// MaxPixels caps decoded and resized area; 0 means DefaultMaxPixels.
	MaxPixels int64

	// MaxSourceBytes caps the bytes read from the source; 0 means
	// DefaultMaxSourceBytes.
	MaxSourceBytes int64

	// MaxJPEGScans caps a JPEG's scans; 0 means DefaultMaxJPEGScans.
	MaxJPEGScans int

	// MaxJPEGScanPixels caps a JPEG's scans times pixels; 0 means ten
	// times MaxPixels.
	MaxJPEGScanPixels int64

	// MaxProgressiveJPEGBytes caps a progressive JPEG's coefficient
	// buffer; 0 means DefaultMaxProgressiveJPEGBytes.
	MaxProgressiveJPEGBytes int64
}

// DefaultMaxProgressiveJPEGBytes caps a progressive JPEG's coefficients.
const DefaultMaxProgressiveJPEGBytes int64 = 512 << 20

// Image is the pipeline value; every transformation returns a new one.
type Image struct {
	img    stdimage.Image
	format Format
	orient int
	frames int
	cfg    Config
}

// Decode reads an image from r. The format is detected automatically.
func Decode(r io.Reader) (*Image, error) {
	return DecodeWithConfig(r, Config{})
}

// DecodeWithConfig is Decode with a non-default Config.
func DecodeWithConfig(r io.Reader, cfg Config) (*Image, error) {
	limit := cfg.MaxSourceBytes
	if limit <= 0 {
		limit = DefaultMaxSourceBytes
	}
	sentinel := limit
	if limit < math.MaxInt64 {
		sentinel = limit + 1
	}
	data, err := io.ReadAll(io.LimitReader(r, sentinel))
	if err != nil {
		return nil, fmt.Errorf("image: read source: %w", err)
	}
	if int64(len(data)) > limit {
		return nil, ErrSourceTooLarge
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

// Open reads an image from a path, refusing ".." segments that remain after filepath.Clean.
// Use OpenFS with os.Root.FS for containment within a directory.
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

// FromImage wraps an image.Image, with no orientation or frame count.
func FromImage(img stdimage.Image, format Format) *Image {
	return &Image{img: img, format: format}
}

func decodeBytes(data []byte, cfg Config) (out *Image, err error) {
	if cfg.MaxPixels <= 0 {
		cfg.MaxPixels = DefaultMaxPixels
	}
	format := Sniff(data)
	if format == FormatUnknown {
		return nil, ErrInvalidInput
	}
	defer func() {
		if r := recover(); r != nil {
			out = nil
			err = fmt.Errorf("image: decode panicked: %v: %w", r, ErrInvalidInput)
		}
	}()
	icfg, _, err := stdimage.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("image: decode config: %w", err)
	}
	if icfg.Width <= 0 || icfg.Height <= 0 {
		return nil, ErrInvalidInput
	}
	if int64(icfg.Width) > cfg.MaxPixels/int64(icfg.Height) {
		return nil, ErrDecompressionBomb
	}
	if format == FormatJPEG {
		maxScans := cfg.MaxJPEGScans
		if maxScans <= 0 {
			maxScans = DefaultMaxJPEGScans
		}
		scans := countJPEGScans(data)
		if scans > maxScans {
			return nil, fmt.Errorf("%w: JPEG carries %d scans (max %d)", ErrDecompressionBomb, scans, maxScans)
		}
		pixels := int64(icfg.Width) * int64(icfg.Height)
		if budget := jpegScanBudget(cfg); int64(scans) > budget/pixels {
			return nil, fmt.Errorf("%w: JPEG carries %d scans of %d pixels (budget %d pixel-scans)", ErrDecompressionBomb, scans, pixels, budget)
		}
		maxCoeff := cfg.MaxProgressiveJPEGBytes
		if maxCoeff <= 0 {
			maxCoeff = DefaultMaxProgressiveJPEGBytes
		}
		n, ok := jpegcost.ProgressiveCoeffBytes(data)
		if !ok {
			return nil, fmt.Errorf("%w: cannot estimate JPEG coefficient memory", ErrDecompressionBomb)
		}
		if n > maxCoeff {
			return nil, fmt.Errorf("%w: progressive JPEG needs %d bytes of coefficients (max %d)", ErrDecompressionBomb, n, maxCoeff)
		}
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

func jpegScanBudget(cfg Config) int64 {
	if cfg.MaxJPEGScanPixels > 0 {
		return cfg.MaxJPEGScanPixels
	}
	if cfg.MaxPixels > math.MaxInt64/jpegScanWalksAtPixelCap {
		return math.MaxInt64
	}
	return jpegScanWalksAtPixelCap * cfg.MaxPixels
}

func countJPEGScans(d []byte) int {
	n := 0
	for i := 2; i+1 < len(d); {
		if d[i] != 0xFF {
			i++
			continue
		}
		m := d[i+1]
		switch {
		case m == 0xFF:
			i++
			continue
		case m == 0x00 || m == 0x01 || m == 0xD8 || (m >= 0xD0 && m <= 0xD7):
			i += 2
			continue
		case m == 0xD9:
			return n
		}
		if m == 0xDA {
			n++
		}
		if i+3 >= len(d) {
			return n
		}
		i += 2 + (int(d[i+2])<<8 | int(d[i+3]))
	}
	return n
}

func countGIFFrames(data []byte) int {
	const (
		blockImageDescriptor = 0x2C
		blockExtension       = 0x21
		blockTrailer         = 0x3B
	)
	if len(data) < 13 || !bytes.HasPrefix(data, []byte("GIF")) {
		return 1
	}
	p := 13
	if data[10]&0x80 != 0 {
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
			p++
			p = skipSubBlocks(data, p)
		case blockExtension:
			if p+2 >= len(data) {
				return maxFrameCount(frames, 1)
			}
			p += 2
			p = skipSubBlocks(data, p)
		case blockTrailer:
			return maxFrameCount(frames, 1)
		default:
			return maxFrameCount(frames, 1)
		}
	}
	return maxFrameCount(frames, 1)
}

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

// GoImage returns the underlying image.Image.
func (i *Image) GoImage() stdimage.Image { return i.img }

// Format returns the source format.
func (i *Image) Format() Format { return i.format }

// Bounds returns the current image bounds.
func (i *Image) Bounds() stdimage.Rectangle { return i.img.Bounds() }

func (i *Image) derive(img stdimage.Image) *Image {
	return &Image{
		img:    img,
		format: i.format,
		orient: i.orient,
		frames: i.frames,
		cfg:    i.cfg,
	}
}
