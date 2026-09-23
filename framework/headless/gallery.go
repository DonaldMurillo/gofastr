package headless

import (
	"strconv"

	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core-ui/urlsafe"
	"github.com/DonaldMurillo/gofastr/core/render"
)

// The gallery: a list of images where every item is a link — to the
// full image, or to whatever the caller's href says. No module: a
// click that opens a Lightbox travels the widget runtime's open
// contract (data-fui-open), which the adapter carries; the primitive
// guarantees the semantics (a named list, alt text on every image, a
// safe href on every link, intrinsic sizes so the layout does not
// shift under the images).

// GalleryItem is one entry.
type GalleryItem struct {
	// Src is the full-resolution image URL. Required; refused when
	// unsafe (a javascript: URL in an image is a payload, not a
	// picture) — it degrades to nothing the day the caller hands one
	// in, so the refusal is loud.
	Src string
	// Thumb is the thumbnail URL. Defaults to Src.
	Thumb string
	// Alt is the image's description. Required: an image with no
	// description is decoration lying about being content.
	Alt string
	// Caption is optional descriptive text under the image.
	Caption string
	// Width / Height for the thumbnail (CLS-safe). Default 200×150.
	Width  int
	Height int
}

// GalleryProps configures the gallery.
type GalleryProps struct {
	// Items are the entries, in order. Required non-empty.
	Items []GalleryItem
	// Label names the list. Required: an unnamed image list is a
	// landmark a screen reader cannot jump to.
	Label string
	// HrefFn, when set, returns a per-item destination. Empty for an
	// item makes that item's link the full image (the no-JS fallback
	// the retired component shipped).
	HrefFn func(i int, it GalleryItem) string
	// ExtraAttrsPerItem adds attributes to item N's anchor (a lightbox
	// group id, a deeplink): index → attrs.
	ExtraAttrsPerItem map[int]html.Attrs

	ID         string
	ExtraAttrs html.Attrs

	// Parts: attrs on the list, items, figures, links, images,
	// captions.
	Parts Parts
}

// Gallery renders the image list.
func Gallery(p GalleryProps, s Classes) render.HTML {
	if len(p.Items) == 0 {
		panic("headless: Gallery requires at least one item — an empty gallery is a list that lists nothing")
	}
	checkLabel("Gallery", "Label", p.Label)
	b := p.Parts.Box(s)
	own := Merge(Safe(p.ExtraAttrs, "aria-label"), Attrs(map[string]string{
		"aria-label": scrubControlBytes(p.Label),
		"id":         p.ID,
	}))
	items := make([]render.HTML, 0, len(p.Items))
	for i, it := range p.Items {
		items = append(items, galleryItem(b, i, it, p))
	}
	return b.El("ul", PartRoot, own, items...)
}

func galleryItem(b Box, i int, it GalleryItem, p GalleryProps) render.HTML {
	if it.Src == "" {
		panic("headless: Gallery item requires Src — a link to nothing is not a fallback")
	}
	if scrubControlBytes(it.Alt) == "" {
		panic("headless: Gallery item requires Alt — an image with no description is decoration lying about being content")
	}
	thumb := it.Thumb
	if thumb == "" {
		thumb = it.Src
	}
	// Thumb and the Src it falls back to pass the image-source policy
	// before either reaches img src (the anchor's href already runs the
	// anchor policy below). An unsafe value degrades the way every URL
	// sink here degrades — the framework's blank stub, never the
	// caller's bytes: a javascript: Thumb is a payload, not a picture.
	if safe := urlsafe.Clean(thumb, urlsafe.ImageSource); safe != "" {
		thumb = safe
	} else {
		thumb = "/__gofastr/blank.png"
	}
	href := safeGalleryHref(it.Src)
	if p.HrefFn != nil {
		if h := p.HrefFn(i, it); h != "" {
			href = safeGalleryHref(h)
		}
	}
	w, h := it.Width, it.Height
	if w == 0 {
		w = 200
	}
	if h == 0 {
		h = 150
	}
	linkAttrs := Attrs(map[string]string{"href": href})
	// Caller-supplied per-item attrs go through the same refusal every
	// extra-attrs surface here goes through: the anchor's own keys
	// (href) are owned, and the refused families (on*, style, the
	// data-hui-*/data-fui-* wiring) never ride an anchor the caller
	// did not build. Safe folds and refuses; its own two-spellings
	// check subsumes the walk this replaced.
	for k, v := range Safe(p.ExtraAttrsPerItem[i], "href") {
		linkAttrs[k] = v
	}
	img := b.El("img", PartBody, Attrs(map[string]string{
		"src":    thumb,
		"alt":    scrubControlBytes(it.Alt),
		"width":  strconv.Itoa(w),
		"height": strconv.Itoa(h),
	}))
	if it.Caption == "" {
		return b.El("li", PartControl, nil, b.El("a", PartLabel, linkAttrs, img))
	}
	return b.El("li", PartControl, nil,
		b.El("figure", PartHeader, nil,
			b.El("a", PartLabel, linkAttrs, img),
			b.El("figcaption", PartText, nil, render.Text(scrubControlBytes(it.Caption)))))
}

// safeGalleryHref degrades an unsafe href to the fragment, the same
// policy every link this package renders follows.
func safeGalleryHref(href string) string {
	if h := cleanTabHref(href); h != "" {
		return h
	}
	return "#"
}

func lowerASCII(s string) string {
	out := []byte(s)
	for i := range out {
		if out[i] >= 'A' && out[i] <= 'Z' {
			out[i] += 'a' - 'A'
		}
	}
	return string(out)
}

func init() {
	Register(Spec{
		Name: "Gallery",
		Anatomy: []Part{PartRoot, PartControl, PartHeader,
			PartLabel, PartBody, PartText},
		WithParts: func(s Classes, parts Parts) render.HTML {
			return Gallery(GalleryProps{Label: "Screenshots", Items: []GalleryItem{
				{Src: "/a.png", Alt: "A"},
			}, Parts: parts}, s)
		},
		Cases: func(k Kit) []Case {
			s := k.Classes
			return []Case{{
				Name: "a gallery with captions",
				Why:  "every item is a real link to the full image, so the gallery works with no script at all",
				HTML: Gallery(GalleryProps{Label: "Screenshots", Items: []GalleryItem{
					{Src: "/one.png", Alt: "The dashboard", Caption: "Overview"},
					{Src: "/two.png", Alt: "The settings", Thumb: "/two-thumb.png"},
				}}, s),
			}, {
				Name: "a gallery with per-item hrefs",
				Why:  "a caller's href replaces the full-image fallback per item, and an unsafe one degrades instead of shipping",
				HTML: Gallery(GalleryProps{Label: "Photos", HrefFn: func(i int, it GalleryItem) string {
					if i == 0 {
						return "/detail/one"
					}
					return "javascript:alert(1)"
				}, Items: []GalleryItem{
					{Src: "/one.png", Alt: "One"},
					{Src: "/two.png", Alt: "Two"},
				}}, s),
			}}
		},
	})
}
