package ui

import (
	"net/url"
	"strconv"

	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core-ui/registry"
	"github.com/DonaldMurillo/gofastr/core-ui/style"
	"github.com/DonaldMurillo/gofastr/core/render"
)

// ─── Gallery ────────────────────────────────────────────────────────
//
// Standalone thumbnail surface. Three visual variants share the same
// Item shape:
//
//   - GalleryGrid    — CSS Grid, `Columns` × `Gap`. The default.
//   - GalleryStrip   — horizontal scroll-snap row.
//   - GalleryMasonry — CSS `columns: <n>` so tiles flow at natural
//                      aspect (Pinterest-style).
//
// Click behaviour (pick ONE; otherwise the anchor opens Src in a new
// tab as the no-JS fallback):
//
//   - Lightbox — name of a paired framework/ui.Lightbox. Each item
//                emits data-fui-open + data-fui-deeplink so clicking
//                opens the overlay with the matching image.
//   - HrefFn   — a function returning a per-item URL the anchor
//                navigates to. Use for "click photo → detail page".
//
// Caption rendering is controlled by CaptionMode.

// GalleryVariant picks the surface layout.
type GalleryVariant string

const (
	GalleryGrid    GalleryVariant = ""
	GalleryStrip   GalleryVariant = "strip"
	GalleryMasonry GalleryVariant = "masonry"
)

// GalleryCaptionMode picks where captions render.
type GalleryCaptionMode string

const (
	GalleryCaptionBelow   GalleryCaptionMode = ""        // <figcaption> under each thumb
	GalleryCaptionOverlay GalleryCaptionMode = "overlay" // gradient + text over the bottom of each thumb on hover/focus
	GalleryCaptionOff     GalleryCaptionMode = "off"     // no caption
)

// GalleryItem is one entry.
type GalleryItem struct {
	// Src is the full-resolution image URL (required).
	Src string
	// Thumb is the thumbnail URL. Defaults to Src.
	Thumb string
	// Alt is the accessible image description (required — empty Alt
	// is rejected at render time to surface omissions).
	Alt string
	// Caption is optional descriptive text shown per CaptionMode.
	Caption string
	// Width / Height for the thumbnail (CLS-safe). Default 200×150.
	Width  int
	Height int
}

// GalleryConfig configures a Gallery.
type GalleryConfig struct {
	// Variant picks the surface layout.
	Variant GalleryVariant
	// Items are the entries (≥1).
	Items []GalleryItem
	// Label is the accessible label for the gallery list. Defaults
	// to "Image gallery".
	Label string
	// Columns (Grid mode) — the number of columns. Default 3.
	// Masonry mode: uses this as the column count too.
	Columns int
	// Gap between thumbs. Default GapMD.
	Gap Gap
	// Lightbox, when non-empty, is the Name of a paired
	// framework/ui.Lightbox. Each item becomes a trigger for that
	// lightbox via data-fui-open + data-fui-deeplink.
	Lightbox string
	// HrefFn, when set, returns a per-item destination URL. Ignored
	// when Lightbox is set.
	HrefFn func(i int, it GalleryItem) string
	// CaptionMode controls caption rendering. Default Below.
	CaptionMode GalleryCaptionMode
	// ID / Class / Attrs pass through to the wrapper.
	ID         string
	Class      string
	ExtraAttrs html.Attrs
}

