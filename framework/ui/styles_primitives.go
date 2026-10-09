package ui

import (
	"fmt"
	"strings"

	"github.com/DonaldMurillo/gofastr/core-ui/registry"
	"github.com/DonaldMurillo/gofastr/core-ui/style"
)

// Style handles for the 10 layout / primitive components added in
// the same commit. Each registers a scoped stylesheet that the SSR
// host and runtime load on first appearance, except where
// LoadAlways is explicitly opted into for chrome that's on every
// screen.

var (
	layoutStyle      = registry.RegisterStyle("ui-layout", layoutCSS)
	stickyStyle      = registry.RegisterStyle("ui-sticky", stickyCSS)
	aspectRatioStyle = registry.RegisterStyle("ui-aspect-ratio", aspectRatioCSS)
	cardStyle        = registry.RegisterStyle("ui-card", cardCSS)
	imageStyle       = registry.RegisterStyle("ui-image", imageCSS)
	toggleStyle      = registry.RegisterStyle("ui-toggle", toggleCSS)
	tooltipStyle     = registry.RegisterStyle("ui-tooltip", tooltipCSS)
	tagStyle         = registry.RegisterStyle("ui-tag", tagCSS)
	spinnerStyle     = registry.RegisterStyle("ui-spinner", spinnerCSS)
	dividerStyle     = registry.RegisterStyle("ui-divider", dividerCSS)
)

// ─── Layout ─────────────────────────────────────────────────────────

// gridMinStepsCSS maps every whole-rem data-min from 1rem to 64rem onto
// --ui-grid-min, the fallback for browsers without typed attr(). A Min
// outside the ladder (px, fractional rem) gets the 16rem default there.
func gridMinStepsCSS() string {
	var b strings.Builder
	for n := 1; n <= 64; n++ {
		fmt.Fprintf(&b, "[data-cui-comp=\"ui-layout\"].fui-grid[data-min=\"%drem\"] { --ui-grid-min: %drem; }\n", n, n)
	}
	return b.String()
}

func layoutCSS(_ style.Theme) string {
	// Layout primitives stack data-cui-comp="ui-layout" AND their own
	// class on the SAME element (Stack, Cluster, Grid, … are sibling
	// classes, not descendants). Selectors below combine the marker
	// and class on the same element via `[data-cui-comp="ui-layout"].<class>`
	// rather than the descendant `[data-cui-comp="ui-layout"] .<class>`.
	return `[data-cui-comp="ui-layout"] {
  box-sizing: border-box;
}
[data-cui-comp="ui-layout"].fui-stack--trim-margins > * { margin-block: 0; }
/* Screen: the page column — at least one viewport tall, last child
   pushed to the bottom, so a short page keeps its footer at the
   bottom of the screen. margin-block-start:auto (not justify-content)
   so earlier children and their gaps keep their rhythm. */
:where([data-cui-comp="ui-layout"]).fui-stack--screen { min-block-size: 100dvh; }
[data-cui-comp="ui-layout"].fui-stack--screen > :last-child { margin-block-start: auto; }
:where([data-cui-comp="ui-layout"]).fui-stack {
  display: flex;
  flex-direction: column;
  gap: var(--ui-layout-gap-md, var(--spacing-md, 8px));
}
:where([data-cui-comp="ui-layout"]).fui-cluster {
  display: flex;
  flex-direction: row;
  flex-wrap: wrap;
  gap: var(--ui-layout-gap-md, var(--spacing-md, 8px));
  align-items: center;
}
[data-cui-comp="ui-layout"].fui-cluster--nowrap { flex-wrap: nowrap; }
/* Shrink: the row narrows below its content, the last child takes the
   squeeze, and the earlier children keep their size. */
:where([data-cui-comp="ui-layout"]).fui-cluster--shrink { min-inline-size: 0; }
[data-cui-comp="ui-layout"].fui-cluster--shrink > :not(:last-child) { flex-shrink: 0; }
[data-cui-comp="ui-layout"].fui-cluster--shrink > :last-child { min-inline-size: 0; }

:where([data-cui-comp="ui-layout"]).fui-grid {
  display: grid;
  gap: var(--ui-layout-gap-md, var(--spacing-md, 8px));
  /* min(…, 100%): a column minimum wider than the container (a 24rem
     card grid on a phone) shrinks to the container instead of
     overflowing it. */
  grid-template-columns: repeat(auto-fit, minmax(min(var(--ui-grid-min, 16rem), 100%), 1fr));
}
[data-cui-comp="ui-layout"].fui-grid--fill {
  grid-template-columns: repeat(auto-fill, minmax(min(var(--ui-grid-min, 16rem), 100%), 1fr));
}
/* GridConfig.Min rides on data-min (no inline style under the CSP).
   Where typed attr() is supported the attribute IS the minimum, any
   length; elsewhere the whole-rem steps below cover 1rem–64rem, the
   range the framework and its generators emit. */
@supports (width: attr(data-min type(<length>))) {
  [data-cui-comp="ui-layout"].fui-grid[data-min] { --ui-grid-min: attr(data-min type(<length>), 16rem); }
}
` + gridMinStepsCSS() + `

:where([data-cui-comp="ui-layout"]).fui-center {
  display: flex;
  align-items: center;
  justify-content: center;
}
:where([data-cui-comp="ui-layout"]).fui-center--viewport { min-block-size: 100vh; }
:where([data-cui-comp="ui-layout"]).fui-center--screen   { min-block-size: 100dvh; }

:where([data-cui-comp="ui-layout"]).fui-spacer {
  flex: 1 1 auto;
  align-self: stretch;
}

[data-cui-comp="ui-layout"].fui-box { background: transparent; }
[data-cui-comp="ui-layout"].fui-box--surface {
  background: var(--color-surface, #FFFFFF);
  border-radius: var(--radii-md, 8px);
}
[data-cui-comp="ui-layout"].fui-box--outlined {
  border: var(--stroke-thin, 1px) solid var(--color-border, #E4E4E7);
  border-radius: var(--radii-md, 8px);
}
[data-cui-comp="ui-layout"].fui-box--pad-sm { padding: var(--spacing-sm, 4px); }
[data-cui-comp="ui-layout"].fui-box--pad-md { padding: var(--spacing-md, 8px); }
[data-cui-comp="ui-layout"].fui-box--pad-lg { padding: var(--spacing-lg, 16px); }
[data-cui-comp="ui-layout"].fui-box--pad-xl { padding: var(--spacing-xl, 24px); }

/* gap modifiers — apply to ui-stack/ui-cluster/ui-grid. Knobs:
   --ui-layout-gap-xs/-sm/-md/-lg/-xl/-2xl, each defaulting to its
   spacing token. A gap is the room between things, not the padding
   inside them, so a theme whose cards cast a hard offset shadow widens
   the gaps without inflating every padding on the spacing scale. */
[data-cui-comp="ui-layout"].fui-layout--gap-none { gap: 0; }
[data-cui-comp="ui-layout"].fui-layout--gap-xs   { gap: var(--ui-layout-gap-xs, var(--spacing-xs, 2px)); }
[data-cui-comp="ui-layout"].fui-layout--gap-sm   { gap: var(--ui-layout-gap-sm, var(--spacing-sm, 4px)); }
[data-cui-comp="ui-layout"].fui-layout--gap-lg   { gap: var(--ui-layout-gap-lg, var(--spacing-lg, 16px)); }
[data-cui-comp="ui-layout"].fui-layout--gap-xl   { gap: var(--ui-layout-gap-xl, var(--spacing-xl, 24px)); }
[data-cui-comp="ui-layout"].fui-layout--gap-2xl  { gap: var(--ui-layout-gap-2xl, var(--spacing-2xl, 32px)); }

/* alignment modifiers. */
[data-cui-comp="ui-layout"].fui-layout--align-start    { align-items: flex-start; }
[data-cui-comp="ui-layout"].fui-layout--align-center   { align-items: center; }
[data-cui-comp="ui-layout"].fui-layout--align-end      { align-items: flex-end; }
[data-cui-comp="ui-layout"].fui-layout--align-baseline { align-items: baseline; }
[data-cui-comp="ui-layout"].fui-layout--align-stretch  { align-items: stretch; }

[data-cui-comp="ui-layout"].fui-layout--justify-start   { justify-content: flex-start; }
[data-cui-comp="ui-layout"].fui-layout--justify-center  { justify-content: center; }
[data-cui-comp="ui-layout"].fui-layout--justify-end     { justify-content: flex-end; }
[data-cui-comp="ui-layout"].fui-layout--justify-between { justify-content: space-between; }
[data-cui-comp="ui-layout"].fui-layout--justify-around  { justify-content: space-around; }`
}

