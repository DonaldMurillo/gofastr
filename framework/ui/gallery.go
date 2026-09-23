package ui

import (
	"strconv"

	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core-ui/registry"
	"github.com/DonaldMurillo/gofastr/core-ui/style"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework/headless"
)

// ─── Gallery ────────────────────────────────────────────────────────
//
// Standalone thumbnail surface. Three visual variants share the same
// Item shape:
//
//   - GalleryGrid:     CSS Grid, `Columns` × `Gap`. The default.
//   - GalleryStrip:    horizontal scroll-snap row.
//   - GalleryMasonry:  CSS `columns: <n>` so tiles flow at natural
//                      aspect (Pinterest-style).
//
// Click behaviour (pick ONE; otherwise the anchor opens Src in a new
// tab as the no-JS fallback):
//
//   - Lightbox:  name of a paired framework/ui.Lightbox. Each item
//                emits data-fui-open + data-fui-deeplink so clicking
//                opens the overlay with the matching image.
//   - HrefFn:    a function returning a per-item URL the anchor
//                navigates to. Use for "click photo → detail page".
//
// Caption rendering is controlled by CaptionMode.
//
// The structure renders through headless.Gallery with the fui-gallery
// class map: a captioned item is li > figure > (a > img, figcaption) —
// the caption sits beside the link, so the link's accessible name is
// the image's alt alone and the caption text is not a click target —
// and a captionless item is a plain li > a > img.

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
	// Alt is the accessible image description (required: empty Alt
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
	// Columns (Grid mode): the MAXIMUM number of columns. Default 3.
	// The grid is responsive: tracks never shrink below --ui-gallery-min
	// (default 9.5rem, overridable per instance via a Class), so narrow
	// viewports automatically get fewer columns without media queries.
	// Masonry mode: uses this as the maximum column count too.
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
	// ID / Class are passed through to the wrapper.
	ID    string
	Class string
	// ExtraAttrs forwards additional attributes (data-* test hooks,
	// analytics markers, ARIA overrides) to the gallery's root <ul>.
	// Keys the component owns are dropped: class and id (use Class /
	// ID), data-fui-*, and aria-label (use Label).
	ExtraAttrs html.Attrs
}

