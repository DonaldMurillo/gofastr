package ui

import (
	"github.com/DonaldMurillo/gofastr/core-ui/registry"
	"github.com/DonaldMurillo/gofastr/core-ui/style"
)

// Style handles for the 10 layout / primitive components added in
// the same commit. Each registers a scoped stylesheet that the SSR
// host and runtime load on first appearance — except where
// LoadAlways is explicitly opted into for chrome that's on every
// screen.

var (
	layoutStyle      = registry.RegisterStyle("ui-layout", layoutCSS)
	stickyStyle      = registry.RegisterStyle("ui-sticky", stickyCSS)
	aspectRatioStyle = registry.RegisterStyle("ui-aspect-ratio", aspectRatioCSS)
	cardStyle       = registry.RegisterStyle("ui-card", cardCSS)
	imageStyle      = registry.RegisterStyle("ui-image", imageCSS)
	toggleStyle     = registry.RegisterStyle("ui-toggle", toggleCSS)
	tooltipStyle    = registry.RegisterStyle("ui-tooltip", tooltipCSS)
	tagStyle        = registry.RegisterStyle("ui-tag", tagCSS)
	spinnerStyle    = registry.RegisterStyle("ui-spinner", spinnerCSS)
	dividerStyle    = registry.RegisterStyle("ui-divider", dividerCSS)
	fileUploadStyle = registry.RegisterStyle("ui-fileupload", fileUploadCSS)
)

// ─── Layout ─────────────────────────────────────────────────────────

func layoutCSS(_ style.Theme) string {
	// Layout primitives stack data-fui-comp="ui-layout" AND their own
	// class on the SAME element (Stack, Cluster, Grid, … are sibling
	// classes — not descendants). Selectors below combine the marker
	// and class on the same element via `[data-fui-comp="ui-layout"].<class>`
	// rather than the descendant `[data-fui-comp="ui-layout"] .<class>`.
	return `[data-fui-comp="ui-layout"] {
  box-sizing: border-box;
}
[data-fui-comp="ui-layout"].ui-stack {
  display: flex;
  flex-direction: column;
  gap: var(--spacing-md, 8px);
}
[data-fui-comp="ui-layout"].ui-cluster {
  display: flex;
  flex-direction: row;
  flex-wrap: wrap;
  gap: var(--spacing-md, 8px);
  align-items: center;
}
[data-fui-comp="ui-layout"].ui-cluster--nowrap { flex-wrap: nowrap; }

[data-fui-comp="ui-layout"].ui-grid {
  display: grid;
  gap: var(--spacing-md, 8px);
  grid-template-columns: repeat(auto-fit, minmax(var(--ui-grid-min, 16rem), 1fr));
}

[data-fui-comp="ui-layout"].ui-center {
  display: flex;
  align-items: center;
  justify-content: center;
}
[data-fui-comp="ui-layout"].ui-center--viewport { min-block-size: 100vh; }
[data-fui-comp="ui-layout"].ui-center--screen   { min-block-size: 100dvh; }

[data-fui-comp="ui-layout"].ui-spacer {
  flex: 1 1 auto;
  align-self: stretch;
}

[data-fui-comp="ui-layout"].ui-box { background: transparent; }
[data-fui-comp="ui-layout"].ui-box--surface {
  background: var(--color-surface, #FFFFFF);
  border-radius: var(--radii-md, 8px);
}
[data-fui-comp="ui-layout"].ui-box--outlined {
  border: 1px solid var(--color-border, #E4E4E7);
  border-radius: var(--radii-md, 8px);
}
[data-fui-comp="ui-layout"].ui-box--pad-sm { padding: var(--spacing-sm, 4px); }
[data-fui-comp="ui-layout"].ui-box--pad-md { padding: var(--spacing-md, 8px); }
[data-fui-comp="ui-layout"].ui-box--pad-lg { padding: var(--spacing-lg, 16px); }
[data-fui-comp="ui-layout"].ui-box--pad-xl { padding: var(--spacing-xl, 24px); }

/* gap modifiers — apply to ui-stack/ui-cluster/ui-grid. */
[data-fui-comp="ui-layout"].ui-layout--gap-none { gap: 0; }
[data-fui-comp="ui-layout"].ui-layout--gap-xs   { gap: var(--spacing-xs, 2px); }
[data-fui-comp="ui-layout"].ui-layout--gap-sm   { gap: var(--spacing-sm, 4px); }
[data-fui-comp="ui-layout"].ui-layout--gap-lg   { gap: var(--spacing-lg, 16px); }
[data-fui-comp="ui-layout"].ui-layout--gap-xl   { gap: var(--spacing-xl, 24px); }
[data-fui-comp="ui-layout"].ui-layout--gap-2xl  { gap: var(--spacing-2xl, 32px); }

/* alignment modifiers. */
[data-fui-comp="ui-layout"].ui-layout--align-start    { align-items: flex-start; }
[data-fui-comp="ui-layout"].ui-layout--align-center   { align-items: center; }
[data-fui-comp="ui-layout"].ui-layout--align-end      { align-items: flex-end; }
[data-fui-comp="ui-layout"].ui-layout--align-baseline { align-items: baseline; }
[data-fui-comp="ui-layout"].ui-layout--align-stretch  { align-items: stretch; }

[data-fui-comp="ui-layout"].ui-layout--justify-start   { justify-content: flex-start; }
[data-fui-comp="ui-layout"].ui-layout--justify-center  { justify-content: center; }
[data-fui-comp="ui-layout"].ui-layout--justify-end     { justify-content: flex-end; }
[data-fui-comp="ui-layout"].ui-layout--justify-between { justify-content: space-between; }
[data-fui-comp="ui-layout"].ui-layout--justify-around  { justify-content: space-around; }`
}