// ─── Card ───────────────────────────────────────────────────────────

func cardCSS(t style.Theme) string {
	return `[data-cui-comp="ui-card"] {
  display: flex;
  flex-direction: column;
  background: var(--color-surface);
  color: var(--color-text);
  border: var(--stroke-thin, 1px) solid var(--color-border);
  border-radius: var(--radii-xl);
  box-shadow: var(--shadow-xs);
  overflow: hidden;
  text-decoration: none;
}
[data-cui-comp="ui-card"].fui-card--outlined {
  box-shadow: none;
}
[data-cui-comp="ui-card"].fui-card--flat {
  box-shadow: none;
  border-color: transparent;
  background: transparent;
}
[data-cui-comp="ui-card"].fui-card--interactive {
  transition: border-color var(--duration-fast, 150ms) var(--easing-ease-in-out, ease),
              box-shadow var(--duration-fast, 150ms) ease,
              translate var(--duration-fast, 150ms) ease;
  cursor: pointer;
}
/* Knobs: --ui-card-hover-shadow / -active-shadow and
   --ui-card-hover-translate / -active-translate, each falling back to
   the shared --ui-press-* knob, then to the state before it. */
[data-cui-comp="ui-card"].fui-card--interactive:hover {
  border-color: var(--color-border-strong);
  box-shadow: var(--ui-card-hover-shadow, var(--ui-press-hover-shadow, var(--shadow-sm)));
  translate: var(--ui-card-hover-translate, var(--ui-press-hover-translate, none));
}
[data-cui-comp="ui-card"].fui-card--interactive:active {
  box-shadow: var(--ui-card-active-shadow, var(--ui-press-active-shadow, var(--ui-card-hover-shadow, var(--ui-press-hover-shadow, var(--shadow-sm)))));
  translate: var(--ui-card-active-translate, var(--ui-press-active-translate, var(--ui-card-hover-translate, var(--ui-press-hover-translate, none))));
}
[data-cui-comp="ui-card"].fui-card--interactive[aria-current="page"] {
  background: var(--color-surface-soft);
  border-color: var(--color-text);
}
[data-cui-comp="ui-card"].fui-card--interactive:focus-visible {
  outline: var(--stroke-focus, 2px) solid var(--color-text-subtle);
  outline-offset: var(--stroke-focus-offset, 2px);
}
[data-cui-comp="ui-card"] .fui-card__inner {
  display: flex;
  flex-direction: column;
  flex: 1 1 auto;
}
[data-cui-comp="ui-card"] .fui-card__header {
  padding: var(--spacing-xl, 24px) var(--spacing-xl, 24px) 0;
  display: flex;
  flex-direction: column;
  gap: calc(var(--spacing-sm, 4px) * 1.5);
}
/* The header pads its own bottom when no body follows it: it is the
   last child, or the always-drawn body slot after it is empty (with or
   without a footer below). */
[data-cui-comp="ui-card"] .fui-card__header:last-child,
[data-cui-comp="ui-card"] .fui-card__header:has(+ .fui-card__body:empty) { padding-block-end: var(--spacing-xl, 24px); }
/* With an Action the header is two columns: the heading and its
   description, then the control at the end, level with the heading. */
[data-cui-comp="ui-card"] .fui-card__header:has(> .fui-card__action) {
  display: grid;
  grid-template-columns: minmax(0, 1fr) auto;
  column-gap: var(--spacing-md, 8px);
}
[data-cui-comp="ui-card"] .fui-card__action {
  grid-column: 2;
  grid-row: 1 / span 2;
  align-self: start;
  display: flex;
  align-items: center;
  gap: var(--spacing-sm, 4px);
}
[data-cui-comp="ui-card"] .fui-card__heading {
  margin: 0;
  font-size: var(--text-base, 1rem);
  font-weight: var(--font-weight-semibold);
  letter-spacing: var(--tracking-snug, -0.01em);
  line-height: calc(var(--leading-snug, 1.4) - 0.1);
  color: var(--color-text);
}
[data-cui-comp="ui-card"] .fui-card__description {
  margin: 0;
  font-size: var(--text-sm, 0.875rem);
  color: var(--color-text-muted);
}
[data-cui-comp="ui-card"] .fui-card__body {
  padding: var(--spacing-xl, 24px);
  color: var(--color-text);
  flex: 1 1 auto;
}
/* The primitive always draws the body node — it is where an island
   swap lands — so the sheet collapses it when empty: a card with no
   body content gains no space. */
[data-cui-comp="ui-card"] .fui-card__body:empty { display: none; }
[data-cui-comp="ui-card"] .fui-card__header + .fui-card__body {
  padding-top: var(--spacing-lg, 16px);
}
[data-cui-comp="ui-card"] .fui-card__footer {
  margin-top: auto;
  padding: var(--spacing-lg, 16px) var(--spacing-xl, 24px);
  border-top: var(--stroke-thin, 1px) solid var(--color-border);
  display: flex;
  align-items: center;
  gap: var(--spacing-md, 8px);
}
[data-cui-comp="ui-card"].fui-card--row { box-shadow: none; border-color: transparent; border-radius: var(--radii-sm); background: transparent; overflow: visible; }
[data-cui-comp="ui-card"].fui-card--row.fui-card--interactive:hover { transform: none; translate: none; box-shadow: none; background: var(--color-surface-soft); }
[data-cui-comp="ui-card"].fui-card--row.fui-card--interactive:active { translate: none; box-shadow: none; }
[data-cui-comp="ui-card"].fui-card--row .fui-card__inner { display: grid; grid-template-columns: minmax(0, 1fr) auto; align-items: center; gap: var(--spacing-xs); padding: var(--spacing-xs) var(--spacing-sm); }
[data-cui-comp="ui-card"].fui-card--row .fui-card__header { display: contents; }
[data-cui-comp="ui-card"].fui-card--row .fui-card__heading { grid-column: 1; grid-row: 1; font-size: var(--text-xs); font-weight: var(--font-weight-normal); color: var(--color-text-muted); }
[data-cui-comp="ui-card"].fui-card--row .fui-card__description { grid-column: 1 / -1; grid-row: 2; color: var(--color-text); line-height: calc(var(--leading-snug, 1.4) - 0.1); }
[data-cui-comp="ui-card"].fui-card--row .fui-card__body { grid-column: 2; grid-row: 1; padding: 0; }
[data-cui-comp="ui-card"].fui-card--row .fui-card__footer { grid-column: 1 / -1; }` +
		customModsCSS(cardMods, "ui-card", "fui-card", t)
}

