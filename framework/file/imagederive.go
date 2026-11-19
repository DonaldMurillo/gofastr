package file

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"

	"github.com/DonaldMurillo/gofastr/core/upload"
	"github.com/DonaldMurillo/gofastr/internal/asciicase"
)

// DerivedVariant is one stored rendition of an uploaded image.
type DerivedVariant struct {
	// StorageRef is the literal storage key; escape each segment for a URL.
	StorageRef string `json:"storage_ref"`

	// MIME is the rendition's content type (e.g. "image/webp").
	MIME string `json:"mime"`

	// Width and Height are the rendition's pixel dimensions.
	Width  int `json:"width"`
	Height int `json:"height"`
}

// VariantStorageRefs returns the storage_ref of every rendition in a stored
// <field>_variants value; ok is false when raw is not a JSON array.
func VariantStorageRefs(raw []byte) (refs []string, ok bool) {
	var vs []struct {
		StorageRef string `json:"storage_ref"`
	}
	if err := json.Unmarshal(raw, &vs); err != nil || vs == nil {
		return nil, false
	}
	for _, v := range vs {
		if v.StorageRef != "" {
			refs = append(refs, v.StorageRef)
		}
	}
	return refs, true
}

// IsExternalURL reports whether s is an absolute http(s) URL with a host.
func IsExternalURL(s string) bool {
	u, err := url.Parse(s)
	if err != nil || u.Host == "" {
		return false
	}
	return strings.EqualFold(u.Scheme, "http") || strings.EqualFold(u.Scheme, "https")
}

// ImageDerivatives holds an uploaded image's renditions and placeholders.
type ImageDerivatives struct {
	// Variants are the stored renditions, ascending by width.
	Variants []DerivedVariant `json:"variants,omitempty"`

	// BlurHash is a short base83 placeholder string.
	BlurHash string `json:"blurhash,omitempty"`

	// Placeholder is an inline raster data: URL (an LQIP).
	Placeholder string `json:"placeholder,omitempty"`
}

// Validate holds derived references to FileField.Validate's rules.
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
		if idx := indexControlByte(v.StorageRef); idx >= 0 {
			return fmt.Errorf("%w: variant %d storage_ref contains %#x at offset %d",
				ErrFileFieldControlBytes, i, v.StorageRef[idx], idx)
		}
		if r, off := indexForgedRune(v.StorageRef); off >= 0 {
			return fmt.Errorf("%w: variant %d storage_ref contains invisible or bidi rune U+%04X at offset %d",
				ErrFileFieldControlBytes, i, r, off)
		}
		if len(v.MIME) > MaxFileFieldStringBytes {
			return fmt.Errorf("%w: variant %d mime is %d bytes (max %d)",
				ErrFileFieldOversize, i, len(v.MIME), MaxFileFieldStringBytes)
		}
		if r, off := indexForgedRune(v.MIME); off >= 0 {
			return fmt.Errorf("%w: variant %d mime contains invisible or bidi rune U+%04X at offset %d",
				ErrFileFieldControlBytes, i, r, off)
		}
		if idx := indexControlByte(v.MIME); idx >= 0 {
			return fmt.Errorf("%w: variant %d mime contains %#x at offset %d",
				ErrFileFieldControlBytes, i, v.MIME[idx], idx)
		}
		if v.MIME != "" && !isSafeMIMEString(v.MIME) {
			return fmt.Errorf("%w: variant %d mime %q", ErrFileFieldMimeUnsafe, i, v.MIME)
		}
		if v.Width < 0 || v.Height < 0 {
			return fmt.Errorf("%w: variant %d is %dx%d", ErrFileFieldSize, i, v.Width, v.Height)
		}
	}
	if len(d.Placeholder) > MaxFileFieldStringBytes {
		return fmt.Errorf("%w: placeholder is %d bytes (max %d)",
			ErrFileFieldOversize, len(d.Placeholder), MaxFileFieldStringBytes)
	}
	if len(d.BlurHash) > MaxFileFieldStringBytes {
		return fmt.Errorf("%w: blurhash is %d bytes (max %d)",
			ErrFileFieldOversize, len(d.BlurHash), MaxFileFieldStringBytes)
	}
	if d.Placeholder != "" && !isRasterDataURL(d.Placeholder) {
		return fmt.Errorf("%w: placeholder is not an inline raster image", ErrFileFieldURLScheme)
	}
	return nil
}

// ImageDeriver turns uploaded image bytes into stored renditions and
// placeholders; framework/imagefield implements it.
type ImageDeriver interface {
	// DeriveImage runs after content sniffing. Renditions must be written
	// through store, keyed from primaryRef.
	DeriveImage(ctx context.Context, store upload.Storage, data []byte, primaryRef string) (*ImageDerivatives, error)
}

// ProcessOption configures ProcessFileField.
type ProcessOption func(*processConfig)

type processConfig struct {
	deriver       ImageDeriver
	stripMetadata bool
	allow         []string
}

// ImageTypes are the raster types an image field takes: PNG, JPEG, GIF
// and WebP.
var ImageTypes = []string{"image/png", "image/jpeg", "image/gif", "image/webp"}

// AllowTypes refuses an upload whose sniffed type is not one of mimes, with
// ErrFileFieldType, before anything is saved.
func AllowTypes(mimes ...string) ProcessOption {
	return func(c *processConfig) { c.allow = mimes }
}

// WithImageDeriver runs deriver over the upload and sets FileField.Image; a
// derive failure fails the upload.
func WithImageDeriver(deriver ImageDeriver) ProcessOption {
	return func(c *processConfig) { c.deriver = deriver }
}

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
	switch asciicase.Lower(meta) {
	case "jpeg", "png", "gif", "webp", "avif":
		return true
	}
	return false
}
