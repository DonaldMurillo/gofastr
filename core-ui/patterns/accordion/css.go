package accordion

// BaseCSS returns the stylesheet rules required for the accordion
// components, using only modern CSS features and CSS custom properties
// from the host theme.
//
// Tokens consumed (with sensible fallbacks):
//
//	--color-border, --color-surface, --color-text, --color-primary
//	--spacing-md, --spacing-lg
//	--radii-md
//
// Animation feature usage:
//
//   - interpolate-size: allow-keywords  → enables transitions to/from
//     `auto` height on <details>, so the content height animates.
//   - ::details-content                  → the polyfilled-into-the-spec
//     pseudo-element representing the slot the browser hides/reveals.
//   - transition-behavior: allow-discrete → keeps `content-visibility`
//     and `display` toggles in sync with the height transition.
//
// Browsers lacking these features fall back to instant open/close, which
// is intentional progressive enhancement.
func BaseCSS() string {
	return `
.accordion {
  display: grid;
  gap: var(--spacing-sm, 4px);
}

.accordion-item {
  border: 1px solid var(--color-border, #E5E7EB);
  border-radius: var(--radii-md, 8px);
  background: var(--color-surface, #FFFFFF);
  overflow: clip;
  interpolate-size: allow-keywords;
}

.accordion-item + .accordion-item {
  margin-top: 0;
}

.accordion-summary {
  list-style: none;
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: var(--spacing-md, 8px);
  padding: var(--spacing-md, 8px) var(--spacing-lg, 16px);
  cursor: pointer;
  font-weight: 600;
  color: var(--color-text, #1F2937);
  user-select: none;
}

.accordion-summary::-webkit-details-marker {
  display: none;
}

.accordion-summary:hover {
  background: color-mix(in oklab, var(--color-surface, #FFFFFF) 92%, var(--color-primary, #4F46E5) 8%);
}

.accordion-summary:focus-visible {
  outline: 2px solid var(--color-primary, #4F46E5);
  outline-offset: -2px;
}

.accordion-marker {
  width: 0.65rem;
  height: 0.65rem;
  border-right: 2px solid currentColor;
  border-bottom: 2px solid currentColor;
  transform: rotate(-45deg);
  transition: transform 200ms ease;
  flex-shrink: 0;
}

.accordion-item[open] > .accordion-summary .accordion-marker {
  transform: rotate(45deg);
}

.accordion-content {
  padding: 0 var(--spacing-lg, 16px) var(--spacing-md, 8px);
  color: var(--color-text, #1F2937);
}

.accordion-item::details-content {
  block-size: 0;
  overflow: clip;
  transition: block-size 220ms ease, content-visibility 220ms ease allow-discrete;
  transition-behavior: allow-discrete;
}

.accordion-item[open]::details-content {
  block-size: auto;
}

@media (prefers-reduced-motion: reduce) {
  .accordion-marker,
  .accordion-item::details-content {
    transition: none;
  }
}
`
}