// Gallery renders the thumbnail surface.
func Gallery(cfg GalleryConfig) render.HTML {
	if len(cfg.Items) == 0 {
		panic("ui: Gallery requires ≥1 Item")
	}
	switch cfg.Variant {
	case GalleryGrid, GalleryStrip, GalleryMasonry:
	default:
		panic("ui: Gallery unknown Variant " + string(cfg.Variant) +
			` — pick one of: "" (grid), strip, masonry`)
	}
	switch cfg.CaptionMode {
	case GalleryCaptionBelow, GalleryCaptionOverlay, GalleryCaptionOff:
	default:
		panic("ui: Gallery unknown CaptionMode " + string(cfg.CaptionMode))
	}
	label := cfg.Label
	if label == "" {
		label = "Image gallery"
	}
	cols := cfg.Columns
	if cols == 0 {
		cols = 3
	}
	gap := cfg.Gap
	if gap != "" && gap != "xs" && gap != "sm" && gap != "lg" && gap != "xl" {
		panic("ui: Gallery unknown Gap " + string(gap))
	}

	cls := "ui-gallery"
	if cfg.Variant != GalleryGrid {
		cls += " ui-gallery--" + string(cfg.Variant)
	}
	if cfg.CaptionMode != GalleryCaptionBelow {
		cls += " ui-gallery--cap-" + string(cfg.CaptionMode)
	}
	if gap != "" {
		cls += " ui-gallery--gap-" + string(gap)
	}
	if cfg.Class != "" {
		cls += " " + cfg.Class
	}

	attrs := html.Attrs{
		"class":      cls,
		"aria-label": label,
	}
	// Columns is exposed as a CSS custom property via data-fui-cols
	// — no inline style needed (CSP). The stylesheet reads attr()
	// where it can; for cross-browser safety we read it via a class
	// suffix that maps to a precomputed --ui-gallery-cols value.
	if cfg.Variant == GalleryGrid || cfg.Variant == GalleryMasonry {
		// Cap at 12 for the precomputed class set; very wide grids
		// fall back to 12.
		c := cols
		if c < 1 {
			c = 1
		}
		if c > 12 {
			c = 12
		}
		attrs["class"] = cls + " ui-gallery--cols-" + strconv.Itoa(c)
	}
	if cfg.ID != "" {
		attrs["id"] = cfg.ID
	}
	for k, v := range cfg.ExtraAttrs {
		attrs[k] = v
	}

	rows := make([]render.HTML, 0, len(cfg.Items))
	for i, it := range cfg.Items {
		if it.Src == "" {
			panic("ui: Gallery item requires Src")
		}
		if it.Alt == "" {
			panic("ui: Gallery item requires Alt — set a meaningful description; the framework panics by default to surface missing alt text")
		}
		thumb := it.Thumb
		if thumb == "" {
			thumb = it.Src
		}
		// Drop unsafe schemes on the thumbnail src (see safety.go). An
		// unsafe value falls back to the framework's 1×1 placeholder so
		// a javascript:/data: URL never reaches <img src>.
		if safe := safeURL(thumb); safe != "" {
			thumb = safe
		} else {
			thumb = "/__gofastr/blank.png"
		}
		w := it.Width
		if w == 0 {
			w = 200
		}
		h := it.Height
		if h == 0 {
			h = 150
		}

		linkAttrs := map[string]string{
			"class":      "ui-gallery__item",
			"aria-label": it.Alt,
		}
		// Click behaviour: Lightbox wins over HrefFn wins over the
		// "open Src in new tab" default.
		switch {
		case cfg.Lightbox != "":
			// Open the paired Lightbox. Deeplink carries src+alt+caption
			// and group=<gallery-id> so the Lightbox runtime can walk
			// siblings for prev/next nav.
			groupID := cfg.ID
			if groupID == "" {
				groupID = cfg.Lightbox + "-gallery"
			}
			dl := "src=" + url.PathEscape(it.Src) +
				"&alt=" + url.PathEscape(it.Alt) +
				"&group=" + url.PathEscape(groupID)
			if it.Caption != "" {
				dl += "&caption=" + url.PathEscape(it.Caption)
			}
			if h := safeURL(it.Src); h != "" {
				linkAttrs["href"] = h
				linkAttrs["target"] = "_blank"
				linkAttrs["rel"] = "noopener"
			}
			linkAttrs["data-fui-open"] = cfg.Lightbox
			linkAttrs["data-fui-deeplink"] = dl
			linkAttrs["data-fui-lightbox-group"] = groupID
		case cfg.HrefFn != nil:
			if h := safeURL(cfg.HrefFn(i, it)); h != "" {
				linkAttrs["href"] = h
			}
		default:
			if h := safeURL(it.Src); h != "" {
				linkAttrs["href"] = h
				linkAttrs["target"] = "_blank"
				linkAttrs["rel"] = "noopener"
			}
		}

		figureChildren := []render.HTML{
			render.Tag("img", map[string]string{
				"src":     thumb,
				"alt":     it.Alt,
				"width":   strconv.Itoa(w),
				"height":  strconv.Itoa(h),
				"loading": "lazy",
				"class":   "ui-gallery__thumb",
			}),
		}
		if it.Caption != "" && cfg.CaptionMode != GalleryCaptionOff {
			figureChildren = append(figureChildren,
				render.Tag("figcaption", map[string]string{"class": "ui-gallery__caption"},
					render.Text(it.Caption)))
		}

		rows = append(rows, render.Tag("li", map[string]string{"class": "ui-gallery__row"},
			render.Tag("a", linkAttrs,
				render.Tag("figure", map[string]string{"class": "ui-gallery__figure"},
					figureChildren...,
				),
			),
		))
	}
	return galleryStyle.WrapHTML(render.Tag("ul", attrs, rows...))
}

var galleryStyle = registry.RegisterStyle("ui-gallery", galleryCSS)

