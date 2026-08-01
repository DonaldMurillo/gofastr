package file

import (
	"context"
	"fmt"

	"github.com/DonaldMurillo/gofastr/core/upload"
)

// DerivedVariant is one stored rendition of an uploaded image — a single
// width/format pair produced alongside the original.
//
// The JSON tags here are snake_case on purpose: this struct is persisted
// verbatim into the entity's `<field>_variants` schema.JSON column, so the
// tags are a stored database format following the snake_case column
// convention — not an API response shape. Renaming them would orphan every
// existing row's variants.
type DerivedVariant struct {
	// StorageRef is the storage backend key the rendition was saved under.
	// It doubles as the URL path, matching FileField.URL/StorageRef.
	StorageRef string `json:"storage_ref"`

	// MIME is the rendition's content type (e.g. "image/webp").
	MIME string `json:"mime"`

	// Width and Height are the rendition's pixel dimensions. They feed a
	// responsive srcset directly.
	Width  int `json:"width"`
	Height int `json:"height"`
}

// ImageDerivatives holds everything derived from an uploaded image beyond
// the original bytes: the stored renditions plus whichever low-fidelity
// placeholder representations were requested.
type ImageDerivatives struct {
	// Variants are the stored renditions, ascending by width.
	Variants []DerivedVariant `json:"variants,omitempty"`

	// BlurHash is a ~28-character base83 string. Cheap to store in a
	// column; render it with framework/image.BlurHashDataURL.
	BlurHash string `json:"blurhash,omitempty"`

	// Placeholder is a base64 data: URL (an LQIP). Larger than a BlurHash
	// but needs no decode step at render time.
	Placeholder string `json:"placeholder,omitempty"`
}

// Validate enforces the same invariants on derived references that
// FileField.Validate enforces on the primary file — they reach the same
// sinks (an <img src>, a storage delete) and arrive from the same
// untrusted places once persisted and read back.
func (d *ImageDerivatives) Validate() error {
	if d == nil {
		return nil
	}
	for i := range d.Variants {
		v := &d.Variants[i]
		if len(v.StorageRef) > MaxFileFieldStringBytes {
			return fmt.Errorf("%w: variant %d storage_ref is %d bytes (max %d)",
				ErrFileFieldOversize, i, len(v.StorageRef), MaxFileFieldStringBytes)
		}
		if isUnsafeURLScheme(v.StorageRef) {
			return fmt.Errorf("%w: variant %d storage_ref %q", ErrFileFieldURLScheme, i, v.StorageRef)
		}
		if hasTraversal(v.StorageRef) {
			return fmt.Errorf("%w: variant %d storage_ref %q", ErrFileFieldTraversal, i, v.StorageRef)
		}
		if v.MIME != "" && !isSafeMIMEString(v.MIME) {
			return fmt.Errorf("%w: variant %d mime %q", ErrFileFieldMimeUnsafe, i, v.MIME)
		}
		if v.Width < 0 || v.Height < 0 {
			return fmt.Errorf("%w: variant %d is %dx%d", ErrFileFieldSize, i, v.Width, v.Height)
		}
	}
	// The placeholder is rendered into an <img src>. A BlurHash is not a
	// URL and is bounded by its own format, so only length is checked.
	if len(d.Placeholder) > MaxFileFieldStringBytes {
		return fmt.Errorf("%w: placeholder is %d bytes (max %d)",
			ErrFileFieldOversize, len(d.Placeholder), MaxFileFieldStringBytes)
	}
	if len(d.BlurHash) > MaxFileFieldStringBytes {
		return fmt.Errorf("%w: blurhash is %d bytes (max %d)",
			ErrFileFieldOversize, len(d.BlurHash), MaxFileFieldStringBytes)
	}
	// An LQIP is an inline raster data: URI and nothing else — that is what
	// image.Placeholder produces and the only thing the render sink
	// (framework/ui.placeholderUsable) will paint. Gating this on
	// isUnsafeURLScheme, as this once did, only ran the allow-list for
	// values that already looked like a scheme attack, so a remote
	// "https://tracker.example/px.gif" sailed through: never painted, but
	// persisted through an image column and handed to any host that renders
	// the raw value. Check unconditionally; the two allow-lists are pinned
	// against each other by TestPlaceholderAllowListsAgree.
	if d.Placeholder != "" && !isRasterDataURL(d.Placeholder) {
		return fmt.Errorf("%w: placeholder is not an inline raster image", ErrFileFieldURLScheme)
	}
	return nil
}

