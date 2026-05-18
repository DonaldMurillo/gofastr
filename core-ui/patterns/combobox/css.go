package combobox

import "github.com/DonaldMurillo/gofastr/core-ui/style"

// styleFn returns the stylesheet rules for the combobox. All
// selectors are scoped under [data-fui-comp="combobox"]. Apps
// customise via theme tokens (--color-*, --spacing-*, --radii-*).
func styleFn(_ style.Theme) string {
	return `[data-fui-comp="combobox"] {
  position: relative;
  display: block;
  inline-size: 100%;
  max-inline-size: 24rem;
}
[data-fui-comp="combobox"] .combobox__label {
  display: block;
  margin-block-end: var(--spacing-xs, 4px);
  font-size: 0.85rem;
  color: var(--color-text-muted, #4b5563);
}
[data-fui-comp="combobox"] .combobox__form {
  display: block;
  margin: 0;
}
[data-fui-comp="combobox"] .combobox__input {
  inline-size: 100%;
  min-height: var(--spacing-touch-target, 44px);
  padding: 0 var(--spacing-md, 12px);
  border: 1px solid var(--color-border, #d0d0d8);
  border-radius: var(--radii-md, 6px);
  background: var(--color-surface, #fff);
  color: var(--color-text, #111);
  font: inherit;
  font-size: 0.95rem;
  box-sizing: border-box;
}
[data-fui-comp="combobox"] .combobox__input:focus-visible {
  outline: none;
  border-color: var(--color-primary, #4F46E5);
  box-shadow: 0 0 0 3px rgba(79, 70, 229, 0.18);
}
[data-fui-comp="combobox"] .combobox__listbox {
  position: absolute;
  inset-inline-start: 0;
  inset-inline-end: 0;
  margin: 4px 0 0 0;
  padding: 4px 0;
  list-style: none;
  background: var(--color-surface, #fff);
  border: 1px solid var(--color-border, #d0d0d8);
  border-radius: var(--radii-md, 6px);
  box-shadow: 0 8px 24px rgba(0,0,0,0.12);
  max-block-size: 18rem;
  overflow-y: auto;
  z-index: 50;
}
[data-fui-comp="combobox"] .combobox__listbox[hidden] { display: none; }
[data-fui-comp="combobox"] .combobox__listbox [role="option"] {
  display: block;
  padding: var(--spacing-sm, 8px) var(--spacing-md, 12px);
  color: var(--color-text, #111);
  cursor: pointer;
  user-select: none;
}
[data-fui-comp="combobox"] .combobox__listbox [role="option"].is-active {
  background: var(--color-muted, #f1f1f3);
}
[data-fui-comp="combobox"] .combobox__listbox [role="option"][aria-disabled="true"] {
  color: var(--color-text-muted, #6b7280);
  cursor: default;
}
@media (pointer: coarse) {
  [data-fui-comp="combobox"] .combobox__listbox [role="option"] {
    min-height: var(--spacing-touch-target, 44px);
    display: flex;
    align-items: center;
  }
}
`
}