func galleryCSS(_ style.Theme) string {
	return `[data-fui-comp="ui-gallery"] {
  list-style: none;
  margin: 0;
  padding: 0;
  --ui-gallery-cols: 3;
  --ui-gallery-gap: var(--spacing-md, 12px);
}
[data-fui-comp="ui-gallery"] .ui-gallery__row {
  margin: 0;
  padding: 0;
}
[data-fui-comp="ui-gallery"] .ui-gallery__item {
  display: block;
  border-radius: var(--radii-md, 8px);
  overflow: hidden;
  border: 1px solid var(--color-border, #E4E4E7);
  background: var(--color-surface, #FFFFFF);
  text-decoration: none;
  color: inherit;
  cursor: zoom-in;
  transition: border-color 120ms ease, transform 120ms ease;
}
[data-fui-comp="ui-gallery"] .ui-gallery__item:hover {
  border-color: var(--color-primary, #4F46E5);
}
[data-fui-comp="ui-gallery"] .ui-gallery__item:focus-visible {
  outline: 2px solid var(--color-primary, #4F46E5);
  outline-offset: 2px;
}
[data-fui-comp="ui-gallery"] .ui-gallery__figure {
  margin: 0;
  display: grid;
  gap: var(--spacing-xs, 4px);
}
[data-fui-comp="ui-gallery"] .ui-gallery__thumb {
  display: block;
  inline-size: 100%;
  block-size: auto;
  object-fit: cover;
}
[data-fui-comp="ui-gallery"] .ui-gallery__caption {
  margin: 0;
  padding: 4px var(--spacing-sm, 8px) var(--spacing-sm, 8px);
  font-size: 0.85rem;
  color: var(--color-text-muted, #52525B);
}

/* Gap presets. */
[data-fui-comp="ui-gallery"].ui-gallery--gap-xs { --ui-gallery-gap: var(--spacing-xs, 4px); }
[data-fui-comp="ui-gallery"].ui-gallery--gap-sm { --ui-gallery-gap: var(--spacing-sm, 8px); }
[data-fui-comp="ui-gallery"].ui-gallery--gap-lg { --ui-gallery-gap: var(--spacing-lg, 24px); }
[data-fui-comp="ui-gallery"].ui-gallery--gap-xl { --ui-gallery-gap: var(--spacing-xl, 32px); }

/* Columns presets — 1..12. */
[data-fui-comp="ui-gallery"].ui-gallery--cols-1 { --ui-gallery-cols: 1; }
[data-fui-comp="ui-gallery"].ui-gallery--cols-2 { --ui-gallery-cols: 2; }
[data-fui-comp="ui-gallery"].ui-gallery--cols-3 { --ui-gallery-cols: 3; }
[data-fui-comp="ui-gallery"].ui-gallery--cols-4 { --ui-gallery-cols: 4; }
[data-fui-comp="ui-gallery"].ui-gallery--cols-5 { --ui-gallery-cols: 5; }
[data-fui-comp="ui-gallery"].ui-gallery--cols-6 { --ui-gallery-cols: 6; }
[data-fui-comp="ui-gallery"].ui-gallery--cols-7 { --ui-gallery-cols: 7; }
[data-fui-comp="ui-gallery"].ui-gallery--cols-8 { --ui-gallery-cols: 8; }
[data-fui-comp="ui-gallery"].ui-gallery--cols-9 { --ui-gallery-cols: 9; }
[data-fui-comp="ui-gallery"].ui-gallery--cols-10 { --ui-gallery-cols: 10; }
[data-fui-comp="ui-gallery"].ui-gallery--cols-11 { --ui-gallery-cols: 11; }
[data-fui-comp="ui-gallery"].ui-gallery--cols-12 { --ui-gallery-cols: 12; }

/* ── Grid variant (default) ── */
[data-fui-comp="ui-gallery"]:not(.ui-gallery--strip):not(.ui-gallery--masonry) {
  display: grid;
  grid-template-columns: repeat(var(--ui-gallery-cols), 1fr);
  gap: var(--ui-gallery-gap);
}

/* ── Strip variant: horizontal scroll-snap ── */
.ui-gallery--strip {
  display: flex;
  flex-wrap: nowrap;
  overflow-x: auto;
  scroll-snap-type: x mandatory;
  gap: var(--ui-gallery-gap);
  padding-block-end: 2px;
}
.ui-gallery--strip > .ui-gallery__row {
  flex: 0 0 auto;
  inline-size: 240px;
  scroll-snap-align: start;
}

/* ── Masonry: CSS columns flow ── */
.ui-gallery--masonry {
  column-count: var(--ui-gallery-cols);
  column-gap: var(--ui-gallery-gap);
  display: block;
}
.ui-gallery--masonry > .ui-gallery__row {
  break-inside: avoid;
  margin-block-end: var(--ui-gallery-gap);
}

/* ── Caption overlay mode ── */
.ui-gallery--cap-overlay .ui-gallery__figure {
  position: relative;
}
.ui-gallery--cap-overlay .ui-gallery__caption {
  position: absolute;
  inset-inline: 0;
  inset-block-end: 0;
  margin: 0;
  padding: var(--spacing-md, 12px) var(--spacing-sm, 8px) var(--spacing-sm, 8px);
  color: white;
  background: linear-gradient(to top, rgba(0,0,0,0.7), transparent);
  font-size: 0.85rem;
  opacity: 0;
  transition: opacity 150ms ease;
}
.ui-gallery--cap-overlay .ui-gallery__item:hover .ui-gallery__caption,
.ui-gallery--cap-overlay .ui-gallery__item:focus-within .ui-gallery__caption {
  opacity: 1;
}`
}
