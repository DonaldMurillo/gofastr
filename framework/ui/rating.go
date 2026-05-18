package ui

import (
	"strconv"

	"github.com/DonaldMurillo/gofastr/core-ui/registry"
	"github.com/DonaldMurillo/gofastr/core-ui/style"
	"github.com/DonaldMurillo/gofastr/core/render"
)

// ─── RatingInput ────────────────────────────────────────────────────
//
// Keyboard-accessible star rating bound to a hidden radio group, so
// it submits via plain form POST without JavaScript. Hover-preview
// uses :has() / sibling selectors — no JS needed.
//
// Markup: <fieldset role=radiogroup> containing <input type=radio>
// + <label> pairs in REVERSE order (5..1). The reverse order plus
// the CSS sibling selector lets the highlight cascade backward from
// the hovered/checked star to all earlier stars without JS.

// RatingShape picks one of the bundled glyphs. For a custom glyph,
// set RatingConfig.Icon instead — Icon overrides Shape.
type RatingShape string

const (
	RatingShapeStar    RatingShape = ""
	RatingShapeHeart   RatingShape = "heart"
	RatingShapeThumb   RatingShape = "thumb"
	RatingShapeFire    RatingShape = "fire"
	RatingShapeDiamond RatingShape = "diamond"
	RatingShapeCircle  RatingShape = "circle"
	RatingShapeSquare  RatingShape = "square"
)

// RatingSize controls the painted glyph size. The tap target stays
// at the --spacing-touch-target floor (44px WCAG 2.5.5) regardless;
// only the SVG glyph inside shrinks or grows.
type RatingSize string

const (
	RatingSizeDefault RatingSize = ""
	RatingSizeSmall   RatingSize = "small"
	RatingSizeLarge   RatingSize = "large"
)

// RatingGap controls the visual gap between stars, independent of
// Size. Useful for compact (tight) inline ratings vs. roomy
// (loose / wide) detail-page ratings.
type RatingGap string

const (
	RatingGapDefault RatingGap = ""      // 2px
	RatingGapTight   RatingGap = "tight" // 0
	RatingGapLoose   RatingGap = "loose" // 8px
	RatingGapWide    RatingGap = "wide"  // 16px
)

// RatingConfig configures a RatingInput.
type RatingConfig struct {
	// Name is the form-field name (required).
	Name string
	// Label is the accessible label (required, used as fieldset
	// legend / radiogroup aria-label).
	Label string
	// Max is the rating ceiling (1..N). Defaults to 5.
	Max int
	// Value is the initial selection (0..Max). 0 = no rating chosen.
	Value int
	// Shape picks one of the bundled glyphs (star/heart/thumb/fire/
	// diamond/circle/square). Ignored when Icon is set.
	Shape RatingShape
	// Icon is a caller-supplied monochrome SVG (or any render.HTML)
	// used in place of the bundled Shape glyph. The fill / stroke
	// inside should use currentColor so the selected-state highlight
	// works. Cloned into every star.
	Icon render.HTML
	// Size picks the icon glyph size. Default=24px, Small=16px,
	// Large=32px. Tap target stays at the WCAG floor regardless.
	Size RatingSize
	// Gap picks the visual spacing between stars. Default keeps the
	// AAA 44×44 tap target per star (glyphs ~22px apart). Tight
	// shrinks the inline tap zone to glyph+8px so adjacent glyphs
	// nearly touch (~8px gap) — relaxes AAA to AA (24px floor) for
	// dense inline ratings. Loose / Wide widen the gap without
	// touching the tap zone. Independent of Size.
	Gap RatingGap
	// Disabled disables all radios.
	Disabled bool
	ID       string
	Class    string
}