// ─── OptimizedImage ─────────────────────────────────────────────────

func imageCSS(_ style.Theme) string {
	return `[data-cui-comp="ui-image"] {
  display: inline-block;
  position: relative;
  line-height: 0;
  /* contain ensures the aspect-ratio rules below don't bleed out. */
  contain: layout style;
}
[data-cui-comp="ui-image"] .fui-image__img {
  display: block;
  max-inline-size: 100%;
  block-size: auto;
  object-fit: cover;
  background: var(--color-surface-soft, #F4F4F5);
}
[data-cui-comp="ui-image"].fui-image--fit-contain .fui-image__img { object-fit: contain; }
[data-cui-comp="ui-image"].fui-image--fit-fill    .fui-image__img { object-fit: fill;    }
/* The root is included so its own background (which --placeheld sets, to
   back the letterbox bars) is rounded too — otherwise its square corners
   peek out around the rounded image. */
[data-cui-comp="ui-image"].fui-image--rounded,
[data-cui-comp="ui-image"].fui-image--rounded     .fui-image__img,
[data-cui-comp="ui-image"].fui-image--rounded     .fui-image__lqip,
[data-cui-comp="ui-image"].fui-image--rounded     picture {
  border-radius: var(--radii-md, 8px);
}

/* Low-quality placeholder: a decoded BlurHash or tiny LQIP, stacked behind
   the real image so something content-shaped is on screen before the real
   pixels land. It is an element rather than a background because a data URL
   is per-instance data and this project cannot emit per-instance CSS —
   inline style attributes are blocked by the CSP and by the
   noinlinestyles linter, and a data URL cannot be enumerated into a class.

   Both layers are positioned, so paint order follows tree order: the
   placeholder is emitted first and the real image covers it. No z-index and
   no JavaScript are involved, and the placeholder is simply left in place
   once the image has loaded. */
[data-cui-comp="ui-image"] .fui-image__lqip {
  position: absolute;
  inset: 0;
  inline-size: 100%;
  block-size: 100%;
  object-fit: cover;
}
[data-cui-comp="ui-image"] .fui-image__img,
[data-cui-comp="ui-image"] picture {
  position: relative;
}
/* The placeholder must letterbox exactly like the image in front of it.
   Without this, a fit-contain image (which does not fill its box) would show
   a cover-cropped blur through the empty bars — permanently, since the
   placeholder is never removed. */
[data-cui-comp="ui-image"].fui-image--fit-contain .fui-image__lqip { object-fit: contain; }
[data-cui-comp="ui-image"].fui-image--fit-fill    .fui-image__lqip { object-fit: fill;    }
/* With a placeholder present the grey resting fill moves to the root, so it
   still backs the letterbox bars while the blur — not the grey — is what
   shows through the image itself. */
[data-cui-comp="ui-image"].fui-image--placeheld {
  background: var(--color-surface-soft, #F4F4F5);
}
[data-cui-comp="ui-image"].fui-image--placeheld .fui-image__img {
  background: transparent;
}
[data-cui-comp="ui-image"].fui-image--aspect-1-1  .fui-image__img { aspect-ratio: 1 / 1;  inline-size: 100%; block-size: auto; }
[data-cui-comp="ui-image"].fui-image--aspect-4-3  .fui-image__img { aspect-ratio: 4 / 3;  inline-size: 100%; block-size: auto; }
[data-cui-comp="ui-image"].fui-image--aspect-16-9 .fui-image__img { aspect-ratio: 16 / 9; inline-size: 100%; block-size: auto; }
[data-cui-comp="ui-image"].fui-image--aspect-21-9 .fui-image__img { aspect-ratio: 21 / 9; inline-size: 100%; block-size: auto; }
[data-cui-comp="ui-image"].fui-image--aspect-3-4  .fui-image__img { aspect-ratio: 3 / 4;  inline-size: 100%; block-size: auto; }

/* Decorative class — allows alt="" without alt-text warnings. */
[data-cui-comp="ui-image"].fui-image--decorative .fui-image__img {
  /* visual same as default; the marker exists for the linter / a11y
     audit to know empty alt is intentional. */
}`
}

