package infinitescroll

import "github.com/DonaldMurillo/gofastr/core-ui/style"

// styleFn is the StyleFn passed to registry.RegisterStyle. The scoped
// selectors below (all under [data-fui-comp="infinitescroll"]) keep
// the rules from leaking into surrounding pages, and let apps
// override the visual defaults via theme tokens (--color-primary,
// --color-border, --duration-medium, etc) without forking the
// component.
//
// The theme parameter is reserved for future use — currently every
// value comes from CSS custom properties so a section-level theme
// override (style.RegisterThemeOverride) reskins this component for
// free.
func styleFn(_ style.Theme) string {
	return `[data-fui-comp="infinitescroll"] {
  display: block;
  position: relative;
}
[data-fui-comp="infinitescroll"][aria-busy="true"]::after {
  content: "";
  display: block;
  block-size: 2px;
  inline-size: 40%;
  margin: var(--spacing-md, 12px) auto;
  background: var(--color-primary, #4F46E5);
  border-radius: 2px;
  animation: infinitescroll-pulse 1.2s ease-in-out infinite;
}
[data-fui-comp="infinitescroll"] .infinitescroll__items {
  display: block;
}
[data-fui-comp="infinitescroll"] .infinitescroll__sentinel {
  block-size: 1px;
  inline-size: 100%;
}
[data-fui-comp="infinitescroll"] .infinitescroll__noscript {
  display: flex;
  justify-content: center;
  padding: var(--spacing-md, 12px) 0;
}
[data-fui-comp="infinitescroll"] .infinitescroll__loadmore {
  min-height: var(--spacing-touch-target, 44px);
  padding: 0 var(--spacing-lg, 16px);
  border: 1px solid var(--color-border, #d0d0d8);
  border-radius: var(--radii-md, 6px);
  background: var(--color-surface, #fff);
  color: var(--color-text, #111);
  font: inherit;
  font-size: var(--text-sm, 0.9rem);
  cursor: pointer;
}
@keyframes infinitescroll-pulse {
  0%, 100% { opacity: 0.3; }
  50% { opacity: 1; }
}
@media (prefers-reduced-motion: reduce) {
  [data-fui-comp="infinitescroll"][aria-busy="true"]::after { animation: none; }
}
`
}
