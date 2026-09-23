package ui

import (
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

func layoutCSS(_ style.Theme) string {
	// Layout primitives stack data-fui-comp="ui-layout" AND their own
	// class on the SAME element (Stack, Cluster, Grid, … are sibling
	// classes, not descendants). Selectors below combine the marker
	// and class on the same element via `[data-fui-comp="ui-layout"].<class>`
	// rather than the descendant `[data-fui-comp="ui-layout"] .<class>`.
	return `[data-fui-comp="ui-layout"] {
  box-sizing: border-box;
}
[data-fui-comp="ui-layout"].fui-stack {
  display: flex;
  flex-direction: column;
  gap: var(--spacing-md, 8px);
}
[data-fui-comp="ui-layout"].fui-cluster {
  display: flex;
  flex-direction: row;
  flex-wrap: wrap;
  gap: var(--spacing-md, 8px);
  align-items: center;
}
[data-fui-comp="ui-layout"].fui-cluster--nowrap { flex-wrap: nowrap; }

[data-fui-comp="ui-layout"].fui-grid {
  display: grid;
  gap: var(--spacing-md, 8px);
  grid-template-columns: repeat(auto-fit, minmax(var(--ui-grid-min, 16rem), 1fr));
}

[data-fui-comp="ui-layout"].fui-center {
  display: flex;
  align-items: center;
  justify-content: center;
}
[data-fui-comp="ui-layout"].fui-center--viewport { min-block-size: 100vh; }
[data-fui-comp="ui-layout"].fui-center--screen   { min-block-size: 100dvh; }

[data-fui-comp="ui-layout"].fui-spacer {
  flex: 1 1 auto;
  align-self: stretch;
}

[data-fui-comp="ui-layout"].fui-box { background: transparent; }
[data-fui-comp="ui-layout"].fui-box--surface {
  background: var(--color-surface, #FFFFFF);
  border-radius: var(--radii-md, 8px);
}
[data-fui-comp="ui-layout"].fui-box--outlined {
  border: 1px solid var(--color-border, #E4E4E7);
  border-radius: var(--radii-md, 8px);
}
[data-fui-comp="ui-layout"].fui-box--pad-sm { padding: var(--spacing-sm, 4px); }
[data-fui-comp="ui-layout"].fui-box--pad-md { padding: var(--spacing-md, 8px); }
[data-fui-comp="ui-layout"].fui-box--pad-lg { padding: var(--spacing-lg, 16px); }
[data-fui-comp="ui-layout"].fui-box--pad-xl { padding: var(--spacing-xl, 24px); }

/* gap modifiers — apply to ui-stack/ui-cluster/ui-grid. */
[data-fui-comp="ui-layout"].fui-layout--gap-none { gap: 0; }
[data-fui-comp="ui-layout"].fui-layout--gap-xs   { gap: var(--spacing-xs, 2px); }
[data-fui-comp="ui-layout"].fui-layout--gap-sm   { gap: var(--spacing-sm, 4px); }
[data-fui-comp="ui-layout"].fui-layout--gap-lg   { gap: var(--spacing-lg, 16px); }
[data-fui-comp="ui-layout"].fui-layout--gap-xl   { gap: var(--spacing-xl, 24px); }
[data-fui-comp="ui-layout"].fui-layout--gap-2xl  { gap: var(--spacing-2xl, 32px); }

/* alignment modifiers. */
[data-fui-comp="ui-layout"].fui-layout--align-start    { align-items: flex-start; }
[data-fui-comp="ui-layout"].fui-layout--align-center   { align-items: center; }
[data-fui-comp="ui-layout"].fui-layout--align-end      { align-items: flex-end; }
[data-fui-comp="ui-layout"].fui-layout--align-baseline { align-items: baseline; }
[data-fui-comp="ui-layout"].fui-layout--align-stretch  { align-items: stretch; }

[data-fui-comp="ui-layout"].fui-layout--justify-start   { justify-content: flex-start; }
[data-fui-comp="ui-layout"].fui-layout--justify-center  { justify-content: center; }
[data-fui-comp="ui-layout"].fui-layout--justify-end     { justify-content: flex-end; }
[data-fui-comp="ui-layout"].fui-layout--justify-between { justify-content: space-between; }
[data-fui-comp="ui-layout"].fui-layout--justify-around  { justify-content: space-around; }`
}

// ─── Card ───────────────────────────────────────────────────────────

func cardCSS(t style.Theme) string {
	return `[data-fui-comp="ui-card"] {
  display: flex;
  flex-direction: column;
  background: var(--color-surface, #FFFFFF);
  color: var(--color-text, #18181B);
  border-radius: var(--radii-lg, 12px);
  box-shadow: var(--shadows-sm, 0 1px 2px rgba(0,0,0,0.05));
  overflow: hidden;
  text-decoration: none;
}
[data-fui-comp="ui-card"].fui-card--outlined {
  box-shadow: none;
  border: 1px solid var(--color-border, #E4E4E7);
}
[data-fui-comp="ui-card"].fui-card--flat {
  box-shadow: none;
  background: transparent;
}
[data-fui-comp="ui-card"].fui-card--interactive {
  transition: transform var(--duration-fast, 150ms) ease,
              box-shadow var(--duration-fast, 150ms) ease;
  cursor: pointer;
}
[data-fui-comp="ui-card"].fui-card--interactive:hover {
  transform: translateY(-2px);
  box-shadow: var(--shadows-md, 0 4px 6px -1px rgba(0,0,0,0.10));
}
[data-fui-comp="ui-card"].fui-card--interactive:focus-visible {
  outline: 2px solid var(--color-primary, #4F46E5);
  outline-offset: 2px;
}
[data-fui-comp="ui-card"] .fui-card__inner {
  display: flex;
  flex-direction: column;
  flex: 1 1 auto;
}
[data-fui-comp="ui-card"] .fui-card__header {
  padding: var(--spacing-lg, 16px) var(--spacing-lg, 16px) var(--spacing-md, 8px);
  display: flex;
  flex-direction: column;
  gap: var(--spacing-xs, 2px);
}
[data-fui-comp="ui-card"] .fui-card__heading {
  margin: 0;
  font-size: var(--text-base, 1rem);
  font-weight: 600;
  color: var(--color-text, #18181B);
}
[data-fui-comp="ui-card"] .fui-card__description {
  margin: 0;
  font-size: var(--text-sm, 0.875rem);
  color: var(--color-text-muted, #52525B);
}
[data-fui-comp="ui-card"] .fui-card__body {
  padding: var(--spacing-md, 8px) var(--spacing-lg, 16px);
  color: var(--color-text, #18181B);
  flex: 1 1 auto;
}
/* The primitive always draws the body node — it is where an island
   swap lands — so the sheet collapses it when empty: a card with no
   body content gains no space. */
[data-fui-comp="ui-card"] .fui-card__body:empty { display: none; }
[data-fui-comp="ui-card"] .fui-card__header + .fui-card__body {
  padding-top: 0;
}
[data-fui-comp="ui-card"] .fui-card__footer {
  margin-top: auto;
  padding: var(--spacing-md, 8px) var(--spacing-lg, 16px);
  border-top: 1px solid var(--color-border, #E4E4E7);
  display: flex;
  align-items: center;
  gap: var(--spacing-sm, 4px);
  background: var(--color-surface-soft, #F4F4F5);
}
[data-fui-comp="ui-card"].fui-card--flat .fui-card__footer { background: transparent; }` +
		customModsCSS(cardMods, "ui-card", "fui-card", t)
}

// ─── OptimizedImage ─────────────────────────────────────────────────

func imageCSS(_ style.Theme) string {
	return `[data-fui-comp="ui-image"] {
  display: inline-block;
  position: relative;
  line-height: 0;
  /* contain ensures the aspect-ratio rules below don't bleed out. */
  contain: layout style;
}
[data-fui-comp="ui-image"] .fui-image__img {
  display: block;
  max-inline-size: 100%;
  block-size: auto;
  object-fit: cover;
  background: var(--color-surface-soft, #F4F4F5);
}
[data-fui-comp="ui-image"].fui-image--fit-contain .fui-image__img { object-fit: contain; }
[data-fui-comp="ui-image"].fui-image--fit-fill    .fui-image__img { object-fit: fill;    }
/* The root is included so its own background (which --placeheld sets, to
   back the letterbox bars) is rounded too — otherwise its square corners
   peek out around the rounded image. */
[data-fui-comp="ui-image"].fui-image--rounded,
[data-fui-comp="ui-image"].fui-image--rounded     .fui-image__img,
[data-fui-comp="ui-image"].fui-image--rounded     .fui-image__lqip,
[data-fui-comp="ui-image"].fui-image--rounded     picture {
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
[data-fui-comp="ui-image"] .fui-image__lqip {
  position: absolute;
  inset: 0;
  inline-size: 100%;
  block-size: 100%;
  object-fit: cover;
}
[data-fui-comp="ui-image"] .fui-image__img,
[data-fui-comp="ui-image"] picture {
  position: relative;
}
/* The placeholder must letterbox exactly like the image in front of it.
   Without this, a fit-contain image (which does not fill its box) would show
   a cover-cropped blur through the empty bars — permanently, since the
   placeholder is never removed. */
[data-fui-comp="ui-image"].fui-image--fit-contain .fui-image__lqip { object-fit: contain; }
[data-fui-comp="ui-image"].fui-image--fit-fill    .fui-image__lqip { object-fit: fill;    }
/* With a placeholder present the grey resting fill moves to the root, so it
   still backs the letterbox bars while the blur — not the grey — is what
   shows through the image itself. */
[data-fui-comp="ui-image"].fui-image--placeheld {
  background: var(--color-surface-soft, #F4F4F5);
}
[data-fui-comp="ui-image"].fui-image--placeheld .fui-image__img {
  background: transparent;
}
[data-fui-comp="ui-image"].fui-image--aspect-1-1  .fui-image__img { aspect-ratio: 1 / 1;  inline-size: 100%; block-size: auto; }
[data-fui-comp="ui-image"].fui-image--aspect-4-3  .fui-image__img { aspect-ratio: 4 / 3;  inline-size: 100%; block-size: auto; }
[data-fui-comp="ui-image"].fui-image--aspect-16-9 .fui-image__img { aspect-ratio: 16 / 9; inline-size: 100%; block-size: auto; }
[data-fui-comp="ui-image"].fui-image--aspect-21-9 .fui-image__img { aspect-ratio: 21 / 9; inline-size: 100%; block-size: auto; }
[data-fui-comp="ui-image"].fui-image--aspect-3-4  .fui-image__img { aspect-ratio: 3 / 4;  inline-size: 100%; block-size: auto; }

/* Decorative class — allows alt="" without alt-text warnings. */
[data-fui-comp="ui-image"].fui-image--decorative .fui-image__img {
  /* visual same as default; the marker exists for the linter / a11y
     audit to know empty alt is intentional. */
}`
}

// ─── Toggle (Checkbox / Radio / Switch) ─────────────────────────────

func toggleCSS(_ style.Theme) string {
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
  gap: var(--spacing-sm, 4px);
  cursor: pointer;
  /* Token-scaled touch target; the label wrap is the accessible
     target (WCAG 2.5.8), the row keeps the comfortable height. */
  min-block-size: var(--spacing-touch-target, 44px);
  padding-block: var(--spacing-sm, 4px);
}
.fui-choice__input {
  appearance: none;
  -webkit-appearance: none;
  flex-shrink: 0;
  box-sizing: border-box;
  inline-size: 1.25rem;
  block-size: 1.25rem;
  margin: 0;
  border: 1.5px solid var(--color-border-strong, #A1A1AA);
  background: var(--color-surface, #FFFFFF);
  cursor: inherit;
  transition: background-color var(--duration-fast, 150ms) ease,
              border-color var(--duration-fast, 150ms) ease;
}
.fui-choice__text {
  flex: 1 1 auto;
  font-size: var(--text-base, 1rem);
  color: var(--color-text, #18181B);
  line-height: 1.4;
  min-inline-size: 0;
}
.fui-choice__hint {
  display: block;
  flex-basis: 100%;
  margin-block-start: var(--spacing-xs, 2px);
  /* Logical inline-start, like the rest of this sheet: the indent
     follows the control in RTL, staying under the label, instead of
     detaching to the physical left of the row. */
  margin-inline-start: calc(1.25rem + var(--spacing-sm, 4px));
  font-size: var(--text-sm, 0.875rem);
  color: var(--color-text-muted, #52525B);
}

/* ─── Checkbox: the check is two gradient strokes, so its ink is a
   token (--color-primary-fg) rather than a hex baked into an SVG. ─── */
.fui-choice--checkbox .fui-choice__input {
  border-radius: var(--radii-sm, 4px);
}
.fui-choice--checkbox .fui-choice__input:checked {
  background-color: var(--color-primary, #4F46E5);
  border-color: var(--color-primary, #4F46E5);
  background-image:
    linear-gradient(45deg, transparent 52%, var(--color-primary-fg, #FFFFFF) 52%, var(--color-primary-fg, #FFFFFF) 68%, transparent 68%),
    linear-gradient(135deg, transparent 34%, var(--color-primary-fg, #FFFFFF) 34%, var(--color-primary-fg, #FFFFFF) 50%, transparent 50%);
  background-repeat: no-repeat;
  background-size: 9px 9px, 12px 9px;
  background-position: 3px 7px, 5px 3px;
}

/* ─── Radio: the dot is a hard-stop radial gradient. ─── */
.fui-choice--radio .fui-choice__input {
  border-radius: 50%;
}
.fui-choice--radio .fui-choice__input:checked {
  border-color: var(--color-primary, #4F46E5);
  background-image: radial-gradient(circle at center, var(--color-primary, #4F46E5) 0 5px, transparent 5.5px);
}

/* ─── Switch: the track IS the input; the thumb is a positioned
   gradient that slides with background-position. ─── */
.fui-switch {
  display: inline-flex;
  flex-wrap: wrap;
  align-items: center;
  gap: var(--spacing-sm, 4px);
  cursor: pointer;
  min-block-size: var(--spacing-touch-target, 44px);
  padding-block: var(--spacing-sm, 4px);
}
.fui-switch__input {
  appearance: none;
  -webkit-appearance: none;
  flex-shrink: 0;
  box-sizing: border-box;
  inline-size: 2.25rem;
  block-size: 1.25rem;
  margin: 0;
  border: 1px solid var(--color-border, #E4E4E7);
  border-radius: var(--radii-full, 9999px);
  background-color: var(--color-surface-soft, #F4F4F5);
  background-image: radial-gradient(circle, var(--color-primary-fg, #FFFFFF) 0 7.5px, rgba(0, 0, 0, 0.18) 7.5px 8.5px, transparent 9px);
  background-repeat: no-repeat;
  background-size: 1.125rem 1.125rem;
  background-position: left 0.0625rem center;
  cursor: inherit;
  transition: background-color var(--duration-fast, 150ms) ease,
              background-position var(--duration-fast, 150ms) ease;
}
.fui-switch__input:checked {
  background-color: var(--color-primary, #4F46E5);
  border-color: var(--color-primary, #4F46E5);
  background-position: right 0.0625rem center;
}
.fui-switch__text {
  flex: 1 1 auto;
  font-size: var(--text-base, 1rem);
  color: var(--color-text, #18181B);
  line-height: 1.4;
}

/* ─── Shared state styling, from the state attributes themselves. ─── */
.fui-choice__input:focus-visible,
.fui-switch__input:focus-visible {
  outline: 2px solid var(--color-primary, #4F46E5);
  outline-offset: 2px;
}
.fui-choice__input[aria-invalid="true"],
.fui-switch__input[aria-invalid="true"] {
  border-color: var(--color-danger, #DC2626);
}
.fui-choice__input:disabled,
.fui-switch__input:disabled {
  opacity: 0.55;
  cursor: not-allowed;
}
.fui-choice:has(.fui-choice__input:disabled),
.fui-switch:has(.fui-switch__input:disabled) {
  opacity: 0.55;
  cursor: not-allowed;
}

/* ─── The errored-run shell: a standalone choice with a message. ─── */
.fui-choice-field {
  display: grid;
  gap: var(--spacing-xs, 2px);
  justify-items: start;
}
.fui-choice-field__error,
.fui-choice-field__hint {
  margin: 0;
  font-size: var(--text-sm, 0.875rem);
}
.fui-choice-field__error { color: var(--color-danger, #DC2626); }
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
  font-weight: 500;
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
	return `[data-fui-comp="ui-tooltip"] {
  position: relative;
  display: inline-block;
  /* Allow keyboard focus on the wrapper for :focus-within to reveal
     the pop without requiring focus on a particular descendant
     element. */
}
[data-fui-comp="ui-tooltip"] .fui-tooltip__pop {
  position: absolute;
  inset-block-end: calc(100% + 6px);
  inset-inline-start: 50%;
  transform: translateX(-50%) translateY(4px);
  background: var(--color-text, #18181B);
  color: var(--color-surface, #FFFFFF);
  padding: var(--spacing-xs, 2px) var(--spacing-sm, 4px);
  font-size: var(--text-xs, 0.75rem);
  line-height: 1.2;
  border-radius: var(--radii-sm, 4px);
  pointer-events: none;
  opacity: 0;
  visibility: hidden;
  white-space: nowrap;
  max-inline-size: 240px;
  z-index: var(--zindex-popover, 400);
  transition: opacity var(--duration-fast, 150ms) ease,
              transform var(--duration-fast, 150ms) ease,
              visibility 0s var(--duration-fast, 150ms);
}
[data-fui-comp="ui-tooltip"]:hover .fui-tooltip__pop,
[data-fui-comp="ui-tooltip"]:focus-within .fui-tooltip__pop {
  opacity: 1;
  visibility: visible;
  transform: translateX(-50%) translateY(0);
  transition-delay: 0s;
}

[data-fui-comp="ui-tooltip"].fui-tooltip--bottom .fui-tooltip__pop {
  inset-block-end: auto;
  inset-block-start: calc(100% + 6px);
  transform: translateX(-50%) translateY(-4px);
}
[data-fui-comp="ui-tooltip"].fui-tooltip--bottom:hover .fui-tooltip__pop,
[data-fui-comp="ui-tooltip"].fui-tooltip--bottom:focus-within .fui-tooltip__pop {
  transform: translateX(-50%) translateY(0);
}
[data-fui-comp="ui-tooltip"].fui-tooltip--left .fui-tooltip__pop {
  inset-block-end: 50%;
  inset-inline-start: auto;
  inset-inline-end: calc(100% + 6px);
  transform: translateY(50%) translateX(4px);
}
[data-fui-comp="ui-tooltip"].fui-tooltip--left:hover .fui-tooltip__pop,
[data-fui-comp="ui-tooltip"].fui-tooltip--left:focus-within .fui-tooltip__pop {
  transform: translateY(50%) translateX(0);
}
[data-fui-comp="ui-tooltip"].fui-tooltip--right .fui-tooltip__pop {
  inset-block-end: 50%;
  inset-inline-start: calc(100% + 6px);
  transform: translateY(50%) translateX(-4px);
}
[data-fui-comp="ui-tooltip"].fui-tooltip--right:hover .fui-tooltip__pop,
[data-fui-comp="ui-tooltip"].fui-tooltip--right:focus-within .fui-tooltip__pop {
  transform: translateY(50%) translateX(0);
}

@media (prefers-reduced-motion: reduce) {
  [data-fui-comp="ui-tooltip"] .fui-tooltip__pop { transition: none; transform: translateX(-50%) translateY(0); }
}`
}

// ─── Tag / Chip ─────────────────────────────────────────────────────

func tagCSS(t style.Theme) string {
	return `[data-fui-comp="ui-tag"] {
  display: inline-flex;
  align-items: center;
  gap: var(--spacing-xs, 2px);
  padding: var(--spacing-xs, 2px) var(--spacing-md, 8px);
  border: 1px solid transparent;
  border-radius: var(--radii-full, 9999px);
  font-size: var(--text-xs, 0.75rem);
  font-weight: 500;
  line-height: 1.3;
  text-decoration: none;
}
[data-fui-comp="ui-tag"].fui-tag--neutral {
  background: var(--color-surface-soft, #F4F4F5);
  color: var(--color-text, #18181B);
  border-color: var(--color-border, #E4E4E7);
}
[data-fui-comp="ui-tag"].fui-tag--success {
  background: color-mix(in oklab, var(--color-success, #16A34A) 15%, var(--color-surface, #fff) 85%);
  color: var(--color-success, #16A34A);
  border-color: color-mix(in oklab, var(--color-success, #16A34A) 30%, var(--color-surface, #fff) 70%);
}
[data-fui-comp="ui-tag"].fui-tag--warning {
  background: color-mix(in oklab, var(--color-warning, #CA8A04) 15%, var(--color-surface, #fff) 85%);
  color: var(--color-warning, #CA8A04);
  border-color: color-mix(in oklab, var(--color-warning, #CA8A04) 30%, var(--color-surface, #fff) 70%);
}
[data-fui-comp="ui-tag"].fui-tag--danger {
  background: color-mix(in oklab, var(--color-danger, #DC2626) 15%, var(--color-surface, #fff) 85%);
  color: var(--color-danger, #DC2626);
  border-color: color-mix(in oklab, var(--color-danger, #DC2626) 30%, var(--color-surface, #fff) 70%);
}
[data-fui-comp="ui-tag"].fui-tag--info {
  background: color-mix(in oklab, var(--color-info, #2563EB) 15%, var(--color-surface, #fff) 85%);
  color: var(--color-info, #2563EB);
  border-color: color-mix(in oklab, var(--color-info, #2563EB) 30%, var(--color-surface, #fff) 70%);
}
[data-fui-comp="ui-tag"].fui-tag--interactive {
  cursor: pointer;
  transition: filter var(--duration-fast, 150ms) ease;
}
[data-fui-comp="ui-tag"].fui-tag--interactive:hover { filter: brightness(0.96); }
[data-fui-comp="ui-tag"]:focus-visible {
  outline: 2px solid var(--color-primary, #4F46E5);
  outline-offset: 1px;
}
[data-fui-comp="ui-tag"] .fui-tag__dismiss {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  inline-size: 1.1rem;
  block-size:  1.1rem;
  margin-inline-end: -4px;
  border: 0;
  background: transparent;
  color: inherit;
  cursor: pointer;
  border-radius: 50%;
  font-size: var(--text-base, 1rem);
  line-height: 1;
  padding: 0;
}
[data-fui-comp="ui-tag"] .fui-tag__dismiss:hover { background: rgba(0,0,0,0.08); }
[data-fui-comp="ui-tag"] .fui-tag__dismiss:focus-visible {
  outline: 2px solid var(--color-primary, #4F46E5);
  outline-offset: 1px;
}` + customStatusCSS("ui-tag", "fui-tag", t)
}

// ─── Spinner ────────────────────────────────────────────────────────

func spinnerCSS(_ style.Theme) string {
	return `[data-fui-comp="ui-spinner"] {
  display: flex;
  align-items: center;
  justify-content: center;
  gap: var(--spacing-sm, 4px);
  --ui-spinner-size: 1.5rem;
}
[data-fui-comp="ui-spinner"].fui-spinner--sm { --ui-spinner-size: 1rem; }
[data-fui-comp="ui-spinner"].fui-spinner--lg { --ui-spinner-size: 2.5rem; }
[data-fui-comp="ui-spinner"].fui-spinner--inline { display: inline-flex; }
[data-fui-comp="ui-spinner"] .fui-spinner__ring {
  display: inline-block;
  inline-size: var(--ui-spinner-size);
  block-size:  var(--ui-spinner-size);
  border-radius: 50%;
  border: 2px solid var(--color-border, #E4E4E7);
  border-top-color: var(--color-primary, #4F46E5);
  animation: fui-spinner-rotate var(--duration-slow, 400ms) linear infinite;
}
[data-fui-comp="ui-spinner"] .fui-spinner__dots {
  display: inline-flex;
  align-items: center;
  gap: var(--spacing-sm, 4px);
}
[data-fui-comp="ui-spinner"] .fui-spinner__dot {
  display: inline-block;
  inline-size: calc(var(--ui-spinner-size) * 0.28);
  block-size:  calc(var(--ui-spinner-size) * 0.28);
  border-radius: 50%;
  background: var(--color-primary, #4F46E5);
  animation: fui-spinner-pulse 1.2s ease-in-out infinite both;
}
[data-fui-comp="ui-spinner"] .fui-spinner__dot:nth-child(1) { animation-delay: -0.32s; }
[data-fui-comp="ui-spinner"] .fui-spinner__dot:nth-child(2) { animation-delay: -0.16s; }

/* SpinnerGrid — 3×3 cells with a diagonal-ripple delay schedule. */
[data-fui-comp="ui-spinner"] .fui-spinner__grid {
  display: grid;
  grid-template-columns: repeat(3, 1fr);
  gap: calc(var(--ui-spinner-size) * 0.08);
  inline-size: var(--ui-spinner-size);
  block-size:  var(--ui-spinner-size);
}
[data-fui-comp="ui-spinner"] .fui-spinner__cell {
  display: block;
  background: var(--color-primary, #4F46E5);
  border-radius: 2px;
  animation: fui-spinner-grid 1.3s ease-in-out infinite both;
}
[data-fui-comp="ui-spinner"] .fui-spinner__cell:nth-child(1) { animation-delay: 0.0s; }
[data-fui-comp="ui-spinner"] .fui-spinner__cell:nth-child(2) { animation-delay: 0.1s; }
[data-fui-comp="ui-spinner"] .fui-spinner__cell:nth-child(3) { animation-delay: 0.2s; }
[data-fui-comp="ui-spinner"] .fui-spinner__cell:nth-child(4) { animation-delay: 0.1s; }
[data-fui-comp="ui-spinner"] .fui-spinner__cell:nth-child(5) { animation-delay: 0.2s; }
[data-fui-comp="ui-spinner"] .fui-spinner__cell:nth-child(6) { animation-delay: 0.3s; }
[data-fui-comp="ui-spinner"] .fui-spinner__cell:nth-child(7) { animation-delay: 0.2s; }
[data-fui-comp="ui-spinner"] .fui-spinner__cell:nth-child(8) { animation-delay: 0.3s; }
[data-fui-comp="ui-spinner"] .fui-spinner__cell:nth-child(9) { animation-delay: 0.4s; }

@keyframes fui-spinner-rotate {
  to { transform: rotate(360deg); }
}
@keyframes fui-spinner-pulse {
  0%, 80%, 100% { opacity: 0.2; transform: scale(0.8); }
  40%           { opacity: 1;   transform: scale(1); }
}
@keyframes fui-spinner-grid {
  0%, 70%, 100% { opacity: 0.2; transform: scale(0.7); }
  35%           { opacity: 1;   transform: scale(1); }
}

@media (prefers-reduced-motion: reduce) {
  [data-fui-comp="ui-spinner"] .fui-spinner__ring,
  [data-fui-comp="ui-spinner"] .fui-spinner__dot,
  [data-fui-comp="ui-spinner"] .fui-spinner__cell {
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
	return `[data-fui-comp="ui-divider"] {
  border: 0;
  background: var(--color-border, #E4E4E7);
}
hr[data-fui-comp="ui-divider"] {
  block-size: 1px;
  inline-size: 100%;
  margin: var(--spacing-md, 8px) 0;
}
[data-fui-comp="ui-divider"].fui-divider--vertical {
  display: inline-block;
  inline-size: 1px;
  block-size: 1em;
  margin: 0 var(--spacing-sm, 4px);
  vertical-align: middle;
}
[data-fui-comp="ui-divider"].fui-divider--labelled {
  display: flex;
  align-items: center;
  gap: var(--spacing-md, 8px);
  margin: var(--spacing-md, 8px) 0;
  background: transparent;
  color: var(--color-text-muted, #52525B);
  font-size: var(--text-sm, 0.875rem);
  font-weight: 500;
}
[data-fui-comp="ui-divider"] .fui-divider__line {
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
	return `[data-fui-comp="ui-sticky"] {
  position: -webkit-sticky;
  position: sticky;
  z-index: var(--z-sticky, 200);
}
[data-fui-comp="ui-sticky"][data-fui-z-tier="dropdown"] { z-index: var(--z-dropdown, 100); }
[data-fui-comp="ui-sticky"][data-fui-z-tier="modal"]    { z-index: var(--z-modal, 300); }
[data-fui-comp="ui-sticky"][data-fui-z-tier="popover"]  { z-index: var(--z-popover, 400); }
[data-fui-comp="ui-sticky"][data-fui-z-tier="toast"]    { z-index: var(--z-toast, 500); }
[data-fui-comp="ui-sticky"]::after {
  content: "";
  position: absolute;
  left: 0;
  right: 0;
  bottom: -1px;
  height: 1px;
  background: var(--color-border, #E4E4E7);
  opacity: 0;
  transition: opacity 0.15s;
}
/* Edge offsets */
[data-fui-comp="ui-sticky"].fui-sticky--top { top: 0; }
[data-fui-comp="ui-sticky"].fui-sticky--bottom { bottom: 0; }
[data-fui-comp="ui-sticky"].fui-sticky--offset-sm { top: var(--spacing-sm, 4px); }
[data-fui-comp="ui-sticky"].fui-sticky--offset-md { top: var(--spacing-md, 8px); }
[data-fui-comp="ui-sticky"].fui-sticky--offset-lg { top: var(--spacing-lg, 16px); }
[data-fui-comp="ui-sticky"].fui-sticky--offset-xl { top: var(--spacing-xl, 24px); }
/* Show a subtle bottom border when the element is stuck (only top-sticky) */
@supports ((position: -webkit-sticky) or (position: sticky)) {
  [data-fui-comp="ui-sticky"].fui-sticky--top:not(:is(:first-child))::after {
    opacity: 1;
  }
}`
}

func aspectRatioCSS(_ style.Theme) string {
	return `[data-fui-comp="ui-aspect-ratio"] {
  position: relative;
  width: 100%;
}
[data-fui-comp="ui-aspect-ratio"] > * {
  position: absolute;
  inset: 0;
  width: 100%;
  height: 100%;
}
[data-fui-comp="ui-aspect-ratio"].fui-ar--1-1  { aspect-ratio: 1 / 1; }
[data-fui-comp="ui-aspect-ratio"].fui-ar--4-3  { aspect-ratio: 4 / 3; }
[data-fui-comp="ui-aspect-ratio"].fui-ar--16-9 { aspect-ratio: 16 / 9; }
[data-fui-comp="ui-aspect-ratio"].fui-ar--21-9 { aspect-ratio: 21 / 9; }
[data-fui-comp="ui-aspect-ratio"].fui-ar--3-4  { aspect-ratio: 3 / 4; }
[data-fui-comp="ui-aspect-ratio"].fui-ar--3-2  { aspect-ratio: 3 / 2; }
[data-fui-comp="ui-aspect-ratio"].fui-ar--2-3  { aspect-ratio: 2 / 3; }
/* auto: no aspect-ratio, child sizes naturally */
[data-fui-comp="ui-aspect-ratio"].fui-ar--auto > * {
  position: static;
  width: auto;
  height: auto;
}`
}