// ─── Toggle (Checkbox / Radio / Switch) ─────────────────────────────

func toggleCSS(_ style.Theme) string {
	// Knobs: --ui-choice-box-size (1.25rem) squares the checkbox/radio
	// box (the hint indent below is calc() over it);
	// --ui-switch-track-width (2.25rem) / --ui-switch-track-height
	// (1.25rem) size the switch track; --ui-switch-thumb-rim
	// (rgba(0, 0, 0, 0.18)) is the thumb's drawn rim in the gradient.
	return `/* Choice rows are drawn from the native input itself
   (appearance: none): headless.Choice's anatomy has no indicator
   span, and an input cannot host a pseudo-element — so the box, the
   dot and the switch's thumb are background layers keyed to :checked.
   The row keeps its own wrapping-label structure and deliberately
   ignores the field sheet's --fui-field-columns: a choice row is one
   inline run, not a label track above a control track. */
.fui-choice {
  display: inline-flex;
  flex-wrap: wrap;
  align-items: center;
  gap: var(--spacing-md, 8px);
  cursor: pointer;
  /* Token-scaled touch target; the label wrap is the accessible
     target (WCAG 2.5.8), the row keeps the comfortable height. The
     height counts the inset: a checkbox in a table row is the target's
     height, not 8px more. */
  box-sizing: border-box;
  min-block-size: var(--spacing-touch-target, 44px);
  padding-block: var(--spacing-sm, 4px);
}
.fui-choice__input {
  appearance: none;
  -webkit-appearance: none;
  flex-shrink: 0;
  box-sizing: border-box;
  inline-size: var(--ui-choice-box-size, 1.25rem);
  block-size: var(--ui-choice-box-size, 1.25rem);
  margin: 0;
  /* An unchecked box is drawn by its border alone, so the border is a
     control boundary (WCAG 1.4.11, 3:1), not a hairline: text-subtle
     clears it in both schemes, border-strong does not. */
  border: var(--stroke-thin, 1px) solid var(--color-text-subtle);
  background: var(--color-surface);
  box-shadow: var(--shadow-xs);
  cursor: inherit;
  transition: background-color var(--duration-fast, 150ms) var(--easing-ease-in-out, ease),
              border-color var(--duration-fast, 150ms) ease;
}
.fui-choice__text {
  flex: 1 1 auto;
  font-size: var(--text-sm);
  color: var(--color-text, #18181B);
  line-height: var(--leading-snug, 1.4);
  min-inline-size: 0;
}
.fui-choice__hint {
  display: block;
  flex-basis: 100%;
  margin-block-start: var(--spacing-xs, 2px);
  /* Logical inline-start, like the rest of this sheet: the indent
     follows the control in RTL, staying under the label, instead of
     detaching to the physical left of the row. */
  margin-inline-start: calc(var(--ui-choice-box-size, 1.25rem) + var(--spacing-sm, 4px));
  font-size: var(--text-sm, 0.875rem);
  color: var(--color-text-muted, #52525B);
}

/* ─── Checkbox: the check is two gradient strokes, so its ink is a
   token (--color-primary-fg) rather than a hex baked into an SVG.
   Each stroke is the 50% line of a corner-keyword gradient, which
   runs exactly corner to corner of its tile whatever the tile's
   aspect: "to top right" draws the short leg (\) and "to bottom
   right" the long leg (/), and the two tiles share the corner where
   the legs meet. Angle gradients (45deg, 135deg) only hit the corners
   of a square tile, which drew a broken, crossed mark. ─── */
.fui-choice--checkbox .fui-choice__input {
  border-radius: calc(var(--radii-sm) - 2px);
}
.fui-choice--checkbox .fui-choice__input:checked {
  background-color: var(--color-primary, #18181B);
  border-color: var(--color-primary, #18181B);
  background-image:
    linear-gradient(to top right, transparent calc(50% - 1px), var(--color-primary-fg, #FFFFFF) calc(50% - 1px), var(--color-primary-fg, #FFFFFF) calc(50% + 1px), transparent calc(50% + 1px)),
    linear-gradient(to bottom right, transparent calc(50% - 1px), var(--color-primary-fg, #FFFFFF) calc(50% - 1px), var(--color-primary-fg, #FFFFFF) calc(50% + 1px), transparent calc(50% + 1px));
  background-repeat: no-repeat;
  background-size: 5px 5px, 8px 9px;
  background-position: 3px 8px, 7px 4px;
}

/* A mixed box (a select-all over a partial selection) is a filled box
   with a bar. */
.fui-choice--checkbox .fui-choice__input:indeterminate {
  background-color: var(--color-primary, #18181B);
  border-color: var(--color-primary, #18181B);
  background-image: linear-gradient(var(--color-primary-fg, #FFFFFF), var(--color-primary-fg, #FFFFFF));
  background-repeat: no-repeat;
  background-size: 50% 2px;
  background-position: center;
}

/* ─── Radio: the dot is a hard-stop radial gradient. ─── */
.fui-choice--radio .fui-choice__input {
  border-radius: 50%;
}
.fui-choice--radio .fui-choice__input:checked {
  border-color: var(--color-primary, #18181B);
  background-image: radial-gradient(circle at center, var(--color-primary, #18181B) 0 5px, transparent 5.5px);
}

/* ─── Switch: the track IS the input; the thumb is a positioned
   gradient that slides with background-position. ─── */
.fui-switch {
  display: inline-flex;
  flex-wrap: wrap;
  align-items: center;
  gap: var(--spacing-md, 8px);
  cursor: pointer;
  min-block-size: var(--spacing-touch-target, 44px);
  padding-block: var(--spacing-sm, 4px);
}
.fui-switch__input {
  appearance: none;
  -webkit-appearance: none;
  flex-shrink: 0;
  box-sizing: border-box;
  inline-size: var(--ui-switch-track-width, 2.25rem);
  block-size: var(--ui-switch-track-height, 1.25rem);
  margin: 0;
  border: var(--stroke-thin, 1px) solid var(--color-border, #E4E4E7);
  border-radius: var(--radii-full, 9999px);
  background-color: var(--color-surface-soft, #F4F4F5);
  background-image: radial-gradient(circle, var(--color-primary-fg, #FFFFFF) 0 7.5px, var(--ui-switch-thumb-rim, rgba(0, 0, 0, 0.18)) 7.5px 8.5px, transparent 9px);
  background-repeat: no-repeat;
  background-size: 1.125rem 1.125rem;
  background-position: left 0.0625rem center;
  cursor: inherit;
  transition: background-color var(--duration-fast, 150ms) var(--easing-ease-in-out, ease),
              background-position var(--duration-fast, 150ms) ease;
}
.fui-switch__input:checked {
  background-color: var(--color-primary, #18181B);
  border-color: var(--color-primary, #18181B);
  background-position: right 0.0625rem center;
}
.fui-switch__text {
  flex: 1 1 auto;
  font-size: var(--text-sm);
  color: var(--color-text, #18181B);
  line-height: var(--leading-snug, 1.4);
}

/* ─── Shared state styling, from the state attributes themselves. ─── */
.fui-choice__input:focus-visible,
.fui-switch__input:focus-visible {
  outline: var(--stroke-focus, 2px) solid var(--color-text-subtle);
  outline-offset: var(--stroke-focus-offset, 2px);
}
.fui-choice__input[aria-invalid="true"],
.fui-switch__input[aria-invalid="true"] {
  border-color: var(--color-danger, #DC2626);
}
.fui-choice__input:disabled,
.fui-switch__input:disabled {
  opacity: var(--opacity-muted, 0.6);
  cursor: not-allowed;
}
.fui-choice:has(.fui-choice__input:disabled),
.fui-switch:has(.fui-switch__input:disabled) {
  opacity: var(--opacity-muted, 0.6);
  cursor: not-allowed;
}

/* ─── The errored-run shell: a standalone choice with a message. ─── */
.fui-choice-field {
  display: grid;
  gap: var(--spacing-xs, 2px);
  justify-items: start;
}
.fui-choice-field__error,
.fui-choice-field__hint,
.fui-choice + [data-hui-field-error] {
  margin: 0;
  font-size: var(--text-sm, 0.875rem);
}
/* The hook alias dresses the paragraph the runtime's form-errors module
   places after a bare choice (see the field sheet's twin). */
.fui-choice-field__error,
.fui-choice + [data-hui-field-error] { color: var(--color-danger, #DC2626); }
.fui-choice-field__hint { color: var(--color-text-muted, #52525B); }

/* ─── Choice groups: a real fieldset, laid out as a stack of rows. ─── */
.fui-choice-group {
  border: none;
  padding: 0;
  margin: 0;
  display: grid;
  gap: var(--spacing-sm, 4px);
}
.fui-choice-group__legend {
  font-weight: var(--font-weight-medium);
  font-size: var(--text-sm, 0.875rem);
  color: var(--color-text, #18181B);
  padding: 0;
  margin-bottom: var(--spacing-xs, 2px);
}
/* The same required mark a field's label carries, drawn from the same
   state attribute: an asterisk with empty alternative text, so the
   legend's accessible name stays clean. The rule itself is on the
   leaves' required attributes; this is the cue beside it. */
.fui-choice-group__legend[data-required]::after {
  content: " *" / "";
  color: var(--color-danger, #DC2626);
}
.fui-choice-group__hint,
.fui-choice-group__error {
  margin: 0;
  font-size: var(--text-sm, 0.875rem);
}
.fui-choice-group__hint  { color: var(--color-text-muted, #52525B); }
.fui-choice-group__error { color: var(--color-danger, #DC2626); }`
}