// ImageDeriver turns uploaded image bytes into stored renditions and
// placeholder metadata.
//
// It is an interface rather than a direct call into framework/image because
// this package is a leaf that framework/crud imports: an edge to
// framework/image would link every image decoder plus the WebP encoder into
// every application that has a CRUD handler, whether or not it processes
// images. framework/imagefield provides the implementation, so only
// applications that ask for it pay for the pipeline.
//
// Scope note: this is the framework's only per-field transform seam, and it
// is deliberately image-shaped — it runs on write, for schema.Image fields,
// and its output goes to sibling columns. The general version (write and read
// transforms for any field, composed per field) is being designed in
// https://github.com/DonaldMurillo/gofastr/issues/144. If that lands, this
// interface is a candidate to become one instance of it rather than its own
// mechanism; until then, resist growing it sideways into a general hook —
// widening this interface to cover non-image fields would prejudge the
// harder half of that design (read transforms versus filter/sort/search).
type ImageDeriver interface {
	// DeriveImage is called with the raw uploaded bytes after the upload
	// has passed content sniffing but before ProcessFileField returns.
	//
	// primaryRef is the storage key the original was saved under;
	// implementations derive rendition keys from it so everything for one
	// upload lives together. Renditions must be written through store.
	DeriveImage(ctx context.Context, store upload.Storage, data []byte, primaryRef string) (*ImageDerivatives, error)
}

// ProcessOption configures ProcessFileField.
type ProcessOption func(*processConfig)

type processConfig struct {
	deriver ImageDeriver
}

// WithImageDeriver runs deriver over the uploaded bytes and attaches the
// result to FileField.Image.
//
// A derive failure fails the whole upload rather than yielding a file with
// no renditions. The caller asked for renditions; silently returning a
// FileField whose Image is nil would surface much later as a page with no
// placeholder and no srcset, with nothing in the logs pointing back here.
// It also means a non-image uploaded to an image field is rejected, which
// is the desired behavior for a schema.Image column.
func WithImageDeriver(deriver ImageDeriver) ProcessOption {
	return func(c *processConfig) { c.deriver = deriver }
}

// isRasterDataURL reports whether s is a `data:` URL carrying one of the
// raster media types an LQIP can legitimately use. Deliberately narrow and
// local: this package is a leaf and does not import the UI tree's
// urlsafe helper, and the two allow-lists are checked against each other
// by TestPlaceholderAllowListsAgree.
func isRasterDataURL(s string) bool {
	const prefix = "data:image/"
	if len(s) <= len(prefix) {
		return false
	}
	for i := range len(prefix) {
		c := s[i]
		if c >= 'A' && c <= 'Z' {
			c += 'a' - 'A'
		}
		if c != prefix[i] {
			return false
		}
	}
	rest := s[len(prefix):]
	comma := -1
	for i := 0; i < len(rest); i++ {
		if rest[i] == ',' {
			comma = i
			break
		}
	}
	if comma <= 0 {
		return false
	}
	meta := rest[:comma]
	for i := 0; i < len(meta); i++ {
		if meta[i] == ';' {
			meta = meta[:i]
			break
		}
	}
	switch lowerASCII(meta) {
	case "jpeg", "png", "gif", "webp", "avif":
		return true
	}
	return false
}

func lowerASCII(s string) string {
	b := []byte(s)
	for i := range b {
		if b[i] >= 'A' && b[i] <= 'Z' {
			b[i] += 'a' - 'A'
		}
	}
	return string(b)
}