// ─── Card ───────────────────────────────────────────────────────────

func cardCSS(_ style.Theme) string {
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
[data-fui-comp="ui-card"].ui-card--outlined {
  box-shadow: none;
  border: 1px solid var(--color-border, #E4E4E7);
}
[data-fui-comp="ui-card"].ui-card--flat {
  box-shadow: none;
  background: transparent;
}
[data-fui-comp="ui-card"].ui-card--interactive {
  transition: transform var(--duration-fast, 150ms) ease,
              box-shadow var(--duration-fast, 150ms) ease;
  cursor: pointer;
}
[data-fui-comp="ui-card"].ui-card--interactive:hover {
  transform: translateY(-2px);
  box-shadow: var(--shadows-md, 0 4px 6px -1px rgba(0,0,0,0.10));
}
[data-fui-comp="ui-card"].ui-card--interactive:focus-visible {
  outline: 2px solid var(--color-primary, #4F46E5);
  outline-offset: 2px;
}
[data-fui-comp="ui-card"] .ui-card__inner {
  display: flex;
  flex-direction: column;
}
[data-fui-comp="ui-card"] .ui-card__header {
  padding: var(--spacing-lg, 16px) var(--spacing-lg, 16px) var(--spacing-md, 8px);
  display: flex;
  flex-direction: column;
  gap: var(--spacing-xs, 2px);
}
[data-fui-comp="ui-card"] .ui-card__heading {
  margin: 0;
  font-size: 1.05rem;
  font-weight: 600;
  color: var(--color-text, #18181B);
}
[data-fui-comp="ui-card"] .ui-card__description {
  margin: 0;
  font-size: 0.9rem;
  color: var(--color-text-muted, #52525B);
}
[data-fui-comp="ui-card"] .ui-card__body {
  padding: var(--spacing-md, 8px) var(--spacing-lg, 16px);
  color: var(--color-text, #18181B);
  flex: 1 1 auto;
}
[data-fui-comp="ui-card"] .ui-card__header + .ui-card__body {
  padding-top: 0;
}
[data-fui-comp="ui-card"] .ui-card__footer {
  padding: var(--spacing-md, 8px) var(--spacing-lg, 16px);
  border-top: 1px solid var(--color-border, #E4E4E7);
  display: flex;
  align-items: center;
  gap: var(--spacing-sm, 4px);
  background: var(--color-surface-soft, #F4F4F5);
}
[data-fui-comp="ui-card"].ui-card--flat .ui-card__footer { background: transparent; }`
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
[data-fui-comp="ui-image"] .ui-image__img {
  display: block;
  max-inline-size: 100%;
  block-size: auto;
  object-fit: cover;
  background: var(--color-surface-soft, #F4F4F5);
}
[data-fui-comp="ui-image"].ui-image--fit-contain .ui-image__img { object-fit: contain; }
[data-fui-comp="ui-image"].ui-image--fit-fill    .ui-image__img { object-fit: fill;    }
[data-fui-comp="ui-image"].ui-image--rounded     .ui-image__img,
[data-fui-comp="ui-image"].ui-image--rounded     picture {
  border-radius: var(--radii-md, 8px);
}
[data-fui-comp="ui-image"].ui-image--aspect-1-1  .ui-image__img { aspect-ratio: 1 / 1;  inline-size: 100%; block-size: auto; }
[data-fui-comp="ui-image"].ui-image--aspect-4-3  .ui-image__img { aspect-ratio: 4 / 3;  inline-size: 100%; block-size: auto; }
[data-fui-comp="ui-image"].ui-image--aspect-16-9 .ui-image__img { aspect-ratio: 16 / 9; inline-size: 100%; block-size: auto; }
[data-fui-comp="ui-image"].ui-image--aspect-21-9 .ui-image__img { aspect-ratio: 21 / 9; inline-size: 100%; block-size: auto; }
[data-fui-comp="ui-image"].ui-image--aspect-3-4  .ui-image__img { aspect-ratio: 3 / 4;  inline-size: 100%; block-size: auto; }

/* Decorative class — allows alt="" without alt-text warnings. */
[data-fui-comp="ui-image"].ui-image--decorative .ui-image__img {
  /* visual same as default; the marker exists for the linter / a11y
     audit to know empty alt is intentional. */
}`
}

// ─── Toggle (Checkbox / Radio / Switch) ─────────────────────────────

func toggleCSS(_ style.Theme) string {
	return `[data-fui-comp="ui-toggle"] {
  display: inline-flex;
  /* flex-wrap so the help/error pseudo-line below wraps to a new
     line instead of forcing itself between the control and the
     label text. */
  flex-wrap: wrap;
  align-items: center;
  gap: var(--spacing-md, 8px);
  cursor: pointer;
  /* Token-scaled touch target. */
  min-block-size: var(--spacing-touch-target, 44px);
  padding-block: 4px;
}
[data-fui-comp="ui-toggle"].is-disabled { opacity: 0.55; cursor: not-allowed; }

[data-fui-comp="ui-toggle"] .ui-toggle__control {
  position: relative;
  flex-shrink: 0;
  inline-size: 1.25rem;
  block-size:  1.25rem;
  display: inline-flex;
  align-items: center;
  justify-content: center;
}
[data-fui-comp="ui-toggle"] .ui-toggle__input {
  position: absolute;
  inset: 0;
  inline-size: 100%;
  block-size: 100%;
  margin: 0;
  opacity: 0;
  cursor: inherit;
}
[data-fui-comp="ui-toggle"] .ui-toggle__indicator {
  /* inline-flex so the ::after marker (checkmark / radio dot) is
     centered both axes regardless of variant. inline-block lets the
     pseudo-element drift to top-left in some browsers — visible
     bug. */
  display: inline-flex;
  align-items: center;
  justify-content: center;
  inline-size: 1.1rem;
  block-size:  1.1rem;
  border: 1.5px solid var(--color-border-strong, #A1A1AA);
  background: var(--color-surface, #FFFFFF);
  transition: background var(--duration-fast, 150ms) ease,
              border-color var(--duration-fast, 150ms) ease;
}

/* ─── Checkbox ─── */
[data-fui-comp="ui-toggle"].ui-toggle--checkbox .ui-toggle__indicator {
  border-radius: var(--radii-sm, 4px);
}
[data-fui-comp="ui-toggle"].ui-toggle--checkbox .ui-toggle__input:checked + .ui-toggle__indicator {
  background: var(--color-primary, #4F46E5);
  border-color: var(--color-primary, #4F46E5);
}
[data-fui-comp="ui-toggle"].ui-toggle--checkbox .ui-toggle__input:checked + .ui-toggle__indicator::after {
  content: "";
  display: block;
  inline-size: 0.35rem;
  block-size:  0.65rem;
  border: solid var(--color-primary-fg, #FFFFFF);
  border-width: 0 2px 2px 0;
  transform: translateY(-1px) rotate(45deg);
}

/* ─── Radio ─── */
[data-fui-comp="ui-toggle"].ui-toggle--radio .ui-toggle__indicator {
  border-radius: 50%;
}
[data-fui-comp="ui-toggle"].ui-toggle--radio .ui-toggle__input:checked + .ui-toggle__indicator {
  border-color: var(--color-primary, #4F46E5);
}
[data-fui-comp="ui-toggle"].ui-toggle--radio .ui-toggle__input:checked + .ui-toggle__indicator::after {
  content: "";
  inline-size: 0.55rem;
  block-size: 0.55rem;
  border-radius: 50%;
  background: var(--color-primary, #4F46E5);
}

/* ─── Switch ─── */
[data-fui-comp="ui-toggle"].ui-toggle--switch .ui-toggle__control {
  inline-size: 2.25rem;
  block-size:  1.25rem;
}
[data-fui-comp="ui-toggle"].ui-toggle--switch .ui-toggle__indicator {
  inline-size: 2.25rem;
  block-size:  1.25rem;
  border-radius: 9999px;
  background: var(--color-surface-soft, #F4F4F5);
  border-color: var(--color-border, #E4E4E7);
  position: relative;
}
[data-fui-comp="ui-toggle"].ui-toggle--switch .ui-toggle__indicator::after {
  content: "";
  position: absolute;
  inset-block-start: 50%;
  inset-inline-start: 2px;
  inline-size: 1rem;
  block-size: 1rem;
  border-radius: 50%;
  background: #FFFFFF;
  transform: translateY(-50%);
  transition: inset-inline-start var(--duration-fast, 150ms) ease;
  box-shadow: 0 1px 2px rgba(0,0,0,0.2);
}
[data-fui-comp="ui-toggle"].ui-toggle--switch .ui-toggle__input:checked + .ui-toggle__indicator {
  background: var(--color-primary, #4F46E5);
  border-color: var(--color-primary, #4F46E5);
}
[data-fui-comp="ui-toggle"].ui-toggle--switch .ui-toggle__input:checked + .ui-toggle__indicator::after {
  inset-inline-start: calc(100% - 1.125rem);
}

/* ─── Focus ring shared across all variants ─── */
[data-fui-comp="ui-toggle"] .ui-toggle__input:focus-visible + .ui-toggle__indicator {
  outline: 2px solid var(--color-primary, #4F46E5);
  outline-offset: 2px;
}
[data-fui-comp="ui-toggle"].is-error .ui-toggle__indicator {
  border-color: var(--color-danger, #DC2626);
}

[data-fui-comp="ui-toggle"] .ui-toggle__label {
  flex: 1 1 auto;
  font-size: 0.95rem;
  color: var(--color-text, #18181B);
  line-height: 1.4;
}
[data-fui-comp="ui-toggle"] .ui-toggle__help,
[data-fui-comp="ui-toggle"] .ui-toggle__error {
  display: block;
  flex-basis: 100%;
  margin: var(--spacing-xs, 2px) 0 0 calc(1.25rem + var(--spacing-md, 8px));
  font-size: 0.85rem;
}
[data-fui-comp="ui-toggle"] .ui-toggle__help  { color: var(--color-text-muted, #52525B); }
[data-fui-comp="ui-toggle"] .ui-toggle__error { color: var(--color-danger, #DC2626); }

/* ─── Toggle Group (fieldset wrapper for RadioGroup / CheckboxGroup) ─── */
.ui-toggle-group {
  border: none;
  padding: 0;
  margin: 0;
  display: grid;
  gap: var(--spacing-sm, 8px);
}
.ui-toggle-group .ui-toggle-group__legend {
  font-weight: 500;
  font-size: 0.9rem;
  color: var(--color-text, #18181B);
  padding: 0;
  margin-bottom: var(--spacing-xs, 4px);
}
.ui-toggle-group .ui-toggle-group__help {
  margin: 0;
  font-size: 0.85rem;
  color: var(--color-text-muted, #52525B);
}
.ui-toggle-group .ui-toggle-group__error {
  margin: 0;
  font-size: 0.85rem;
  color: var(--color-danger, #DC2626);
}`
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
[data-fui-comp="ui-tooltip"] .ui-tooltip__pop {
  position: absolute;
  inset-block-end: calc(100% + 6px);
  inset-inline-start: 50%;
  transform: translateX(-50%) translateY(4px);
  background: var(--color-text, #18181B);
  color: var(--color-surface, #FFFFFF);
  padding: var(--spacing-xs, 2px) var(--spacing-sm, 4px);
  font-size: 0.8rem;
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
[data-fui-comp="ui-tooltip"]:hover .ui-tooltip__pop,
[data-fui-comp="ui-tooltip"]:focus-within .ui-tooltip__pop {
  opacity: 1;
  visibility: visible;
  transform: translateX(-50%) translateY(0);
  transition-delay: 0s;
}

[data-fui-comp="ui-tooltip"].ui-tooltip--bottom .ui-tooltip__pop {
  inset-block-end: auto;
  inset-block-start: calc(100% + 6px);
  transform: translateX(-50%) translateY(-4px);
}
[data-fui-comp="ui-tooltip"].ui-tooltip--bottom:hover .ui-tooltip__pop,
[data-fui-comp="ui-tooltip"].ui-tooltip--bottom:focus-within .ui-tooltip__pop {
  transform: translateX(-50%) translateY(0);
}
[data-fui-comp="ui-tooltip"].ui-tooltip--left .ui-tooltip__pop {
  inset-block-end: 50%;
  inset-inline-start: auto;
  inset-inline-end: calc(100% + 6px);
  transform: translateY(50%) translateX(4px);
}
[data-fui-comp="ui-tooltip"].ui-tooltip--left:hover .ui-tooltip__pop,
[data-fui-comp="ui-tooltip"].ui-tooltip--left:focus-within .ui-tooltip__pop {
  transform: translateY(50%) translateX(0);
}
[data-fui-comp="ui-tooltip"].ui-tooltip--right .ui-tooltip__pop {
  inset-block-end: 50%;
  inset-inline-start: calc(100% + 6px);
  transform: translateY(50%) translateX(-4px);
}
[data-fui-comp="ui-tooltip"].ui-tooltip--right:hover .ui-tooltip__pop,
[data-fui-comp="ui-tooltip"].ui-tooltip--right:focus-within .ui-tooltip__pop {
  transform: translateY(50%) translateX(0);
}

@media (prefers-reduced-motion: reduce) {
  [data-fui-comp="ui-tooltip"] .ui-tooltip__pop { transition: none; transform: translateX(-50%) translateY(0); }
}`
}

// ─── Tag / Chip ─────────────────────────────────────────────────────

func tagCSS(_ style.Theme) string {
	return `[data-fui-comp="ui-tag"] {
  display: inline-flex;
  align-items: center;
  gap: var(--spacing-xs, 2px);
  padding: 2px var(--spacing-md, 8px);
  border: 1px solid transparent;
  border-radius: var(--radii-full, 9999px);
  font-size: 0.8rem;
  font-weight: 500;
  line-height: 1.3;
  text-decoration: none;
}
[data-fui-comp="ui-tag"].ui-tag--neutral {
  background: var(--color-surface-soft, #F4F4F5);
  color: var(--color-text, #18181B);
  border-color: var(--color-border, #E4E4E7);
}
[data-fui-comp="ui-tag"].ui-tag--success {
  background: color-mix(in oklab, var(--color-success, #16A34A) 15%, var(--color-surface, #fff) 85%);
  color: var(--color-success, #16A34A);
  border-color: color-mix(in oklab, var(--color-success, #16A34A) 30%, var(--color-surface, #fff) 70%);
}
[data-fui-comp="ui-tag"].ui-tag--warning {
  background: color-mix(in oklab, var(--color-warning, #CA8A04) 15%, var(--color-surface, #fff) 85%);
  color: var(--color-warning, #CA8A04);
  border-color: color-mix(in oklab, var(--color-warning, #CA8A04) 30%, var(--color-surface, #fff) 70%);
}
[data-fui-comp="ui-tag"].ui-tag--danger {
  background: color-mix(in oklab, var(--color-danger, #DC2626) 15%, var(--color-surface, #fff) 85%);
  color: var(--color-danger, #DC2626);
  border-color: color-mix(in oklab, var(--color-danger, #DC2626) 30%, var(--color-surface, #fff) 70%);
}
[data-fui-comp="ui-tag"].ui-tag--info {
  background: color-mix(in oklab, var(--color-info, #2563EB) 15%, var(--color-surface, #fff) 85%);
  color: var(--color-info, #2563EB);
  border-color: color-mix(in oklab, var(--color-info, #2563EB) 30%, var(--color-surface, #fff) 70%);
}
[data-fui-comp="ui-tag"].ui-tag--interactive {
  cursor: pointer;
  transition: filter var(--duration-fast, 150ms) ease;
}
[data-fui-comp="ui-tag"].ui-tag--interactive:hover { filter: brightness(0.96); }
[data-fui-comp="ui-tag"]:focus-visible {
  outline: 2px solid var(--color-primary, #4F46E5);
  outline-offset: 1px;
}
[data-fui-comp="ui-tag"] .ui-tag__dismiss {
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
  font-size: 1rem;
  line-height: 1;
  padding: 0;
}
[data-fui-comp="ui-tag"] .ui-tag__dismiss:hover { background: rgba(0,0,0,0.08); }
[data-fui-comp="ui-tag"] .ui-tag__dismiss:focus-visible {
  outline: 2px solid var(--color-primary, #4F46E5);
  outline-offset: 1px;
}`
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
[data-fui-comp="ui-spinner"].ui-spinner--sm { --ui-spinner-size: 1rem; }
[data-fui-comp="ui-spinner"].ui-spinner--lg { --ui-spinner-size: 2.5rem; }
[data-fui-comp="ui-spinner"].ui-spinner--inline { display: inline-flex; }
[data-fui-comp="ui-spinner"] .ui-spinner__ring {
  display: inline-block;
  inline-size: var(--ui-spinner-size);
  block-size:  var(--ui-spinner-size);
  border-radius: 50%;
  border: 2px solid var(--color-border, #E4E4E7);
  border-top-color: var(--color-primary, #4F46E5);
  animation: ui-spinner-rotate var(--duration-slow, 800ms) linear infinite;
}
[data-fui-comp="ui-spinner"] .ui-spinner__dots {
  display: inline-flex;
  align-items: center;
  gap: 4px;
}
[data-fui-comp="ui-spinner"] .ui-spinner__dot {
  display: inline-block;
  inline-size: calc(var(--ui-spinner-size) * 0.28);
  block-size:  calc(var(--ui-spinner-size) * 0.28);
  border-radius: 50%;
  background: var(--color-primary, #4F46E5);
  animation: ui-spinner-pulse 1.2s ease-in-out infinite both;
}
[data-fui-comp="ui-spinner"] .ui-spinner__dot:nth-child(1) { animation-delay: -0.32s; }
[data-fui-comp="ui-spinner"] .ui-spinner__dot:nth-child(2) { animation-delay: -0.16s; }

/* SpinnerGrid — 3×3 cells with a diagonal-ripple delay schedule. */
[data-fui-comp="ui-spinner"] .ui-spinner__grid {
  display: grid;
  grid-template-columns: repeat(3, 1fr);
  gap: calc(var(--ui-spinner-size) * 0.08);
  inline-size: var(--ui-spinner-size);
  block-size:  var(--ui-spinner-size);
}
[data-fui-comp="ui-spinner"] .ui-spinner__cell {
  display: block;
  background: var(--color-primary, #4F46E5);
  border-radius: 2px;
  animation: ui-spinner-grid 1.3s ease-in-out infinite both;
}
[data-fui-comp="ui-spinner"] .ui-spinner__cell:nth-child(1) { animation-delay: 0.0s; }
[data-fui-comp="ui-spinner"] .ui-spinner__cell:nth-child(2) { animation-delay: 0.1s; }
[data-fui-comp="ui-spinner"] .ui-spinner__cell:nth-child(3) { animation-delay: 0.2s; }
[data-fui-comp="ui-spinner"] .ui-spinner__cell:nth-child(4) { animation-delay: 0.1s; }
[data-fui-comp="ui-spinner"] .ui-spinner__cell:nth-child(5) { animation-delay: 0.2s; }
[data-fui-comp="ui-spinner"] .ui-spinner__cell:nth-child(6) { animation-delay: 0.3s; }
[data-fui-comp="ui-spinner"] .ui-spinner__cell:nth-child(7) { animation-delay: 0.2s; }
[data-fui-comp="ui-spinner"] .ui-spinner__cell:nth-child(8) { animation-delay: 0.3s; }
[data-fui-comp="ui-spinner"] .ui-spinner__cell:nth-child(9) { animation-delay: 0.4s; }

@keyframes ui-spinner-rotate {
  to { transform: rotate(360deg); }
}
@keyframes ui-spinner-pulse {
  0%, 80%, 100% { opacity: 0.2; transform: scale(0.8); }
  40%           { opacity: 1;   transform: scale(1); }
}
@keyframes ui-spinner-grid {
  0%, 70%, 100% { opacity: 0.2; transform: scale(0.7); }
  35%           { opacity: 1;   transform: scale(1); }
}

@media (prefers-reduced-motion: reduce) {
  [data-fui-comp="ui-spinner"] .ui-spinner__ring,
  [data-fui-comp="ui-spinner"] .ui-spinner__dot,
  [data-fui-comp="ui-spinner"] .ui-spinner__cell {
    animation-duration: 2.4s;
  }
}

.ui-visually-hidden {
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
[data-fui-comp="ui-divider"].ui-divider--vertical {
  display: inline-block;
  inline-size: 1px;
  block-size: 1em;
  margin: 0 var(--spacing-sm, 4px);
  vertical-align: middle;
}
[data-fui-comp="ui-divider"].ui-divider--labelled {
  display: flex;
  align-items: center;
  gap: var(--spacing-md, 8px);
  margin: var(--spacing-md, 8px) 0;
  background: transparent;
  color: var(--color-text-muted, #52525B);
  font-size: 0.85rem;
  font-weight: 500;
}
[data-fui-comp="ui-divider"].ui-divider--labelled::before,
[data-fui-comp="ui-divider"].ui-divider--labelled::after {
  content: "";
  flex: 1 1 0;
  block-size: 1px;
  background: var(--color-border, #E4E4E7);
}`
}

// ─── FileUpload ─────────────────────────────────────────────────────

func fileUploadCSS(_ style.Theme) string {
	return `[data-fui-comp="ui-fileupload"] {
  display: flex;
  flex-direction: column;
  gap: var(--spacing-xs, 2px);
  cursor: pointer;
}
[data-fui-comp="ui-fileupload"].is-disabled { opacity: 0.6; cursor: not-allowed; }
[data-fui-comp="ui-fileupload"] .ui-fileupload__label {
  font-weight: 500;
  font-size: 0.9rem;
  color: var(--color-text, #18181B);
}
[data-fui-comp="ui-fileupload"] .ui-fileupload__zone {
  position: relative;
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  gap: var(--spacing-xs, 2px);
  padding: var(--spacing-xl, 24px);
  border: 2px dashed var(--color-border, #E4E4E7);
  border-radius: var(--radii-lg, 12px);
  background: var(--color-surface, #FFFFFF);
  color: var(--color-text-muted, #52525B);
  text-align: center;
  transition: border-color var(--duration-fast, 150ms) ease,
              background var(--duration-fast, 150ms) ease;
  min-block-size: calc(var(--spacing-touch-target, 44px) * 2);
}
[data-fui-comp="ui-fileupload"] .ui-fileupload__zone:hover {
  border-color: var(--color-primary, #4F46E5);
}
[data-fui-comp="ui-fileupload"].is-dragover .ui-fileupload__zone {
  border-color: var(--color-primary, #4F46E5);
  background: color-mix(in oklab, var(--color-primary, #4F46E5) 10%, var(--color-surface, #FFFFFF) 90%);
}
[data-fui-comp="ui-fileupload"].is-error .ui-fileupload__zone {
  border-color: var(--color-danger, #DC2626);
}
[data-fui-comp="ui-fileupload"] .ui-fileupload__input {
  position: absolute;
  inset: 0;
  inline-size: 100%;
  block-size: 100%;
  opacity: 0;
  cursor: inherit;
}
[data-fui-comp="ui-fileupload"] .ui-fileupload__input:focus-visible + .ui-fileupload__prompt {
  outline: 2px solid var(--color-primary, #4F46E5);
  outline-offset: 4px;
}
[data-fui-comp="ui-fileupload"] .ui-fileupload__prompt {
  margin: 0;
  font-size: 0.95rem;
  color: var(--color-text, #18181B);
}
[data-fui-comp="ui-fileupload"] .ui-fileupload__filename {
  margin: 0;
  font-size: 0.85rem;
  color: var(--color-text-muted, #52525B);
  font-weight: 600;
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: var(--spacing-sm, 4px);
}
[data-fui-comp="ui-fileupload"] .ui-fileupload__filename:empty { display: none; }
[data-fui-comp="ui-fileupload"] .ui-fileupload__filename .ui-fileupload__list {
  list-style: none;
  margin: 0;
  padding: 0;
  text-align: center;
  color: var(--color-text, #18181B);
  font-weight: 500;
}
[data-fui-comp="ui-fileupload"] .ui-fileupload__filename .ui-fileupload__list li {
  padding: 1px 0;
}
[data-fui-comp="ui-fileupload"] .ui-fileupload__filename .ui-fileupload__thumb {
  inline-size: 96px;
  block-size: 96px;
  object-fit: cover;
  border-radius: var(--radii-md, 8px);
  border: 1px solid var(--color-border, #E4E4E7);
  background: var(--color-surface-soft, #F4F4F5);
}
[data-fui-comp="ui-fileupload"] .ui-fileupload__help,
[data-fui-comp="ui-fileupload"] .ui-fileupload__error {
  margin: 0;
  font-size: 0.85rem;
}
[data-fui-comp="ui-fileupload"] .ui-fileupload__help  { color: var(--color-text-muted, #52525B); }
[data-fui-comp="ui-fileupload"] .ui-fileupload__error { color: var(--color-danger, #DC2626); }`
}

func stickyCSS(_ style.Theme) string {
	return `[data-fui-comp="ui-sticky"] {
  position: -webkit-sticky;
  position: sticky;
  z-index: var(--z-index-sticky, 100);
}
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
[data-fui-comp="ui-sticky"].ui-sticky--top { top: 0; }
[data-fui-comp="ui-sticky"].ui-sticky--bottom { bottom: 0; }
[data-fui-comp="ui-sticky"].ui-sticky--offset-sm { top: var(--spacing-sm, 0.5rem); }
[data-fui-comp="ui-sticky"].ui-sticky--offset-md { top: var(--spacing-md, 1rem); }
[data-fui-comp="ui-sticky"].ui-sticky--offset-lg { top: var(--spacing-lg, 1.5rem); }
[data-fui-comp="ui-sticky"].ui-sticky--offset-xl { top: var(--spacing-xl, 2rem); }
/* Show a subtle bottom border when the element is stuck (only top-sticky) */
@supports ((position: -webkit-sticky) or (position: sticky)) {
  [data-fui-comp="ui-sticky"].ui-sticky--top:not(:is(:first-child))::after {
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
[data-fui-comp="ui-aspect-ratio"].ui-ar--1-1  { aspect-ratio: 1 / 1; }
[data-fui-comp="ui-aspect-ratio"].ui-ar--4-3  { aspect-ratio: 4 / 3; }
[data-fui-comp="ui-aspect-ratio"].ui-ar--16-9 { aspect-ratio: 16 / 9; }
[data-fui-comp="ui-aspect-ratio"].ui-ar--21-9 { aspect-ratio: 21 / 9; }
[data-fui-comp="ui-aspect-ratio"].ui-ar--3-4  { aspect-ratio: 3 / 4; }
[data-fui-comp="ui-aspect-ratio"].ui-ar--3-2  { aspect-ratio: 3 / 2; }
[data-fui-comp="ui-aspect-ratio"].ui-ar--2-3  { aspect-ratio: 2 / 3; }
/* auto: no aspect-ratio, child sizes naturally */
[data-fui-comp="ui-aspect-ratio"].ui-ar--auto > * {
  position: static;
  width: auto;
  height: auto;
}`
}