// ─── Tooltip ────────────────────────────────────────────────────────

func tooltipCSS(_ style.Theme) string {
	// Knob: --ui-tooltip-pop-max-width (240px) keeps a one-line pop
	// from crossing a narrow viewport.
	return `[data-cui-comp="ui-tooltip"] {
  position: relative;
  display: inline-block;
  /* Allow keyboard focus on the wrapper for :focus-within to reveal
     the pop without requiring focus on a particular descendant
     element. */
}
[data-cui-comp="ui-tooltip"] .fui-tooltip__pop {
  position: absolute;
  inset-block-end: calc(100% + calc(var(--spacing-sm, 4px) * 1.5));
  inset-inline-start: 50%;
  transform: translateX(-50%) translateY(4px);
  background: var(--color-text);
  color: var(--color-surface);
  padding: calc(var(--spacing-sm, 4px) * 1.5) calc(var(--spacing-sm, 4px) * 3);
  font-size: var(--text-xs, 0.75rem);
  line-height: var(--leading-tight, 1.2);
  border-radius: var(--radii-md);
  pointer-events: none;
  opacity: 0;
  visibility: hidden;
  white-space: nowrap;
  max-inline-size: var(--ui-tooltip-pop-max-width, 240px);
  z-index: var(--z-popover);
  transition: opacity var(--duration-fast, 150ms) var(--easing-ease-in-out, ease),
              transform var(--duration-fast, 150ms) ease,
              visibility 0s var(--duration-fast, 150ms);
}
[data-cui-comp="ui-tooltip"]:hover .fui-tooltip__pop,
[data-cui-comp="ui-tooltip"]:focus-within .fui-tooltip__pop {
  opacity: 1;
  visibility: visible;
  transform: translateX(-50%) translateY(0);
  transition-delay: 0s;
}

[data-cui-comp="ui-tooltip"].fui-tooltip--bottom .fui-tooltip__pop {
  inset-block-end: auto;
  inset-block-start: calc(100% + calc(var(--spacing-sm, 4px) * 1.5));
  transform: translateX(-50%) translateY(-4px);
}
[data-cui-comp="ui-tooltip"].fui-tooltip--bottom:hover .fui-tooltip__pop,
[data-cui-comp="ui-tooltip"].fui-tooltip--bottom:focus-within .fui-tooltip__pop {
  transform: translateX(-50%) translateY(0);
}
[data-cui-comp="ui-tooltip"].fui-tooltip--left .fui-tooltip__pop {
  inset-block-end: 50%;
  inset-inline-start: auto;
  inset-inline-end: calc(100% + calc(var(--spacing-sm, 4px) * 1.5));
  transform: translateY(50%) translateX(4px);
}
[data-cui-comp="ui-tooltip"].fui-tooltip--left:hover .fui-tooltip__pop,
[data-cui-comp="ui-tooltip"].fui-tooltip--left:focus-within .fui-tooltip__pop {
  transform: translateY(50%) translateX(0);
}
[data-cui-comp="ui-tooltip"].fui-tooltip--right .fui-tooltip__pop {
  inset-block-end: 50%;
  inset-inline-start: calc(100% + calc(var(--spacing-sm, 4px) * 1.5));
  transform: translateY(50%) translateX(-4px);
}
[data-cui-comp="ui-tooltip"].fui-tooltip--right:hover .fui-tooltip__pop,
[data-cui-comp="ui-tooltip"].fui-tooltip--right:focus-within .fui-tooltip__pop {
  transform: translateY(50%) translateX(0);
}

@media (prefers-reduced-motion: reduce) {
  [data-cui-comp="ui-tooltip"] .fui-tooltip__pop { transition: none; transform: translateX(-50%) translateY(0); }
}`
}

