package headless

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/html"
)

func renderGallery(p GalleryProps) string { return string(Gallery(p, nil)) }

func TestGalleryRendersNamedListWithSafeLinks(t *testing.T) {
	h := renderGallery(GalleryProps{Label: "Screenshots", Items: []GalleryItem{
		{Src: "/one.png", Alt: "The dashboard", Caption: "Overview"},
		{Src: "/two.png", Alt: "The settings", Thumb: "/two-thumb.png", Width: 100, Height: 50},
	}})
	for _, want := range []string{
		`<ul aria-label="Screenshots">`,
		`href="/one.png"`,
		`alt="The dashboard" height="150" src="/one.png" width="200"`,
		`src="/two-thumb.png"`,
		`height="50" src="/two-thumb.png" width="100"`,
		`<figcaption>Overview</figcaption>`,
	} {
		if !strings.Contains(h, want) {
			t.Errorf("gallery missing %q:\n%s", want, h)
		}
	}
}

func TestGalleryHrefFnAndPerItemAttrs(t *testing.T) {
	h := renderGallery(GalleryProps{Label: "Photos", Items: []GalleryItem{
		{Src: "/one.png", Alt: "One"},
		{Src: "/two.png", Alt: "Two"},
	}, HrefFn: func(i int, it GalleryItem) string {
		if i == 0 {
			return "/detail/one"
		}
		return ""
	}, ExtraAttrsPerItem: map[int]map[string]string(nil)})
	if !strings.Contains(h, `href="/detail/one"`) {
		t.Errorf("the caller's href must replace the fallback:\n%s", h)
	}
	if !strings.Contains(h, `href="/two.png"`) {
		t.Errorf("an empty per-item href keeps the full-image fallback:\n%s", h)
	}
}

func TestGalleryUnsafeHrefDegrades(t *testing.T) {
	h := renderGallery(GalleryProps{Label: "P", Items: []GalleryItem{
		{Src: "/ok.png", Alt: "ok"},
	}, HrefFn: func(i int, it GalleryItem) string {
		return "javascript:alert(1)"
	}})
	if strings.Contains(h, `href="javascript:`) {
		t.Errorf("an unsafe href reached the link:\n%s", h)
	}
	if !strings.Contains(h, `href="#"`) {
		t.Errorf("the unsafe href must degrade to #:\n%s", h)
	}
}

func TestGalleryHostileThumbAndAttrsDegrade(t *testing.T) {
	h := renderGallery(GalleryProps{Label: "P", Items: []GalleryItem{
		{Src: "/ok.png", Thumb: "javascript:alert(1)", Alt: "a"},
	}, ExtraAttrsPerItem: map[int]html.Attrs{0: {
		"onclick":           "alert(1)",
		"style":             "display:none",
		"data-hui-lightbox": "forged",
		"data-gallery-tag":  "kept",
	}}})
	if strings.Contains(h, `src="javascript:`) {
		t.Errorf("an unsafe Thumb reached img src:\n%s", h)
	}
	if !strings.Contains(h, `src="/__gofastr/blank.png"`) {
		t.Errorf("an unsafe Thumb must degrade to the blank stub:\n%s", h)
	}
	for _, dropped := range []string{"onclick", "style=", "data-hui-lightbox"} {
		if strings.Contains(h, dropped) {
			t.Errorf("a refused per-item attr (%s) rode the anchor:\n%s", dropped, h)
		}
	}
	if !strings.Contains(h, `data-gallery-tag="kept"`) {
		t.Errorf("a benign data-* per-item attr was dropped with the refused ones:\n%s", h)
	}
}

func TestGalleryRefusesBrokenConfiguration(t *testing.T) {
	cases := []struct {
		name string
		p    GalleryProps
	}{
		{"no items", GalleryProps{Label: "L"}},
		{"control bytes in the lightbox name", GalleryProps{Label: "L", Lightbox: GalleryLightbox{Name: "a\rb"}, Items: []GalleryItem{{Src: "/a.png", Alt: "a"}}}},
		{"control bytes in the lightbox group", GalleryProps{Label: "L", Lightbox: GalleryLightbox{Name: "lb", Group: "g\x7f"}, Items: []GalleryItem{{Src: "/a.png", Alt: "a"}}}},
		{"no src", GalleryProps{Label: "L", Items: []GalleryItem{{Alt: "a"}}}},
		{"no alt", GalleryProps{Label: "L", Items: []GalleryItem{{Src: "/a.png"}}}},
	}
	for _, tc := range cases {
		func() {
			defer func() {
				if recover() == nil {
					t.Errorf("%s: rendering should have been refused", tc.name)
				}
			}()
			Gallery(tc.p, nil)
		}()
	}
}

func TestGalleryLightboxWiring(t *testing.T) {
	h := renderGallery(GalleryProps{Label: "Shots", Lightbox: GalleryLightbox{Name: "docs"}, Items: []GalleryItem{
		{Src: "/one.png", Alt: "The dashboard", Caption: "Overview"},
	}})
	for _, want := range []string{
		`data-fui-open="docs"`,
		`data-fui-lightbox-group="docs-gallery"`, // derived: Group empty
		`data-fui-deeplink="src=%2Fone.png&amp;alt=The%20dashboard&amp;group=docs-gallery&amp;caption=Overview"`,
		`href="/one.png"`, // the no-script path stays the full image
	} {
		if !strings.Contains(h, want) {
			t.Errorf("lightbox wiring missing %q:\n%s", want, h)
		}
	}
	// An explicit group wins over the derived one, and the caption is
	// absent from the deeplink when the item carries none.
	h2 := renderGallery(GalleryProps{Label: "Shots", Lightbox: GalleryLightbox{Name: "docs", Group: "shots"}, Items: []GalleryItem{
		{Src: "/a.png", Alt: "A"},
	}})
	if !strings.Contains(h2, `data-fui-lightbox-group="shots"`) || strings.Contains(h2, "caption=") {
		t.Errorf("explicit group or captionless deeplink wrong:\n%s", h2)
	}
	// A hostile caption cannot smuggle a control byte into the deeplink.
	h3 := renderGallery(GalleryProps{Label: "Shots", Lightbox: GalleryLightbox{Name: "docs"}, Items: []GalleryItem{
		{Src: "/a.png", Alt: "A", Caption: "x\x00y"},
	}})
	if strings.Contains(h3, "x%00y") {
		t.Errorf("a control byte travelled into the deeplink:\n%s", h3)
	}
}
