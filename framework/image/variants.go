package image

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"strconv"
)

// Variant describes one output produced by a VariantSet. Height is
// derived from the source aspect ratio (FitInside semantics).
type Variant struct {
	// Width is the target box width in pixels. Required, must be >= 1.
	Width int

	// Format is the output encoding. Required.
	Format Format

	// Quality applies to JPEG (1..100). 0 uses the format default.
	Quality int

	// Suffix is appended to BaseName when forming the output Name.
	// Empty defaults to the variant width (e.g., "photo-800.jpg").
	Suffix string
}

// VariantSet is a declarative spec for producing N outputs from one
// source image. The same Process call also handles optional LQIP
// (Placeholder) and BlurHash generation, so callers get one typed
// result bag that captures every artefact derived from the source.
//
// VariantSet is intentionally headless: no UI, no HTTP, no storage
// dependency. Pair its output with framework/ui.PipelineImage to
// render or with a battery/storage backend to persist.
type VariantSet struct {
	// Variants is the ordered list of outputs to produce. Empty means
	// "no encoded variants — just the placeholder and/or hash".
	Variants []Variant

	// Placeholder, if non-nil, produces a base64 data: URL LQIP.
	Placeholder *PlaceholderOptions

	// BlurHashX and BlurHashY are the BlurHash component counts (1..9).
	// Both zero means no BlurHash. Setting only one is an error.
	BlurHashX int
	BlurHashY int

	// BaseName is the prefix used for VariantOutput.Name. Default "image".
	BaseName string

	// AllowUpscale opts back in to producing variants larger than the
	// source. The default (false) clamps every variant's effective
	// width to min(Variant.Width, source width). Without the clamp a
	// 16×16 source with Variant{Width: 2048} would silently produce a
	// 2048×2048 pixel-multiplied-garbage output — almost never what a
	// caller wants.
	AllowUpscale bool

	// RejectAnimated returns ErrAnimatedSource when the source's
	// Metadata.FrameCount is > 1 (today: animated GIFs). Use this on
	// upload pipelines where the silent first-frame-flatten behavior
	// of the default decoder would be a surprise — e.g. avatar
	// uploads, profile photos. Default (false) preserves the legacy
	// "first frame wins" behavior.
	RejectAnimated bool
}

// VariantOutput is one fully rendered variant.
type VariantOutput struct {
	// Name is a storage-friendly identifier composed from BaseName,
	// Suffix (or width), and the format extension. Example:
	// "photo-800.jpg".
	Name string

	// Format is the encoding format.
	Format Format

	// Width and Height are the rendered output dimensions.
	Width  int
	Height int

	// Bytes is the encoded image. Callers typically hand this to a
	// storage backend (battery/storage) or to an HTTP response writer.
	Bytes []byte

	// MIME is the canonical Content-Type for Format.
	MIME string
}

// VariantResult is the typed result of Process. Fields are populated
// only when requested by VariantSet — Placeholder is empty when
// VariantSet.Placeholder is nil, BlurHash is empty when both BlurHash
// component counts are zero.
type VariantResult struct {
	// SourceWidth and SourceHeight reflect the input image's bounds at
	// the moment Process was called.
	SourceWidth  int
	SourceHeight int

	// Variants holds one VariantOutput per VariantSet.Variants entry,
	// in the same order.
	Variants []VariantOutput

	// Placeholder is the LQIP data: URL when requested.
	Placeholder string

	// BlurHash is the base83 hash string when requested.
	BlurHash string
}