// ─── Tag / Chip ─────────────────────────────────────────────────────

func tagCSS(t style.Theme) string {
	// Knobs: --ui-tag-line-height (1rem) pins the chip's line box;
	// --ui-tag-dismiss-size (1.1rem) squares the × hit area and
	// --ui-tag-dismiss-opacity (0.7) rests it until hover; --ui-tag-case
	// (none) sets its letter case.
	return `[data-cui-comp="ui-tag"] {
  display: inline-flex;
  /* A chip keeps its own width in a flex column too (a Card header,
     a Stack), where a flex item with an auto width stretches. */
  inline-size: fit-content;
  align-items: center;
  gap: var(--spacing-xs, 2px);
  padding: var(--spacing-xs, 2px) var(--spacing-md, 8px);
  border: var(--stroke-thin, 1px) solid transparent;
  border-radius: var(--radii-md);
  font-size: var(--text-xs, 0.75rem);
  font-weight: var(--font-weight-medium);
  line-height: var(--ui-tag-line-height, 1rem);
  text-decoration: none;
  text-transform: var(--ui-tag-case, none);
  /* In a box narrower than the chip (a phone row's cell) the label
     ends with an ellipsis; the icon and the dismiss keep their size. */
  min-inline-size: 0;
}
[data-cui-comp="ui-tag"] .fui-tag__label {
  min-inline-size: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
[data-cui-comp="ui-tag"] .fui-tag__icon,
[data-cui-comp="ui-tag"] .fui-tag__dismiss {
  flex: none;
}
/* Same drawing as ui-badge: the tint is 15% of the status hue, the ink
   mixes the hue toward --color-text so it holds AA on the tint in
   either scheme. */
[data-cui-comp="ui-tag"].fui-tag--neutral {
  background: var(--color-surface-soft);
  color: var(--color-text);
  border-color: var(--color-border);
}
[data-cui-comp="ui-tag"].fui-tag--success {
  background: color-mix(in oklab, var(--color-success) 15%, var(--color-surface) 85%);
  color: color-mix(in oklab, var(--color-success) 55%, var(--color-text) 45%);
  border-color: color-mix(in oklab, var(--color-success) 30%, var(--color-surface) 70%);
}
[data-cui-comp="ui-tag"].fui-tag--warning {
  background: color-mix(in oklab, var(--color-warning) 15%, var(--color-surface) 85%);
  color: color-mix(in oklab, var(--color-warning) 55%, var(--color-text) 45%);
  border-color: color-mix(in oklab, var(--color-warning) 30%, var(--color-surface) 70%);
}
[data-cui-comp="ui-tag"].fui-tag--danger {
  background: color-mix(in oklab, var(--color-danger) 15%, var(--color-surface) 85%);
  color: color-mix(in oklab, var(--color-danger) 55%, var(--color-text) 45%);
  border-color: color-mix(in oklab, var(--color-danger) 30%, var(--color-surface) 70%);
}
[data-cui-comp="ui-tag"].fui-tag--info {
  background: color-mix(in oklab, var(--color-info) 15%, var(--color-surface) 85%);
  color: color-mix(in oklab, var(--color-info) 55%, var(--color-text) 45%);
  border-color: color-mix(in oklab, var(--color-info) 30%, var(--color-surface) 70%);
}
[data-cui-comp="ui-tag"].fui-tag--interactive {
  cursor: pointer;
  transition: border-color var(--duration-fast, 150ms) var(--easing-ease-in-out, ease), box-shadow var(--duration-fast, 150ms) var(--easing-ease-in-out, ease), translate var(--duration-fast, 150ms) var(--easing-ease-in-out, ease);
}
/* Knobs: --ui-tag-hover-shadow / -active-shadow and
   --ui-tag-hover-translate / -active-translate over the shared
   --ui-press-* knobs. */
[data-cui-comp="ui-tag"].fui-tag--interactive:hover {
  border-color: var(--color-border-strong);
  box-shadow: var(--ui-tag-hover-shadow, var(--ui-press-hover-shadow, none));
  translate: var(--ui-tag-hover-translate, var(--ui-press-hover-translate, none));
}
[data-cui-comp="ui-tag"].fui-tag--interactive:active {
  box-shadow: var(--ui-tag-active-shadow, var(--ui-press-active-shadow, var(--ui-tag-hover-shadow, var(--ui-press-hover-shadow, none))));
  translate: var(--ui-tag-active-translate, var(--ui-press-active-translate, var(--ui-tag-hover-translate, var(--ui-press-hover-translate, none))));
}
[data-cui-comp="ui-tag"]:focus-visible {
  outline: var(--stroke-focus, 2px) solid var(--color-text-subtle);
  outline-offset: var(--stroke-focus-offset, 2px);
}
[data-cui-comp="ui-tag"] .fui-tag__dismiss {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  inline-size: var(--ui-tag-dismiss-size, 1.1rem);
  block-size:  var(--ui-tag-dismiss-size, 1.1rem);
  margin-inline-end: calc(var(--spacing-sm, 4px) * -1);
  border: 0;
  background: transparent;
  color: inherit;
  opacity: var(--ui-tag-dismiss-opacity, 0.7);
  cursor: pointer;
  border-radius: 50%;
  font-size: var(--text-base, 1rem);
  line-height: 1;
  padding: 0;
  /* A dismiss with a DismissPath renders as an <a>; the × is an icon,
     not link text, so it carries no underline. */
  text-decoration: none;
}
[data-cui-comp="ui-tag"] .fui-tag__dismiss:hover { opacity: 1; background: color-mix(in srgb, currentColor 12%, transparent); }
[data-cui-comp="ui-tag"] .fui-tag__dismiss:focus-visible {
  outline: var(--stroke-focus, 2px) solid var(--color-text-subtle);
  outline-offset: var(--stroke-focus-offset, 2px);
}` + customStatusCSS("ui-tag", "fui-tag", t)
}

