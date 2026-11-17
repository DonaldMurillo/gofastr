package image

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"strconv"
	"unicode/utf8"
)

// Variant describes one output of a VariantSet.
type Variant struct {
	// Width is the target width in pixels; the height keeps the aspect.
	Width int
	// Format is the output encoding.
	Format Format
	// Quality is the JPEG quality, 0 for the default.
	Quality int
	// Suffix names the output, the width when empty.
	Suffix string
}

// VariantSet declares the renditions, placeholder and BlurHash of a source.
type VariantSet struct {
	// Variants are the outputs, in order.
	Variants []Variant
	// Placeholder, if non-nil, adds a data: URL placeholder.
	Placeholder *PlaceholderOptions
	// BlurHashX and BlurHashY are the BlurHash components, both 0 for none.
	BlurHashX int
	BlurHashY int
	// BaseName prefixes every output Name, "image" when empty.
	BaseName string
	// AllowUpscale lets a variant be wider than the source.
	AllowUpscale bool
	// RejectAnimated refuses a source of more than one frame.
	RejectAnimated bool
}

// MaxVariantNameBytes caps a rendition's file name.
const MaxVariantNameBytes = 255

// MinVariantBaseBytes is the room a variant's suffix must leave for the base.
const MinVariantBaseBytes = 64

// MaxVariantsPerSet caps the length of VariantSet.Variants.
const MaxVariantsPerSet = 64

// VariantOutput is one fully rendered variant.
type VariantOutput struct {
	// Name is <BaseName>-<Suffix>.<ext>, as in "photo-800.jpg".
	Name string
	// Format is the encoding format.
	Format Format
	// Width and Height are the rendered dimensions.
	Width  int
	Height int
	// Bytes is the encoded image.
	Bytes []byte
	// MIME is the Content-Type for Format.
	MIME string
}

// VariantResult is the result of Process.
type VariantResult struct {
	// SourceWidth and SourceHeight are the source's dimensions.
	SourceWidth  int
	SourceHeight int
	// Variants holds one output per VariantSet.Variants entry, in order.
	Variants []VariantOutput
	// Placeholder is the data: URL placeholder, when requested.
	Placeholder string
	// BlurHash is the hash, when requested.
	BlurHash string
}

// Process produces everything the set declares, in memory.
func (s VariantSet) Process(src *Image) (VariantResult, error) {
	var outs []VariantOutput
	sr, err := s.run(src, "Process", func(h VariantHeader, buf *bytes.Buffer) error {
		outs = append(outs, VariantOutput{
			Name:   h.Name,
			Format: h.Format,
			Width:  h.Width,
			Height: h.Height,
			Bytes:  bytes.Clone(buf.Bytes()),
			MIME:   h.MIME,
		})
		return nil
	})
	return VariantResult{
		SourceWidth:  sr.SourceWidth,
		SourceHeight: sr.SourceHeight,
		Variants:     outs,
		Placeholder:  sr.Placeholder,
		BlurHash:     sr.BlurHash,
	}, err
}

// VariantHeader describes the variant ProcessTo hands its sink.
type VariantHeader struct {
	Name   string
	Format Format
	Width  int
	Height int
	MIME   string
}

// StreamResult is the result of ProcessTo.
type StreamResult struct {
	SourceWidth  int
	SourceHeight int
	Placeholder  string
	BlurHash     string
}

// VariantSink receives each variant; r fails with ErrReaderClosed once
// the sink returns.
type VariantSink func(h VariantHeader, r io.Reader) error

// ErrReaderClosed is returned by a sink's reader after the sink returns.
var ErrReaderClosed = errors.New("image: VariantSink reader used after sink returned")

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

func (o *oneShotReader) WriteTo(w io.Writer) (int64, error) {
	if o.closed {
		return 0, ErrReaderClosed
	}
	if wt, ok := o.r.(io.WriterTo); ok {
		return wt.WriteTo(w)
	}
	return io.Copy(w, o.r)
}

func (o *oneShotReader) close() { o.closed = true }