// RatingInput renders a star/heart rating bound to a hidden radio
// group. Submits as Name=<1..Max> on the surrounding form.
func RatingInput(cfg RatingConfig) render.HTML {
	if cfg.Name == "" {
		panic("ui: RatingInput requires Name")
	}
	if cfg.Label == "" {
		panic("ui: RatingInput requires Label")
	}
	max := cfg.Max
	if max == 0 {
		max = 5
	}
	if max < 1 {
		panic("ui: RatingInput Max must be >= 1")
	}
	switch cfg.Shape {
	case RatingShapeStar, RatingShapeHeart, RatingShapeThumb,
		RatingShapeFire, RatingShapeDiamond, RatingShapeCircle,
		RatingShapeSquare:
	default:
		panic("ui: RatingInput unknown Shape " + string(cfg.Shape) +
			` — pick one of: "" (star), heart, thumb, fire, diamond, circle, square`)
	}
	switch cfg.Size {
	case RatingSizeDefault, RatingSizeSmall, RatingSizeLarge:
	default:
		panic("ui: RatingInput unknown Size " + string(cfg.Size) +
			` — pick one of: "" (default), small, large`)
	}
	switch cfg.Gap {
	case RatingGapDefault, RatingGapTight, RatingGapLoose, RatingGapWide:
	default:
		panic("ui: RatingInput unknown Gap " + string(cfg.Gap) +
			` — pick one of: "" (default), tight, loose, wide`)
	}

	cls := "ui-rating"
	// Apply a shape-specific class so per-shape color overrides (e.g.
	// heart → danger red, fire → danger, thumb → primary) can theme
	// without the caller picking colors. Only the bundled Shape is
	// considered — when Icon is set, the user is on their own.
	if cfg.Icon == "" && cfg.Shape != RatingShapeStar {
		cls += " ui-rating--" + string(cfg.Shape)
	}
	if cfg.Size != RatingSizeDefault {
		cls += " ui-rating--" + string(cfg.Size)
	}
	if cfg.Gap != RatingGapDefault {
		cls += " ui-rating--gap-" + string(cfg.Gap)
	}
	if cfg.Disabled {
		cls += " is-disabled"
	}
	if cfg.Class != "" {
		cls += " " + cfg.Class
	}
	fsAttrs := map[string]string{"class": cls, "role": "radiogroup", "aria-label": cfg.Label}
	if cfg.ID != "" {
		fsAttrs["id"] = cfg.ID
	}

	// Render in REVERSE order so the CSS ~ sibling selector can
	// cascade highlight from the checked/hovered radio backward.
	items := make([]render.HTML, 0, max*2)
	for i := max; i >= 1; i-- {
		idV := cfg.Name + "-" + strconv.Itoa(i)
		inputAttrs := map[string]string{
			"type":  "radio",
			"name":  cfg.Name,
			"id":    idV,
			"value": strconv.Itoa(i),
			"class": "ui-rating__input",
		}
		if cfg.Value == i {
			inputAttrs["checked"] = ""
		}
		if cfg.Disabled {
			inputAttrs["disabled"] = ""
		}
		items = append(items, render.Tag("input", inputAttrs))
		labelAttrs := map[string]string{
			"for":        idV,
			"class":      "ui-rating__star",
			"aria-label": pluralStars(i),
		}
		glyph := cfg.Icon
		if glyph == "" {
			glyph = render.HTML(ratingIcon(cfg.Shape))
		}
		items = append(items, render.Tag("label", labelAttrs, glyph))
	}

	return ratingStyle.WrapHTML(render.Tag("fieldset", fsAttrs, items...))
}

func pluralStars(n int) string {
	if n == 1 {
		return "1 star"
	}
	return strconv.Itoa(n) + " stars"
}

func ratingIcon(shape RatingShape) string {
	switch shape {
	case RatingShapeHeart:
		return `<svg viewBox="0 0 24 24" fill="currentColor" xmlns="http://www.w3.org/2000/svg"><path d="M12 21s-7.5-4.5-9.5-9.5C1 8 3 4 6.5 4 9 4 11 6 12 7c1-1 3-3 5.5-3C21 4 23 8 21.5 11.5 19.5 16.5 12 21 12 21z"/></svg>`
	case RatingShapeThumb:
		return `<svg viewBox="0 0 24 24" fill="currentColor" xmlns="http://www.w3.org/2000/svg"><path d="M7 21H4a1 1 0 01-1-1v-9a1 1 0 011-1h3v11zm14-10a2 2 0 00-2-2h-6.31l.95-4.57.03-.32a1.5 1.5 0 00-.44-1.06L12.17 2 5.59 8.59A2 2 0 005 10v9a2 2 0 002 2h9c.83 0 1.54-.5 1.84-1.22l3.02-7.05c.09-.23.14-.47.14-.73v-1z"/></svg>`
	case RatingShapeFire:
		return `<svg viewBox="0 0 24 24" fill="currentColor" xmlns="http://www.w3.org/2000/svg"><path d="M13.5.67s.74 2.65.74 4.8c0 2.06-1.35 3.73-3.41 3.73-2.07 0-3.63-1.67-3.63-3.73l.03-.36C5.21 7.51 4 10.62 4 14a8 8 0 0016 0c0-4.16-2-7.88-6.5-13.33zM11.71 19c-1.78 0-3.22-1.4-3.22-3.14 0-1.62 1.05-2.76 2.81-3.12 1.77-.36 3.6-1.21 4.62-2.58.39 1.29.59 2.65.59 4.04 0 2.65-2.15 4.8-4.8 4.8z"/></svg>`
	case RatingShapeDiamond:
		return `<svg viewBox="0 0 24 24" fill="currentColor" xmlns="http://www.w3.org/2000/svg"><path d="M19 3H5L2 9l10 12L22 9l-3-6zM6.5 5h11l1.74 3.48H4.76L6.5 5zM12 18.6L4.83 10h14.34L12 18.6z"/></svg>`
	case RatingShapeCircle:
		return `<svg viewBox="0 0 24 24" fill="currentColor" xmlns="http://www.w3.org/2000/svg"><circle cx="12" cy="12" r="9"/></svg>`
	case RatingShapeSquare:
		return `<svg viewBox="0 0 24 24" fill="currentColor" xmlns="http://www.w3.org/2000/svg"><rect x="3" y="3" width="18" height="18" rx="2"/></svg>`
	default: // star
		return `<svg viewBox="0 0 24 24" fill="currentColor" xmlns="http://www.w3.org/2000/svg"><path d="M12 2l3.09 6.26L22 9.27l-5 4.87 1.18 6.88L12 17.77 5.82 21l1.18-6.88-5-4.87 6.91-1.01z"/></svg>`
	}
}

