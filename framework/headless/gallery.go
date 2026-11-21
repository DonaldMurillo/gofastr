package headless

import (
	"net/url"
	"strconv"
	"strings"

	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core-ui/urlsafe"
	"github.com/DonaldMurillo/gofastr/core/render"
)

// GalleryItem is one entry.
type GalleryItem struct {
	Src     string // required: the full image; an unsafe one degrades to the blank stub and a # link
	Thumb   string // the thumbnail, Src by default
	Alt     string // required: the image's description
	Caption string
	Width   int // the thumbnail's width, 200 by default
	Height  int // the thumbnail's height, 150 by default
}

// GalleryProps configures the gallery.
type GalleryProps struct {
	Items             []GalleryItem                      // required, at least one
	Label             string                             // required: names the list
	HrefFn            func(i int, it GalleryItem) string // per-item destination; empty keeps the full image
	ExtraAttrsPerItem map[int]html.Attrs                 // through Safe, on item N's anchor
	Lightbox          GalleryLightbox                    // opens a framework lightbox instead of navigating

	ID         string
	ExtraAttrs html.Attrs

	Parts Parts
}

// GalleryLightbox names the lightbox a gallery's items open; it is a typed prop because its keys are data-cui-* wiring.
type GalleryLightbox struct {
	Name  string // required for the wiring: the Name of a mounted framework/ui.Lightbox
	Group string // the data-fui-lightbox-group the viewer's nav walks; "<Name>-gallery" by default
}

// Gallery renders the image list.
func Gallery(p GalleryProps, s Classes) render.HTML {
	if len(p.Items) == 0 {
		panic("headless: Gallery requires at least one item — an empty gallery is a list that lists nothing")
	}
	checkLabel("Gallery", "Label", p.Label)
	if p.Lightbox.Name != "" {
		checkNoControlBytes("Gallery", "Lightbox.Name", p.Lightbox.Name)
		if p.Lightbox.Group != "" {
			checkNoControlBytes("Gallery", "Lightbox.Group", p.Lightbox.Group)
		}
	}
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
	if safe := urlsafe.Clean(thumb, urlsafe.ImageSource); safe != "" {
		thumb = safe
	} else {
		thumb = "/__gofastr/blank.png"
	}
	href := safeHref(it.Src)
	if p.HrefFn != nil {
		if h := p.HrefFn(i, it); h != "" {
			href = safeHref(h)
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
	for k, v := range Safe(p.ExtraAttrsPerItem[i], "href") {
		linkAttrs[k] = v
	}
	if p.Lightbox.Name != "" {
		group := p.Lightbox.Group
		if group == "" {
			group = p.Lightbox.Name + "-gallery"
		}
		src := urlsafe.Clean(it.Src, urlsafe.ImageSource)
		if src == "" {
			src = "/__gofastr/blank.png"
		}
		dl := "src=" + deeplinkEscape(src) +
			"&alt=" + deeplinkEscape(scrubControlBytes(it.Alt)) +
			"&group=" + deeplinkEscape(group)
		if it.Caption != "" {
			dl += "&caption=" + deeplinkEscape(scrubControlBytes(it.Caption))
		}
		linkAttrs["data-cui-open"] = p.Lightbox.Name
		linkAttrs["data-cui-deeplink"] = dl
		//gofastr:allow(layerprefix) the gallery link carries the framework lightbox module's group key by explicit Wiring; the hui family is the unwired render
		linkAttrs["data-fui-lightbox-group"] = group
	}
	img := b.El("img", PartBody, Attrs(map[string]string{
		"src":    thumb,
		"alt":    scrubControlBytes(it.Alt),
		"width":  strconv.Itoa(w),
		"height": strconv.Itoa(h),
	}))
	if it.Caption == "" {
		return b.El("li", PartControl, Internal(nil), b.El("a", PartLabel, linkAttrs, img))
	}
	return b.El("li", PartControl, Internal(nil),
		b.El("figure", PartHeader, nil,
			b.El("a", PartLabel, linkAttrs, img),
			b.El("figcaption", PartText, nil, render.Text(scrubControlBytes(it.Caption)))))
}

// deeplinkEscape encodes a deeplink key or value for the runtime's split on & and = and its decodeURIComponent.
func deeplinkEscape(s string) string {
	return strings.ReplaceAll(url.QueryEscape(s), "+", "%20")
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
			}, {
				Name: "a gallery wired to a lightbox",
				Why:  "the click opens the named overlay through the widget runtime's open contract, and the anchor keeps the full-image href for a reader with no script",
				HTML: Gallery(GalleryProps{Label: "Shots", Lightbox: GalleryLightbox{
					Name:  "docs",
					Group: "docs-gallery",
				}, Items: []GalleryItem{
					{Src: "/one.png", Alt: "The dashboard", Caption: "Overview"},
				}}, s),
			}}
		},
	})
}
