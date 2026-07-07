package tree

import "github.com/DonaldMurillo/gofastr/core-ui/style"

// styleFn returns the stylesheet rules for the tree pattern.
// All selectors are scoped under [data-fui-comp="tree"]. Apps
// override via theme tokens (--color-*, --spacing-*, --radii-*,
// --duration-*).
func styleFn(_ style.Theme) string {
	return `[data-fui-comp="tree"], [data-fui-comp="tree"] .tree__group {
  list-style: none;
  margin: 0;
  padding: 0;
}
[data-fui-comp="tree"] .tree__group { padding-inline-start: var(--spacing-lg, 16px); }
[data-fui-comp="tree"] .tree__group[hidden] { display: none; }
[data-fui-comp="tree"] .tree__item { display: block; }
[data-fui-comp="tree"] .tree__row {
  display: flex;
  align-items: center;
  gap: var(--spacing-xs, 4px);
  min-height: var(--spacing-touch-target, 44px);
  padding: var(--spacing-sm, 4px) 6px;
  border-radius: var(--radii-sm, 4px);
}
/* Focus ring only while focus is actually inside the row — the roving
   tabindex means one item ALWAYS carries tabindex="0", so keying the
   outline on the bare attribute painted a permanent ring on it. */
[data-fui-comp="tree"] .tree__item:focus-visible > .tree__row,
[data-fui-comp="tree"] .tree__item:focus-within > .tree__row {
  outline: 2px solid var(--color-primary, #4F46E5);
  outline-offset: -2px;
}
[data-fui-comp="tree"] .tree__item[aria-selected="true"] > .tree__row {
  background: var(--color-muted, #f1f1f3);
  font-weight: 600;
}
[data-fui-comp="tree"] .tree__toggle {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  inline-size: 1.5em;
  block-size: 1.5em;
  border: none;
  background: transparent;
  color: var(--color-text-muted, #6b7280);
  font: inherit;
  font-size: var(--text-xs, 0.75rem);
  cursor: pointer;
  transition: transform var(--duration-fast, 120ms) var(--easing-standard, ease);
}
[data-fui-comp="tree"] .tree__item[aria-expanded="true"] > .tree__row > .tree__toggle {
  transform: rotate(90deg);
}
[data-fui-comp="tree"] .tree__label {
  color: var(--color-text, #111);
  text-decoration: none;
  flex: 1 1 auto;
  min-inline-size: 0;
  /* The label is a flex item (blockified), so axe's target-size rule
     measures it — unlike inline links, which the rule skips. Give it the
     WCAG 2.2 24px floor; it stays inside the 44px row's content box so the
     row height is unchanged and the text-overflow ellipsis still works. */
  min-block-size: var(--spacing-xl, 24px);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
[data-fui-comp="tree"] a.tree__label:hover { text-decoration: underline; }
`
}