// ─── Spinner ────────────────────────────────────────────────────────

func spinnerCSS(_ style.Theme) string {
	return `[data-cui-comp="ui-spinner"] {
  display: flex;
  align-items: center;
  justify-content: center;
  gap: var(--spacing-sm, 4px);
  --_spinner-size: var(--ui-spinner-size, 1.5rem);
}
[data-cui-comp="ui-spinner"].fui-spinner--sm { --ui-spinner-size: 1rem; }
[data-cui-comp="ui-spinner"].fui-spinner--lg { --ui-spinner-size: 2.5rem; }
:where([data-cui-comp="ui-spinner"]).fui-spinner--inline { display: inline-flex; }
[data-cui-comp="ui-spinner"] .fui-spinner__ring {
  display: inline-block;
  inline-size: var(--_spinner-size);
  block-size:  var(--_spinner-size);
  border-radius: 50%;
  border: var(--stroke-thick, 2px) solid var(--color-border, #E4E4E7);
  border-top-color: var(--color-primary, #18181B);
  animation: fui-spinner-rotate var(--duration-slow, 400ms) linear infinite;
}
[data-cui-comp="ui-spinner"] .fui-spinner__dots {
  display: inline-flex;
  align-items: center;
  gap: var(--spacing-sm, 4px);
}
[data-cui-comp="ui-spinner"] .fui-spinner__dot {
  display: inline-block;
  inline-size: calc(var(--_spinner-size) * 0.28);
  block-size:  calc(var(--_spinner-size) * 0.28);
  border-radius: 50%;
  background: var(--color-primary, #18181B);
  animation: fui-spinner-pulse 1.2s var(--easing-ease-in-out, ease-in-out) infinite both;
}
[data-cui-comp="ui-spinner"] .fui-spinner__dot:nth-child(1) { animation-delay: -0.32s; }
[data-cui-comp="ui-spinner"] .fui-spinner__dot:nth-child(2) { animation-delay: -0.16s; }

/* SpinnerGrid — 3×3 cells with a diagonal-ripple delay schedule. */
[data-cui-comp="ui-spinner"] .fui-spinner__grid {
  display: grid;
  grid-template-columns: repeat(3, 1fr);
  gap: calc(var(--_spinner-size) * 0.08);
  inline-size: var(--_spinner-size);
  block-size:  var(--_spinner-size);
}
[data-cui-comp="ui-spinner"] .fui-spinner__cell {
  display: block;
  background: var(--color-primary, #18181B);
  border-radius: calc(var(--radii-sm, 6px) / 3);
  animation: fui-spinner-grid 1.3s var(--easing-ease-in-out, ease-in-out) infinite both;
}
[data-cui-comp="ui-spinner"] .fui-spinner__cell:nth-child(1) { animation-delay: 0.0s; }
[data-cui-comp="ui-spinner"] .fui-spinner__cell:nth-child(2) { animation-delay: 0.1s; }
[data-cui-comp="ui-spinner"] .fui-spinner__cell:nth-child(3) { animation-delay: 0.2s; }
[data-cui-comp="ui-spinner"] .fui-spinner__cell:nth-child(4) { animation-delay: 0.1s; }
[data-cui-comp="ui-spinner"] .fui-spinner__cell:nth-child(5) { animation-delay: 0.2s; }
[data-cui-comp="ui-spinner"] .fui-spinner__cell:nth-child(6) { animation-delay: 0.3s; }
[data-cui-comp="ui-spinner"] .fui-spinner__cell:nth-child(7) { animation-delay: 0.2s; }
[data-cui-comp="ui-spinner"] .fui-spinner__cell:nth-child(8) { animation-delay: 0.3s; }
[data-cui-comp="ui-spinner"] .fui-spinner__cell:nth-child(9) { animation-delay: 0.4s; }

@keyframes fui-spinner-rotate {
  to { transform: rotate(360deg); }
}
@keyframes fui-spinner-pulse {
  0%, 80%, 100% { opacity: var(--opacity-faint, 0.2); transform: scale(0.8); }
  40%           { opacity: 1;   transform: scale(1); }
}
@keyframes fui-spinner-grid {
  0%, 70%, 100% { opacity: var(--opacity-faint, 0.2); transform: scale(0.7); }
  35%           { opacity: 1;   transform: scale(1); }
}

@media (prefers-reduced-motion: reduce) {
  [data-cui-comp="ui-spinner"] .fui-spinner__ring,
  [data-cui-comp="ui-spinner"] .fui-spinner__dot,
  [data-cui-comp="ui-spinner"] .fui-spinner__cell {
    animation-duration: 2.4s;
  }
}

.fui-visually-hidden,
.fui-visually-hidden {
  position: absolute;
  inline-size: 1px;
  block-size: 1px;
  padding: 0;
  margin: -1px;
  overflow: hidden;
  clip: rect(0, 0, 0, 0);
  white-space: nowrap;
  border: 0;
}`
}

