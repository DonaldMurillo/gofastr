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

// RatingShape picks the glyph (star or heart).
type RatingShape string

const (
	RatingShapeStar  RatingShape = ""
	RatingShapeHeart RatingShape = "heart"
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
	// Shape picks star (default) or heart.
	Shape RatingShape
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
	case RatingShapeStar, RatingShapeHeart:
	default:
		panic("ui: RatingInput unknown Shape " + string(cfg.Shape) +
			` — pick one of: "" (star), heart`)
	}

	cls := "ui-rating"
	if cfg.Shape == RatingShapeHeart {
		cls += " ui-rating--heart"
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
		items = append(items, render.Tag("label", labelAttrs,
			render.HTML(ratingIcon(cfg.Shape))))
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
	if shape == RatingShapeHeart {
		return `<svg width="24" height="24" viewBox="0 0 24 24" fill="currentColor" xmlns="http://www.w3.org/2000/svg"><path d="M12 21s-7.5-4.5-9.5-9.5C1 8 3 4 6.5 4 9 4 11 6 12 7c1-1 3-3 5.5-3C21 4 23 8 21.5 11.5 19.5 16.5 12 21 12 21z"/></svg>`
	}
	return `<svg width="24" height="24" viewBox="0 0 24 24" fill="currentColor" xmlns="http://www.w3.org/2000/svg"><path d="M12 2l3.09 6.26L22 9.27l-5 4.87 1.18 6.88L12 17.77 5.82 21l1.18-6.88-5-4.87 6.91-1.01z"/></svg>`
}

var ratingStyle = registry.RegisterStyle("ui-rating", ratingCSS)

func ratingCSS(_ style.Theme) string {
	return `[data-fui-comp="ui-rating"] {
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
  /* WCAG 2.5.5 — each star is independently clickable. */
  min-block-size: var(--spacing-touch-target, 44px);
  min-inline-size: var(--spacing-touch-target, 44px);
  color: var(--color-border, #E4E4E7);
  cursor: pointer;
  transition: color 120ms ease, transform 120ms ease;
}
[data-fui-comp="ui-rating"] .ui-rating__star:hover {
  transform: scale(1.08);
}
[data-fui-comp="ui-rating"] .ui-rating__input:focus-visible + .ui-rating__star {
  outline: 2px solid var(--color-primary, #4F46E5);
  outline-offset: 2px;
  border-radius: var(--radii-sm, 4px);
}

/* Highlight: the checked input + every later (in DOM = earlier-in-
   reverse-order = smaller-value) sibling label lights up. */
[data-fui-comp="ui-rating"] .ui-rating__input:checked ~ .ui-rating__star {
  color: var(--color-warning, #F59E0B);
}

/* Hover preview — same cascade but tied to :hover. The :has()
   query lifts the preview onto the wrapper so the cursor doesn't
   need to be on the label to highlight earlier ones. */
[data-fui-comp="ui-rating"]:not(.is-disabled) .ui-rating__star:hover,
[data-fui-comp="ui-rating"]:not(.is-disabled) .ui-rating__star:hover ~ .ui-rating__star {
  color: var(--color-warning, #F59E0B);
}

/* Heart variant — recolor selection / preview to a red/pink. */
.ui-rating--heart .ui-rating__input:checked ~ .ui-rating__star,
.ui-rating--heart:not(.is-disabled) .ui-rating__star:hover,
.ui-rating--heart:not(.is-disabled) .ui-rating__star:hover ~ .ui-rating__star {
  color: var(--color-danger, #DC2626);
}

[data-fui-comp="ui-rating"].is-disabled .ui-rating__star {
  cursor: not-allowed;
  opacity: 0.6;
}`
}