// Gallery renders the thumbnail surface through headless.Gallery with
// the fui-gallery class map (the sheet name "ui-gallery" and marker
// stay). The thumb loads lazily through the parts seam; the default
// and lightbox anchors open the full image in a new tab through the
// per-item attrs, so the no-script path keeps the old behaviour.
func Gallery(cfg GalleryConfig) render.HTML {
	if len(cfg.Items) == 0 {
		panic("ui: Gallery requires ≥1 Item")
	}
	switch cfg.Variant {
	case GalleryGrid, GalleryStrip, GalleryMasonry:
	default:
		panic("ui: Gallery unknown Variant " + string(cfg.Variant) +
			`. Pick one of: "" (grid), strip, masonry`)
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

	rootCls := "fui-gallery"
	if cfg.Variant != GalleryGrid {
		rootCls += " fui-gallery--" + string(cfg.Variant)
	}
	if cfg.CaptionMode != GalleryCaptionBelow {
		rootCls += " fui-gallery--cap-" + string(cfg.CaptionMode)
	}
	if gap != "" {
		rootCls += " fui-gallery--gap-" + string(gap)
	}
	// Columns maps to a precomputed .fui-gallery--cols-<n> class that
	// sets --ui-gallery-cols, no inline style needed (CSP). Grid and
	// masonry cap at 12 for the precomputed class set; very wide grids
	// fall back to 12.
	if cfg.Variant != GalleryStrip {
		rootCls += " fui-gallery--cols-" + strconv.Itoa(min(max(cols, 1), 12))
	}
	if cfg.Class != "" {
		rootCls += " " + cfg.Class
	}

	classes := headless.Classes{
		headless.PartRoot:    rootCls,
		headless.PartControl: "fui-gallery__row",
		headless.PartHeader:  "fui-gallery__figure",
		headless.PartLabel:   "fui-gallery__item",
		headless.PartBody:    "fui-gallery__thumb",
		headless.PartText:    "fui-gallery__caption",
	}

	items := make([]headless.GalleryItem, len(cfg.Items))
	for i, it := range cfg.Items {
		caption := it.Caption
		if cfg.CaptionMode == GalleryCaptionOff {
			caption = ""
		}
		items[i] = headless.GalleryItem{
			Src: it.Src, Thumb: it.Thumb, Alt: it.Alt,
			Caption: caption, Width: it.Width, Height: it.Height,
		}
	}

	// The default and lightbox anchors open the full image in a new
	// tab; an HrefFn anchor goes where the caller said.
	var perItem map[int]html.Attrs
	if cfg.HrefFn == nil {
		perItem = make(map[int]html.Attrs, len(items))
		for i := range items {
			perItem[i] = html.Attrs{"target": "_blank", "rel": "noopener"}
		}
	}

	var hrefFn func(i int, it headless.GalleryItem) string
	if cfg.HrefFn != nil && cfg.Lightbox == "" {
		hrefFn = func(i int, it headless.GalleryItem) string {
			return cfg.HrefFn(i, cfg.Items[i])
		}
	}

	var lightbox headless.GalleryLightbox
	if cfg.Lightbox != "" {
		group := cfg.ID
		if group == "" {
			group = cfg.Lightbox + "-gallery"
		}
		lightbox = headless.GalleryLightbox{Name: cfg.Lightbox, Group: group}
	}

	out := headless.Gallery(headless.GalleryProps{
		Items:    items,
		Label:    label,
		HrefFn:   hrefFn,
		Lightbox: lightbox,
		// The thumb is below the fold as often as not: lazy loading is
		// presentation, so it rides the parts seam rather than a prop.
		ExtraAttrsPerItem: perItem,
		ID:                cfg.ID,
		ExtraAttrs:        headless.Safe(cfg.ExtraAttrs, "aria-label"),
		Parts: headless.Parts{Attrs: headless.PartAttrs{
			headless.PartBody: {"loading": "lazy"},
		}},
	}, classes)
	return galleryStyle.WrapHTML(out)
}

var galleryStyle = registry.RegisterStyle("ui-gallery", galleryCSS)

func galleryCSS(_ style.Theme) string {
	return `[data-fui-comp="ui-gallery"] {
  list-style: none;
  margin: 0;
  padding: 0;
  --ui-gallery-cols: 3;
  --ui-gallery-min: 9.5rem;
  --ui-gallery-gap: var(--spacing-md, 8px);
}
[data-fui-comp="ui-gallery"] .fui-gallery__row {
  margin: 0;
  padding: 0;
}
[data-fui-comp="ui-gallery"] .fui-gallery__item {
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
[data-fui-comp="ui-gallery"] .fui-gallery__item:hover {
  border-color: var(--color-primary, #4F46E5);
}
[data-fui-comp="ui-gallery"] .fui-gallery__item:focus-visible {
  outline: 2px solid var(--color-primary, #4F46E5);
  outline-offset: 2px;
}
[data-fui-comp="ui-gallery"] .fui-gallery__figure {
  margin: 0;
  display: grid;
  gap: var(--spacing-xs, 2px);
}
[data-fui-comp="ui-gallery"] .fui-gallery__thumb {
  display: block;
  inline-size: 100%;
  block-size: auto;
  object-fit: cover;
}
[data-fui-comp="ui-gallery"] .fui-gallery__caption {
  margin: 0;
  padding: var(--spacing-sm, 4px) var(--spacing-sm, 4px) var(--spacing-sm, 4px);
  font-size: var(--text-sm, 0.875rem);
  color: var(--color-text-muted, #52525B);
}

/* Gap presets. */
[data-fui-comp="ui-gallery"].fui-gallery--gap-xs { --ui-gallery-gap: var(--spacing-xs, 2px); }
[data-fui-comp="ui-gallery"].fui-gallery--gap-sm { --ui-gallery-gap: var(--spacing-sm, 4px); }
[data-fui-comp="ui-gallery"].fui-gallery--gap-lg { --ui-gallery-gap: var(--spacing-lg, 16px); }
[data-fui-comp="ui-gallery"].fui-gallery--gap-xl { --ui-gallery-gap: var(--spacing-xl, 24px); }

/* Columns presets — 1..12. */
[data-fui-comp="ui-gallery"].fui-gallery--cols-1 { --ui-gallery-cols: 1; }
[data-fui-comp="ui-gallery"].fui-gallery--cols-2 { --ui-gallery-cols: 2; }
[data-fui-comp="ui-gallery"].fui-gallery--cols-3 { --ui-gallery-cols: 3; }
[data-fui-comp="ui-gallery"].fui-gallery--cols-4 { --ui-gallery-cols: 4; }
[data-fui-comp="ui-gallery"].fui-gallery--cols-5 { --ui-gallery-cols: 5; }
[data-fui-comp="ui-gallery"].fui-gallery--cols-6 { --ui-gallery-cols: 6; }
[data-fui-comp="ui-gallery"].fui-gallery--cols-7 { --ui-gallery-cols: 7; }
[data-fui-comp="ui-gallery"].fui-gallery--cols-8 { --ui-gallery-cols: 8; }
[data-fui-comp="ui-gallery"].fui-gallery--cols-9 { --ui-gallery-cols: 9; }
[data-fui-comp="ui-gallery"].fui-gallery--cols-10 { --ui-gallery-cols: 10; }
[data-fui-comp="ui-gallery"].fui-gallery--cols-11 { --ui-gallery-cols: 11; }
[data-fui-comp="ui-gallery"].fui-gallery--cols-12 { --ui-gallery-cols: 12; }

/* ── Grid variant (default) ──
   --ui-gallery-cols is a MAXIMUM: the calc() term sizes tracks for exactly
   that many columns, and the max() floor (--ui-gallery-min) makes auto-fill
   wrap to fewer columns when tracks would get narrower — responsive with no
   media queries. */
[data-fui-comp="ui-gallery"]:not(.fui-gallery--strip):not(.fui-gallery--masonry) {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(min(100%, max(var(--ui-gallery-min), calc((100% - (var(--ui-gallery-cols) - 1) * var(--ui-gallery-gap)) / var(--ui-gallery-cols)))), 1fr));
  gap: var(--ui-gallery-gap);
}

/* ── Strip variant: horizontal scroll-snap ── */
.fui-gallery--strip {
  display: flex;
  flex-wrap: nowrap;
  overflow-x: auto;
  scroll-snap-type: x mandatory;
  gap: var(--ui-gallery-gap);
  padding-block-end: var(--spacing-xs, 2px);
}
.fui-gallery--strip > .fui-gallery__row {
  flex: 0 0 auto;
  inline-size: 240px;
  scroll-snap-align: start;
}

/* ── Masonry: CSS columns flow ──
   With both column-width and column-count set, count is a maximum and the
   browser drops columns as the container narrows — same responsive contract
   as the grid variant. */
.fui-gallery--masonry {
  column-width: var(--ui-gallery-min);
  column-count: var(--ui-gallery-cols);
  column-gap: var(--ui-gallery-gap);
  display: block;
}
.fui-gallery--masonry > .fui-gallery__row {
  break-inside: avoid;
  margin-block-end: var(--ui-gallery-gap);
}

/* ── Caption overlay mode ── */
.fui-gallery--cap-overlay .fui-gallery__figure {
  position: relative;
}
.fui-gallery--cap-overlay .fui-gallery__caption {
  position: absolute;
  inset-inline: 0;
  inset-block-end: 0;
  margin: 0;
  padding: var(--spacing-md, 8px) var(--spacing-sm, 4px) var(--spacing-sm, 4px);
  color: white;
  background: linear-gradient(to top, rgba(0,0,0,0.7), transparent);
  font-size: var(--text-sm, 0.875rem);
  opacity: 0;
  transition: opacity 150ms ease;
}
/* The caption is the anchor's sibling in the primitive's markup, so
   the row (the li) carries the hover/focus-within surface. */
.fui-gallery--cap-overlay .fui-gallery__row:hover .fui-gallery__caption,
.fui-gallery--cap-overlay .fui-gallery__row:focus-within .fui-gallery__caption {
  opacity: 1;
}`
}