// Process produces every variant + placeholder + hash declared by the
// set. The first error halts processing — callers wanting per-variant
// resilience can call Process repeatedly with single-element sets.
func (s VariantSet) Process(src *Image) (VariantResult, error) {
	if src == nil {
		return VariantResult{}, fmt.Errorf("image: VariantSet.Process: nil source")
	}
	if (s.BlurHashX == 0) != (s.BlurHashY == 0) {
		return VariantResult{}, fmt.Errorf("image: VariantSet: BlurHashX and BlurHashY must both be set or both zero")
	}
	if s.RejectAnimated && src.frames > 1 {
		return VariantResult{}, ErrAnimatedSource
	}

	baseName := s.BaseName
	if baseName == "" {
		baseName = "image"
	}

	bounds := src.Bounds()
	result := VariantResult{
		SourceWidth:  bounds.Dx(),
		SourceHeight: bounds.Dy(),
	}

	for i, v := range s.Variants {
		if v.Width < 1 {
			return result, fmt.Errorf("image: VariantSet.Variants[%d]: Width must be >= 1", i)
		}
		if v.Format == FormatUnknown {
			return result, fmt.Errorf("image: VariantSet.Variants[%d]: Format must be set", i)
		}

		// Width-only resize already preserves aspect; an extra FitInside
		// pass would re-quantise the height and lose a pixel. Without
		// AllowUpscale, clamp the target width to the source width.
		targetW := v.Width
		if !s.AllowUpscale && targetW > bounds.Dx() {
			targetW = bounds.Dx()
		}
		scaled := src.Resize(targetW, 0)
		enc, err := encodeForFormat(scaled, v.Format, v.Quality)
		if err != nil {
			return result, fmt.Errorf("image: VariantSet.Variants[%d]: %w", i, err)
		}
		data, err := enc.Bytes()
		if err != nil {
			return result, fmt.Errorf("image: VariantSet.Variants[%d]: %w", i, err)
		}
		b := scaled.Bounds()
		result.Variants = append(result.Variants, VariantOutput{
			Name:   variantName(baseName, v),
			Format: v.Format,
			Width:  b.Dx(),
			Height: b.Dy(),
			Bytes:  data,
			MIME:   v.Format.MIME(),
		})
	}

	if s.Placeholder != nil {
		durl, err := src.Placeholder(*s.Placeholder)
		if err != nil {
			return result, fmt.Errorf("image: VariantSet placeholder: %w", err)
		}
		result.Placeholder = durl
	}

	if s.BlurHashX > 0 {
		// Downscale for BlurHash speed; spec is O(W·H·xComp·yComp).
		hashSrc := src.Resize(32, 0, WithFit(FitInside), WithoutEnlargement())
		hash, err := hashSrc.BlurHash(s.BlurHashX, s.BlurHashY)
		if err != nil {
			return result, fmt.Errorf("image: VariantSet blurhash: %w", err)
		}
		result.BlurHash = hash
	}

	return result, nil
}

// VariantHeader is the metadata for one streaming variant, delivered
// to the sink callback alongside an io.Reader carrying the encoded
// bytes. ProcessTo emits one VariantHeader per Variant in input order.
type VariantHeader struct {
	Name   string
	Format Format
	Width  int
	Height int
	MIME   string
}

// StreamResult is the typed return of ProcessTo. Variants are streamed
// via the sink rather than carried here.
type StreamResult struct {
	SourceWidth  int
	SourceHeight int
	Placeholder  string
	BlurHash     string
}

// VariantSink is the callback ProcessTo invokes once per variant.
// Implementations must read r before returning; the framework reuses
// the underlying buffer on the next variant and the reader handed in
// is one-shot — it returns ErrReaderClosed on any access after the
// sink returns. Returning an error stops emission and propagates out
// of ProcessTo.
type VariantSink func(h VariantHeader, r io.Reader) error

// ErrReaderClosed is returned when a sink reads from its VariantHeader
// reader after the sink has returned. The reader is intentionally
// one-shot to surface the "I stashed the reader and read it later"
// foot-gun loudly instead of returning corrupted bytes from a later
// variant.
var ErrReaderClosed = errors.New("image: VariantSink reader used after sink returned")

// oneShotReader wraps an io.Reader so reads after Close return
// ErrReaderClosed. ProcessTo closes the reader as soon as the sink
// returns, regardless of whether the sink drained the buffer.
type oneShotReader struct {
	r      io.Reader
	closed bool
}

func (o *oneShotReader) Read(p []byte) (int, error) {
	if o.closed {
		return 0, ErrReaderClosed
	}
	return o.r.Read(p)
}

func (o *oneShotReader) close() { o.closed = true }