// ProcessTo is Process streaming each variant to sink.
func (s VariantSet) ProcessTo(src *Image, sink VariantSink) (StreamResult, error) {
	if src != nil && sink == nil {
		return StreamResult{}, fmt.Errorf("image: VariantSet.ProcessTo: nil sink")
	}
	return s.run(src, "ProcessTo", func(h VariantHeader, buf *bytes.Buffer) error {
		guarded := &oneShotReader{r: buf}
		err := sink(h, guarded)
		guarded.close()
		return err
	})
}

func (s VariantSet) run(src *Image, op string, emit func(VariantHeader, *bytes.Buffer) error) (StreamResult, error) {
	if src == nil {
		return StreamResult{}, fmt.Errorf("image: VariantSet.%s: nil source", op)
	}
	if len(s.Variants) > MaxVariantsPerSet {
		return StreamResult{}, fmt.Errorf("image: VariantSet: too many variants (%d > MaxVariantsPerSet=%d)", len(s.Variants), MaxVariantsPerSet)
	}
	if (s.BlurHashX == 0) != (s.BlurHashY == 0) {
		return StreamResult{}, fmt.Errorf("image: VariantSet: BlurHashX and BlurHashY must both be set or both zero")
	}
	if s.RejectAnimated && src.frames > 1 {
		return StreamResult{}, ErrAnimatedSource
	}
	if err := s.checkUpFront(src); err != nil {
		return StreamResult{}, err
	}

	baseName := fitBaseName(s.BaseName, s.Variants)

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
		scaled := src.Resize(s.targetWidth(v, bounds.Dx()), 0)
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
		if err := emit(header, &buf); err != nil {
			return result, err
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
		hash, err := src.BlurHash(s.BlurHashX, s.BlurHashY)
		if err != nil {
			return result, fmt.Errorf("image: VariantSet blurhash: %w", err)
		}
		result.BlurHash = hash
	}
	return result, nil
}

func (s VariantSet) targetWidth(v Variant, srcW int) int {
	if !s.AllowUpscale && v.Width > srcW {
		return srcW
	}
	return v.Width
}

func (s VariantSet) checkUpFront(src *Image) error {
	if err := CheckVariantNames(s.Variants); err != nil {
		return err
	}
	w := src.Bounds().Dx()
	for i, v := range s.Variants {
		if err := src.checkResize(s.targetWidth(v, w), 0); err != nil {
			return fmt.Errorf("image: VariantSet.Variants[%d]: %w", i, err)
		}
	}
	return nil
}

// CheckVariantNames reports colliding or overlong variant names.
func CheckVariantNames(vs []Variant) error {
	seen := make(map[string]int, len(vs))
	for i, v := range vs {
		name := variantName("", v)
		if len(name) > MaxVariantNameBytes-MinVariantBaseBytes {
			return fmt.Errorf("image: VariantSet.Variants[%d] names %d bytes before any base, leaving under MinVariantBaseBytes=%d of MaxVariantNameBytes=%d for the base", i, len(name), MinVariantBaseBytes, MaxVariantNameBytes)
		}
		if j, dup := seen[name]; dup {
			return fmt.Errorf("image: VariantSet.Variants[%d] and Variants[%d] both name %q", j, i, "<base>"+name)
		}
		seen[name] = i
	}
	return nil
}

func fitBaseName(base string, vs []Variant) string {
	if base == "" {
		base = "image"
	}
	longest := 0
	for _, v := range vs {
		longest = max(longest, len(variantName("", v)))
	}
	budget := MaxVariantNameBytes - longest
	if budget <= 0 || len(base) <= budget {
		return base
	}
	cut := len(base) - budget
	for cut < len(base) && !utf8.RuneStart(base[cut]) {
		cut++
	}
	if cut == len(base) {
		return "image"[:min(budget, len("image"))]
	}
	return base[cut:]
}

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
		return img.WebP(), nil
	}
	return nil, fmt.Errorf("unsupported format %q", f)
}