var ratingStyle = registry.RegisterStyle("ui-rating", ratingCSS)

func ratingCSS(_ style.Theme) string {
	return `[data-fui-comp="ui-rating"] {
  --ui-rating-glyph: 24px;
  --ui-rating-cell: var(--spacing-touch-target, 44px);
  --ui-rating-color: var(--color-warning, #F59E0B);
  display: inline-flex;
  /* Flex-direction:row-reverse turns our reverse-DOM order back
     into 1..N visual order, while keeping the ~ sibling cascade. */
  flex-direction: row-reverse;
  justify-content: flex-end;
  gap: 2px;
  margin: 0;
  padding: 0;
  border: 0;
}
[data-fui-comp="ui-rating"] .ui-rating__input {
  /* Visually hidden; clicking the label activates the input. */
  position: absolute;
  width: 1px;
  height: 1px;
  margin: -1px;
  padding: 0;
  border: 0;
  clip: rect(0,0,0,0);
  overflow: hidden;
  white-space: nowrap;
}
[data-fui-comp="ui-rating"] .ui-rating__star {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  /* Block axis stays at the AAA touch-target floor; inline axis is
     driven by --ui-rating-cell which Gap variants can shrink for
     tighter density. */
  min-block-size: var(--spacing-touch-target, 44px);
  min-inline-size: var(--ui-rating-cell);
  color: var(--color-border, #E4E4E7);
  cursor: pointer;
  transition: color 120ms ease, transform 120ms ease;
}
/* Glyph (svg) size is driven by a custom property so size variants
   only have to override the property, not duplicate the rule. */
[data-fui-comp="ui-rating"] .ui-rating__star svg {
  width: var(--ui-rating-glyph, 24px);
  height: var(--ui-rating-glyph, 24px);
}
[data-fui-comp="ui-rating"].ui-rating--small { --ui-rating-glyph: 16px; }
[data-fui-comp="ui-rating"].ui-rating--large { --ui-rating-glyph: 32px; }

/* Gap presets — independent of Size.
   Default keeps the WCAG 2.5.5 AAA tap-target floor (44×44 per star).
   Tight shrinks the inline tap zone to glyph+8px so adjacent glyphs
   actually touch — the block axis stays 44px and the inline zone
   stays ≥24px (WCAG 2.5.8 AA), but AAA is intentionally relaxed for
   dense inline ratings. */
[data-fui-comp="ui-rating"].ui-rating--gap-tight {
  --ui-rating-cell: max(24px, calc(var(--ui-rating-glyph) + 8px));
  gap: 0;
}
[data-fui-comp="ui-rating"].ui-rating--gap-loose { gap: 8px; }
[data-fui-comp="ui-rating"].ui-rating--gap-wide { gap: 20px; }
[data-fui-comp="ui-rating"] .ui-rating__star:hover {
  transform: scale(1.08);
}
[data-fui-comp="ui-rating"] .ui-rating__input:focus-visible + .ui-rating__star {
  outline: 2px solid var(--color-primary, #4F46E5);
  outline-offset: 2px;
  border-radius: var(--radii-sm, 4px);
}

/* Highlight: the checked input + every later (in DOM = earlier-in-
   reverse-order = smaller-value) sibling label lights up. Color is
   driven by --ui-rating-color so per-shape variants and per-instance
   overrides can recolor without writing new highlight rules. */
[data-fui-comp="ui-rating"] .ui-rating__input:checked ~ .ui-rating__star,
[data-fui-comp="ui-rating"]:not(.is-disabled) .ui-rating__star:hover,
[data-fui-comp="ui-rating"]:not(.is-disabled) .ui-rating__star:hover ~ .ui-rating__star {
  color: var(--ui-rating-color);
}

/* Per-shape color overrides — heart / fire feel red, thumb feels
   primary, diamond feels info. Star (default) and circle / square
   stay on the warning yellow. */
.ui-rating--heart   { --ui-rating-color: var(--color-danger, #DC2626); }
.ui-rating--fire    { --ui-rating-color: var(--color-danger, #DC2626); }
.ui-rating--thumb   { --ui-rating-color: var(--color-primary, #4F46E5); }
.ui-rating--diamond { --ui-rating-color: var(--color-info, #3B82F6); }

[data-fui-comp="ui-rating"].is-disabled .ui-rating__star {
  cursor: not-allowed;
  opacity: 0.6;
}`
}
