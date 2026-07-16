package ui

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/style"
)

func TestGalleryRequiresItems(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("Gallery without Items should panic")
		}
	}()
	Gallery(GalleryConfig{})
}

func TestGalleryItemRequiresAlt(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("Gallery item without Alt should panic")
		}
	}()
	Gallery(GalleryConfig{Items: []GalleryItem{{Src: "/a.jpg"}}})
}

func TestGalleryRendersUlOfLis(t *testing.T) {
	h := string(Gallery(GalleryConfig{
		Items: []GalleryItem{
			{Src: "/a.jpg", Alt: "A"},
			{Src: "/b.jpg", Alt: "B"},
			{Src: "/c.jpg", Alt: "C"},
		},
	}))
	if !strings.Contains(h, "<ul ") {
		t.Errorf("Gallery should render <ul>:\n%s", h)
	}
	if c := strings.Count(h, "<li "); c != 3 {
		t.Errorf("expected 3 <li>, got %d:\n%s", c, h)
	}
}

func TestGalleryDefaultAnchorIsExternal(t *testing.T) {
	// Without Lightbox or HrefFn the anchor should fall back to
	// opening Src in a new tab (no-JS friendly).
	h := string(Gallery(GalleryConfig{
		Items: []GalleryItem{{Src: "/full.jpg", Alt: "F"}},
	}))
	if !strings.Contains(h, `href="/full.jpg"`) {
		t.Errorf("anchor href should default to Src:\n%s", h)
	}
	if !strings.Contains(h, `target="_blank"`) || !strings.Contains(h, `rel="noopener"`) {
		t.Errorf("default anchor should open in new tab with rel=noopener:\n%s", h)
	}
}

func TestGalleryLightboxModeEmitsTriggerAttrs(t *testing.T) {
	h := string(Gallery(GalleryConfig{
		ID:       "photos",
		Lightbox: "photo-viewer",
		Items:    []GalleryItem{{Src: "/a.jpg", Alt: "A", Caption: "First"}},
	}))
	if !strings.Contains(h, `data-fui-open="photo-viewer"`) {
		t.Errorf("Lightbox mode should add data-fui-open=<name>:\n%s", h)
	}
	if !strings.Contains(h, "data-fui-deeplink=") {
		t.Errorf("Lightbox mode should add data-fui-deeplink:\n%s", h)
	}
	// %20-encoding (NOT '+') — runtime decoder is decodeURIComponent.
	if !strings.Contains(h, "caption=First") {
		t.Errorf("deeplink should carry caption:\n%s", h)
	}
	if !strings.Contains(h, `data-fui-lightbox-group="photos"`) {
		t.Errorf("Lightbox mode should add data-fui-lightbox-group=<id>:\n%s", h)
	}
}

func TestGalleryHrefFnOverridesDefault(t *testing.T) {
	h := string(Gallery(GalleryConfig{
		Items: []GalleryItem{{Src: "/a.jpg", Alt: "A"}},
		HrefFn: func(i int, it GalleryItem) string {
			return "/detail/" + it.Alt
		},
	}))
	if !strings.Contains(h, `href="/detail/A"`) {
		t.Errorf("HrefFn should drive the anchor href:\n%s", h)
	}
}

func TestGalleryStripVariantClass(t *testing.T) {
	h := string(Gallery(GalleryConfig{
		Variant: GalleryStrip,
		Items:   []GalleryItem{{Src: "/a.jpg", Alt: "A"}},
	}))
	if !strings.Contains(h, "ui-gallery--strip") {
		t.Errorf("Strip variant should add modifier class:\n%s", h)
	}
}

func TestGalleryMasonryVariantClass(t *testing.T) {
	h := string(Gallery(GalleryConfig{
		Variant: GalleryMasonry,
		Items:   []GalleryItem{{Src: "/a.jpg", Alt: "A"}},
	}))
	if !strings.Contains(h, "ui-gallery--masonry") {
		t.Errorf("Masonry variant should add modifier class:\n%s", h)
	}
}

func TestGalleryColumnsClassClampedTo12(t *testing.T) {
	h := string(Gallery(GalleryConfig{
		Columns: 24,
		Items:   []GalleryItem{{Src: "/a.jpg", Alt: "A"}},
	}))
	if !strings.Contains(h, "ui-gallery--cols-12") {
		t.Errorf("Columns > 12 should clamp to 12:\n%s", h)
	}
}

func TestGalleryCaptionOverlayClass(t *testing.T) {
	h := string(Gallery(GalleryConfig{
		CaptionMode: GalleryCaptionOverlay,
		Items:       []GalleryItem{{Src: "/a.jpg", Alt: "A", Caption: "C"}},
	}))
	if !strings.Contains(h, "ui-gallery--cap-overlay") {
		t.Errorf("Overlay caption mode should add modifier class:\n%s", h)
	}
}

func TestGalleryCaptionOffSkipsFigcaption(t *testing.T) {
	h := string(Gallery(GalleryConfig{
		CaptionMode: GalleryCaptionOff,
		Items:       []GalleryItem{{Src: "/a.jpg", Alt: "A", Caption: "C"}},
	}))
	if strings.Contains(h, "<figcaption") {
		t.Errorf("CaptionOff should NOT emit <figcaption>:\n%s", h)
	}
}

func TestGalleryRejectsUnknownVariant(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("Gallery with unknown Variant should panic")
		}
	}()
	Gallery(GalleryConfig{
		Variant: GalleryVariant("bogus"),
		Items:   []GalleryItem{{Src: "/a.jpg", Alt: "A"}},
	})
}

// The grid and masonry variants must be responsive WITHOUT consumer media
// queries: Columns is a maximum, and tracks collapse once they'd shrink
// below --ui-gallery-min. A plain repeat(N, 1fr) regression would crush
// tiles on narrow viewports for every consumer at once.
func TestGalleryColumnsAreResponsiveMaximum(t *testing.T) {
	css := galleryCSS(style.DefaultTheme())
	if strings.Contains(css, "repeat(var(--ui-gallery-cols), 1fr)") {
		t.Fatal("grid uses a fixed column count — Columns must be a responsive maximum (auto-fill + minmax)")
	}
	for _, want := range []string{
		"--ui-gallery-min",
		"repeat(auto-fill",
		"var(--ui-gallery-cols) - 1) * var(--ui-gallery-gap)",
		"column-width: var(--ui-gallery-min)",
	} {
		if !strings.Contains(css, want) {
			t.Errorf("gallery CSS missing responsive piece %q", want)
		}
	}
}