// ─── Divider ────────────────────────────────────────────────────────

func dividerCSS(_ style.Theme) string {
	return `[data-cui-comp="ui-divider"] {
  border: 0;
  background: var(--color-border, #E4E4E7);
}
:where(hr)[data-cui-comp="ui-divider"] {
  block-size: 1px;
  inline-size: 100%;
  margin: var(--spacing-md, 8px) 0;
}
:where([data-cui-comp="ui-divider"]).fui-divider--vertical {
  display: inline-block;
  inline-size: 1px;
  block-size: 1em;
  margin: 0 var(--spacing-sm, 4px);
  vertical-align: middle;
}
:where([data-cui-comp="ui-divider"]).fui-divider--labelled {
  display: flex;
  align-items: center;
  gap: var(--spacing-md, 8px);
  margin: var(--spacing-md, 8px) 0;
  background: transparent;
  color: var(--color-text-muted, #52525B);
  font-size: var(--text-sm, 0.875rem);
  font-weight: var(--font-weight-medium);
}
[data-cui-comp="ui-divider"] .fui-divider__line {
  flex: 1 1 0;
  block-size: 1px;
  background: var(--color-border, #E4E4E7);
}`
}

func stickyCSS(_ style.Theme) string {
	// The z-index layer comes from StickyConfig.ZIndexTier via the
	// data-fui-z-tier attribute, mapped to the theme's --z-<tier>
	// tokens (style.ZIndexSet: dropdown 100 / sticky 200 / modal 300 /
	// popover 400 / toast 500 by default).
	return `[data-cui-comp="ui-sticky"] {
  position: -webkit-sticky;
  position: sticky;
  z-index: var(--z-sticky, 200);
}
:where([data-cui-comp="ui-sticky"])[data-fui-z-tier="dropdown"] { z-index: var(--z-dropdown, 100); }
:where([data-cui-comp="ui-sticky"])[data-fui-z-tier="modal"]    { z-index: var(--z-modal, 300); }
:where([data-cui-comp="ui-sticky"])[data-fui-z-tier="popover"]  { z-index: var(--z-popover, 400); }
:where([data-cui-comp="ui-sticky"])[data-fui-z-tier="toast"]    { z-index: var(--z-toast, 500); }
[data-cui-comp="ui-sticky"]::after {
  content: "";
  position: absolute;
  left: 0;
  right: 0;
  bottom: -1px;
  height: 1px;
  background: var(--color-border, #E4E4E7);
  opacity: 0;
  transition: opacity var(--duration-fast, 150ms);
}
/* Edge offsets */
:where([data-cui-comp="ui-sticky"]).fui-sticky--top { top: 0; }
:where([data-cui-comp="ui-sticky"]).fui-sticky--bottom { bottom: 0; }
:where([data-cui-comp="ui-sticky"]).fui-sticky--offset-sm { top: var(--spacing-sm, 4px); }
:where([data-cui-comp="ui-sticky"]).fui-sticky--offset-md { top: var(--spacing-md, 8px); }
:where([data-cui-comp="ui-sticky"]).fui-sticky--offset-lg { top: var(--spacing-lg, 16px); }
:where([data-cui-comp="ui-sticky"]).fui-sticky--offset-xl { top: var(--spacing-xl, 24px); }
/* Show a subtle bottom border when the element is stuck (only top-sticky) */
@supports ((position: -webkit-sticky) or (position: sticky)) {
  [data-cui-comp="ui-sticky"].fui-sticky--top:not(:is(:first-child))::after {
    opacity: 1;
  }
}`
}

func aspectRatioCSS(_ style.Theme) string {
	return `[data-cui-comp="ui-aspect-ratio"] {
  position: relative;
  width: 100%;
}
[data-cui-comp="ui-aspect-ratio"] > * {
  position: absolute;
  inset: 0;
  width: 100%;
  height: 100%;
}
[data-cui-comp="ui-aspect-ratio"].fui-ar--1-1  { aspect-ratio: 1 / 1; }
[data-cui-comp="ui-aspect-ratio"].fui-ar--4-3  { aspect-ratio: 4 / 3; }
[data-cui-comp="ui-aspect-ratio"].fui-ar--16-9 { aspect-ratio: 16 / 9; }
[data-cui-comp="ui-aspect-ratio"].fui-ar--21-9 { aspect-ratio: 21 / 9; }
[data-cui-comp="ui-aspect-ratio"].fui-ar--3-4  { aspect-ratio: 3 / 4; }
[data-cui-comp="ui-aspect-ratio"].fui-ar--3-2  { aspect-ratio: 3 / 2; }
[data-cui-comp="ui-aspect-ratio"].fui-ar--2-3  { aspect-ratio: 2 / 3; }
/* auto: no aspect-ratio, child sizes naturally */
[data-cui-comp="ui-aspect-ratio"].fui-ar--auto > * {
  position: static;
  width: auto;
  height: auto;
}`
}