// ProcessTo is the streaming variant of Process. Only one variant
// lives in memory at a time — once the sink returns, the buffer is
// released and the next variant is encoded. Wire the sink directly
// to a storage backend (e.g., core/upload.Storage.Save) to avoid
// holding all variants resident.
func (s VariantSet) ProcessTo(src *Image, sink VariantSink) (StreamResult, error) {
	if src == nil {
		return StreamResult{}, fmt.Errorf("image: VariantSet.ProcessTo: nil source")
	}
	if sink == nil {
		return StreamResult{}, fmt.Errorf("image: VariantSet.ProcessTo: nil sink")
	}
	if (s.BlurHashX == 0) != (s.BlurHashY == 0) {
		return StreamResult{}, fmt.Errorf("image: VariantSet: BlurHashX and BlurHashY must both be set or both zero")
	}
	if s.RejectAnimated && src.frames > 1 {
		return StreamResult{}, ErrAnimatedSource
	}

	baseName := s.BaseName
	if baseName == "" {
		baseName = "image"
	}

	bounds := src.Bounds()
	result := StreamResult{
		SourceWidth:  bounds.Dx(),
		SourceHeight: bounds.Dy(),
	}

	var buf bytes.Buffer
	for i, v := range s.Variants {
		if v.Width < 1 {
			return result, fmt.Errorf("image: VariantSet.Variants[%d]: Width must be >= 1", i)
		}
		if v.Format == FormatUnknown {
			return result, fmt.Errorf("image: VariantSet.Variants[%d]: Format must be set", i)
		}
		targetW := v.Width
		if !s.AllowUpscale && targetW > bounds.Dx() {
			targetW = bounds.Dx()
		}
		scaled := src.Resize(targetW, 0)
		enc, err := encodeForFormat(scaled, v.Format, v.Quality)
		if err != nil {
			return result, fmt.Errorf("image: VariantSet.Variants[%d]: %w", i, err)
		}
		buf.Reset()
		if err := enc.Write(&buf); err != nil {
			return result, fmt.Errorf("image: VariantSet.Variants[%d]: %w", i, err)
		}
		b := scaled.Bounds()
		header := VariantHeader{
			Name:   variantName(baseName, v),
			Format: v.Format,
			Width:  b.Dx(),
			Height: b.Dy(),
			MIME:   v.Format.MIME(),
		}
		guarded := &oneShotReader{r: &buf}
		serr := sink(header, guarded)
		guarded.close()
		// Drop the scaled intermediate so the GC can reclaim the
		// resize-output buffer before we allocate the next one. Without
		// this, each iteration retains a fresh image.RGBA and the
		// peak heap grows with variant count — the doc's "only one
		// variant lives in memory at a time" promise breaks.
		scaled = nil
		enc = nil
		if serr != nil {
			return result, serr
		}
	}

	if s.Placeholder != nil {
		durl, err := src.Placeholder(*s.Placeholder)
		if err != nil {
			return result, fmt.Errorf("image: VariantSet placeholder: %w", err)
		}
		result.Placeholder = durl
	}
	if s.BlurHashX > 0 {
		hashSrc := src.Resize(32, 0, WithFit(FitInside), WithoutEnlargement())
		hash, err := hashSrc.BlurHash(s.BlurHashX, s.BlurHashY)
		if err != nil {
			return result, fmt.Errorf("image: VariantSet blurhash: %w", err)
		}
		result.BlurHash = hash
	}
	return result, nil
}

// variantName composes a storage-friendly file name for a variant. If
// Suffix is set, it's used verbatim; otherwise the width is the suffix.
// Format extension is always appended.
func variantName(base string, v Variant) string {
	suffix := v.Suffix
	if suffix == "" {
		suffix = strconv.Itoa(v.Width)
	}
	return base + "-" + suffix + "." + variantExt(v.Format)
}

func variantExt(f Format) string {
	switch f {
	case FormatJPEG:
		return "jpg"
	case FormatPNG:
		return "png"
	case FormatGIF:
		return "gif"
	case FormatBMP:
		return "bmp"
	case FormatTIFF:
		return "tiff"
	case FormatWebP:
		return "webp"
	}
	return "bin"
}

// encodeForFormat returns the Encoder for the given Format + Quality.
// Quality is honoured for JPEG; ignored for formats that don't expose a
// quality knob in their option struct.
func encodeForFormat(img *Image, f Format, quality int) (*Encoder, error) {
	switch f {
	case FormatJPEG:
		return img.JPEG(JPEGOptions{Quality: quality}), nil
	case FormatPNG:
		return img.PNG(), nil
	case FormatGIF:
		return img.GIF(), nil
	case FormatBMP:
		return img.BMP(), nil
	case FormatTIFF:
		return img.TIFF(), nil
	case FormatWebP:
		// Default to lossless; lossy WebP is not supported.
		return img.WebP(), nil
	}
	return nil, fmt.Errorf("unsupported format %q", f)
}
