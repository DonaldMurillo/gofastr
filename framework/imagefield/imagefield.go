// Package imagefield adapts framework/image to file.ImageDeriver.
package imagefield

import (
	"context"
	"fmt"
	"io"
	"path"
	"sort"
	"strings"

	"github.com/DonaldMurillo/gofastr/core/upload"
	"github.com/DonaldMurillo/gofastr/framework/file"
	fwimage "github.com/DonaldMurillo/gofastr/framework/image"
)

// Config declares what to derive from each uploaded image.
type Config struct {
	// Variants are the renditions stored beside the original.
	Variants []fwimage.Variant

	// BlurHashX and BlurHashY are the BlurHash components, both 0 for none.
	BlurHashX int
	BlurHashY int

	// Placeholder, when non-nil, also stores a data URL placeholder.
	Placeholder *fwimage.PlaceholderOptions

	// RejectAnimated fails an upload of more than one frame.
	RejectAnimated bool

	// AllowUpscale allows renditions wider than the source.
	AllowUpscale bool

	// MaxPixels overrides framework/image's decode pixel cap.
	MaxPixels int64
}

// Deriver implements file.ImageDeriver over a VariantSet.
type Deriver struct {
	set fwimage.VariantSet
	cfg fwimage.Config
}

var _ file.ImageDeriver = (*Deriver)(nil)

// New builds a Deriver, erroring on a config that cannot derive anything.
func New(cfg Config) (*Deriver, error) {
	if (cfg.BlurHashX == 0) != (cfg.BlurHashY == 0) {
		return nil, fmt.Errorf("imagefield: BlurHashX and BlurHashY must both be set or both zero")
	}
	if cfg.BlurHashX < 0 || cfg.BlurHashX > 9 || cfg.BlurHashY < 0 || cfg.BlurHashY > 9 {
		return nil, fmt.Errorf("imagefield: BlurHash components must be in 1..9, got %dx%d",
			cfg.BlurHashX, cfg.BlurHashY)
	}
	if len(cfg.Variants) == 0 && cfg.BlurHashX == 0 && cfg.Placeholder == nil {
		return nil, fmt.Errorf("imagefield: Config derives nothing: set Variants, BlurHashX/Y, or Placeholder")
	}
	if err := fwimage.CheckVariants(cfg.Variants); err != nil {
		return nil, fmt.Errorf("imagefield: %w", err)
	}
	return &Deriver{
		set: fwimage.VariantSet{
			Variants:       cfg.Variants,
			Placeholder:    cfg.Placeholder,
			BlurHashX:      cfg.BlurHashX,
			BlurHashY:      cfg.BlurHashY,
			RejectAnimated: cfg.RejectAnimated,
			AllowUpscale:   cfg.AllowUpscale,
		},
		cfg: fwimage.Config{MaxPixels: cfg.MaxPixels},
	}, nil
}

// MustNew is New for package-level wiring, panicking on a bad config.
func MustNew(cfg Config) *Deriver {
	d, err := New(cfg)
	if err != nil {
		panic(err)
	}
	return d
}

// DeriveImage stores the renditions beside primaryRef and returns them.
func (d *Deriver) DeriveImage(ctx context.Context, store upload.Storage, data []byte, primaryRef string) (*file.ImageDerivatives, error) {
	if store == nil {
		return nil, fmt.Errorf("imagefield: storage backend is required")
	}
	src, err := fwimage.DecodeBytesWithConfig(data, d.cfg)
	if err != nil {
		return nil, fmt.Errorf("imagefield: decoding upload: %w", err)
	}
	src = src.AutoOrient()

	base := d.set
	base.BaseName = baseNameFor(primaryRef)

	out := &file.ImageDerivatives{}
	dir := path.Dir(primaryRef)
	sr, err := base.ProcessTo(src, func(h fwimage.VariantHeader, r io.Reader) error {
		key := path.Join(dir, h.Name)
		if err := store.Save(ctx, key, r); err != nil {
			return fmt.Errorf("saving rendition %q: %w", h.Name, err)
		}
		out.Variants = append(out.Variants, file.DerivedVariant{
			StorageRef: key,
			MIME:       h.MIME,
			Width:      h.Width,
			Height:     h.Height,
		})
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("imagefield: %w", err)
	}
	out.BlurHash = sr.BlurHash
	out.Placeholder = sr.Placeholder

	sort.Slice(out.Variants, func(i, j int) bool { return out.Variants[i].Width < out.Variants[j].Width })
	return out, nil
}

func baseNameFor(primaryRef string) string {
	b := path.Base(primaryRef)
	if b == "." || b == "/" || b == "" {
		return "image"
	}
	if ext := path.Ext(b); ext != "" {
		b = strings.TrimSuffix(b, ext)
	}
	if b == "" {
		return "image"
	}
	return b
}
