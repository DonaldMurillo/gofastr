// Package imagefield connects the image pipeline to the upload path: it
// turns a framework/image.VariantSet into the file.ImageDeriver that
// ProcessFileField and the CRUD upload handler call, so declaring a
// schema.Image field is what makes uploads produce renditions and a
// BlurHash — no per-entity upload handler.
//
// It is a separate package on purpose. framework/file is a leaf that
// framework/crud imports, so an edge from there to framework/image would
// link every image decoder plus the WebP encoder into every application
// with a CRUD handler. Keeping the adapter here means only applications
// that actually want the pipeline pay for it.
//
// Typical wiring — one option on the app:
//
//	framework.NewApp(
//	    framework.WithFileStorage(store),
//	    framework.WithImagePipeline(imagefield.MustNew(imagefield.Config{
//	        Variants: []image.Variant{
//	            {Width: 480, Format: image.FormatWebP, Suffix: "sm"},
//	            {Width: 960, Format: image.FormatWebP, Suffix: "md"},
//	            {Width: 480, Format: image.FormatJPEG, Quality: 82, Suffix: "sm"},
//	            {Width: 960, Format: image.FormatJPEG, Quality: 82, Suffix: "md"},
//	        },
//	        BlurHashX: 4, BlurHashY: 3,
//	    })),
//	)
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

// Config declares what to derive from each uploaded image. The zero value
// derives nothing and New rejects it — an image pipeline that produces no
// renditions, no hash, and no placeholder is a configuration mistake, not
// a no-op worth honouring silently.
type Config struct {
	// Variants are the renditions to produce and store. Each entry's
	// Suffix (or width, when Suffix is empty) distinguishes its storage
	// key from the original's.
	Variants []fwimage.Variant

	// BlurHashX and BlurHashY are the BlurHash component counts (1..9).
	// Both zero means no BlurHash; setting only one is an error. 4x3 suits
	// landscape images, 3x4 portrait.
	BlurHashX int
	BlurHashY int

	// Placeholder, when non-nil, also stores an LQIP data URL. Redundant
	// alongside a BlurHash for most callers — a BlurHash costs ~28 bytes
	// in the column against a few hundred, and framework/image renders
	// either one the same way.
	Placeholder *fwimage.PlaceholderOptions

	// RejectAnimated fails the upload when the source has more than one
	// frame instead of silently flattening to the first. Worth setting on
	// avatar and profile-photo fields, where a surprise still frame is
	// worse than a rejection the user can act on.
	RejectAnimated bool

	// AllowUpscale opts back in to renditions wider than the source. The
	// default clamps each rendition to the source width, so a small upload
	// does not fan out into pixel-multiplied storage waste.
	AllowUpscale bool

	// MaxPixels overrides the decompression-bomb guard for this pipeline
	// (default framework/image.DefaultMaxPixels, 64 MP).
	MaxPixels int64
}

// Deriver implements file.ImageDeriver over a VariantSet.
type Deriver struct {
	set fwimage.VariantSet
	cfg fwimage.Config
}

var _ file.ImageDeriver = (*Deriver)(nil)

// New builds a Deriver from cfg. It returns an error for a configuration
// that could not produce anything, or that framework/image would reject at
// process time anyway — better at wiring time than on the first upload.
func New(cfg Config) (*Deriver, error) {
	if (cfg.BlurHashX == 0) != (cfg.BlurHashY == 0) {
		return nil, fmt.Errorf("imagefield: BlurHashX and BlurHashY must both be set or both zero")
	}
	if cfg.BlurHashX < 0 || cfg.BlurHashX > 9 || cfg.BlurHashY < 0 || cfg.BlurHashY > 9 {
		return nil, fmt.Errorf("imagefield: BlurHash components must be in 1..9, got %dx%d",
			cfg.BlurHashX, cfg.BlurHashY)
	}
	if len(cfg.Variants) == 0 && cfg.BlurHashX == 0 && cfg.Placeholder == nil {
		return nil, fmt.Errorf("imagefield: Config derives nothing — set Variants, BlurHashX/Y, or Placeholder")
	}
	if len(cfg.Variants) > fwimage.MaxVariantsPerSet {
		return nil, fmt.Errorf("imagefield: %d variants exceeds MaxVariantsPerSet=%d",
			len(cfg.Variants), fwimage.MaxVariantsPerSet)
	}
	for i, v := range cfg.Variants {
		if v.Width < 1 {
			return nil, fmt.Errorf("imagefield: Variants[%d].Width must be >= 1", i)
		}
		if v.Format == fwimage.FormatUnknown {
			return nil, fmt.Errorf("imagefield: Variants[%d].Format must be set", i)
		}
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

// DeriveImage decodes the upload, produces every configured rendition,
// stores them beside the original, and returns their references plus the
// placeholder metadata.
//
// Renditions stream one at a time through VariantSet.ProcessTo, so peak
// memory stays near a single rendition rather than all of them summed —
// this runs inside a request, on bytes a client chose.
func (d *Deriver) DeriveImage(ctx context.Context, store upload.Storage, data []byte, primaryRef string) (*file.ImageDerivatives, error) {
	if store == nil {
		return nil, fmt.Errorf("imagefield: storage backend is required")
	}
	src, err := fwimage.DecodeBytesWithConfig(data, d.cfg)
	if err != nil {
		// Reached when a non-image (or a format this build cannot decode)
		// lands on an image field. Surfacing it fails the upload, which is
		// the point: the column is declared as an image.
		return nil, fmt.Errorf("imagefield: decoding upload: %w", err)
	}

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

	// Ascending width so the slice drops straight into a srcset.
	sort.Slice(out.Variants, func(i, j int) bool { return out.Variants[i].Width < out.Variants[j].Width })
	return out, nil
}

// baseNameFor derives the rendition name prefix from the original's storage
// key: "products/cover/a1b2-photo.jpg" becomes "a1b2-photo". Renditions
// therefore sit beside the original under the same directory and inherit
// its random suffix, so two uploads of the same filename never collide.
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
