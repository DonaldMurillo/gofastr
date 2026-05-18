package ui

import (
	"github.com/DonaldMurillo/gofastr/core-ui/registry"
	"github.com/DonaldMurillo/gofastr/core-ui/style"
)

// Per-component CSS handles. Each function returns a self-contained
// stylesheet whose top-level selectors are already scoped to
// [data-fui-comp="<name>"]. The framework registers each handle at
// package init; rendering through Style.WrapHTML emits the marker on
// the outermost tag, and the runtime auto-loads the sheet on first
// appearance.
//
// LoadAuto is the default — load when the marker first hits the DOM.
// PageHeader uses LoadAlways (separate file) because it's on every
// screen. Anything that would benefit from idle prefetch should use
// registry.WithLoad(registry.LoadPrewarm).

var (
	sectionStyle      = registry.RegisterStyle("ui-section", sectionCSS)
	formFieldStyle    = registry.RegisterStyle("ui-form-field", formFieldCSS)
	formSectionStyle  = registry.RegisterStyle("ui-form-section", formSectionCSS)
	buttonStyle       = registry.RegisterStyle("ui-button", buttonCSS, registry.WithLoad(registry.LoadAlways))
	statusBadgeStyle  = registry.RegisterStyle("ui-badge", statusBadgeCSS)
	emptyStateStyle   = registry.RegisterStyle("ui-empty-state", emptyStateCSS)
	calloutStyle      = registry.RegisterStyle("ui-callout", calloutCSS)
	statCardStyle     = registry.RegisterStyle("ui-stat-card", statCardCSS)
	avatarStyle       = registry.RegisterStyle("ui-avatar", avatarCSS)
	formStyle         = registry.RegisterStyle("ui-form", formCSS)
	notificationStyle = registry.RegisterStyle("ui-notification", notificationCSS)
	_                 = registry.RegisterStyle("ui-toast-stack", toastStackCSS)
	dataTableStyle    = registry.RegisterStyle("ui-data-table", dataTableCSS)
	codeBlockStyle    = registry.RegisterStyle("ui-code-block", codeBlockCSS)
)

// buttonCSS is the base .ui-button styling that several call sites
// (including html.Button users with class="ui-button") expect. It is
// LoadAlways because buttons are everywhere — paying the eager link
// avoids the "looks like a native browser button on first paint"
// failure mode.
//
// .ui-button is class-based, not pure scope-based, because it's
// applied to <button> tags rendered through core-ui/html (which
// doesn't go through Style.WrapHTML). The scope still applies to
// any element with both data-fui-comp="ui-button" AND class="ui-
// button" — and via the html selector under the scope we cover the
// plain class usage too.
func buttonCSS(_ style.Theme) string {
	return `[data-fui-comp="ui-button"], .ui-button {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  gap: var(--spacing-sm);
  /* Token-scaled touch target: --spacing-touch-target defaults to
     44px (WCAG 2.5.5 floor). Apps that want a larger tap zone for
     accessibility-mode skins can bump it via theme.Layout.
     TouchTarget without forking the component. */
  min-height: var(--spacing-touch-target);
  padding: 10px var(--spacing-lg);
  border: 1px solid transparent;
  border-radius: var(--radii-md);
  font: inherit;
  font-size: 0.95rem;
  font-weight: 600;
  cursor: pointer;
  background: var(--color-primary);
  color: var(--color-primary-fg);
  text-decoration: none;
  transition: filter 150ms ease, opacity 150ms ease;
}
[data-fui-comp="ui-button"]:hover, .ui-button:hover { filter: brightness(0.95); }
/* Layered focus ring: inner halo in the surface color creates a
   visible gap between the button and the outer primary ring, so
   keyboard focus stays visible regardless of the button's own
   background color. */
[data-fui-comp="ui-button"]:focus-visible, .ui-button:focus-visible {
  outline: none;
  box-shadow: 0 0 0 2px var(--color-surface), 0 0 0 4px var(--color-primary);
}
[data-fui-comp="ui-button"]:disabled, .ui-button:disabled,
[data-fui-comp="ui-button"][aria-disabled="true"], .ui-button[aria-disabled="true"] {
  cursor: not-allowed;
  opacity: 0.6;
  filter: none;
}

/* Variants — Primary is the default style above. */
.ui-button--secondary {
  background: var(--color-surface);
  color: var(--color-text);
  border-color: var(--color-border);
}
.ui-button--secondary:hover { filter: none; background: var(--color-surface-soft); }
/* secondary's bg IS --color-surface, so the layered-shadow inner halo
   would be invisible. Use a plain outline with offset instead — it
   contrasts against any page background. */
.ui-button--secondary:focus-visible {
  box-shadow: none;
  outline: 2px solid var(--color-text);
  outline-offset: 2px;
}

.ui-button--danger {
  background: var(--color-danger);
  color: #FFFFFF;
}
.ui-button--danger:focus-visible {
  box-shadow: 0 0 0 2px var(--color-surface), 0 0 0 4px var(--color-danger);
}

.ui-button--ghost {
  background: transparent;
  color: var(--color-text);
  border-color: transparent;
}
.ui-button--ghost:hover { filter: none; background: var(--color-surface-soft); }
/* ghost sits on --color-background; a halo of --color-surface collapses
   to near-invisible. Plain outline + offset is reliable on any bg. */
.ui-button--ghost:focus-visible {
  box-shadow: none;
  outline: 2px solid var(--color-text);
  outline-offset: 2px;
}`
}

func codeBlockCSS(_ style.Theme) string {
	// Uses the dedicated code-surface tokens (defined in style.Theme
	// defaults) so dark mode can override them independently of the
	// page Text/Background pair. Light-mode fallback values keep the
	// classic "dark inkwell" feel; dark-mode apps redefine the tokens
	// in their app stylesheet under [data-color-scheme="dark"].
	return `[data-fui-comp="ui-code-block"] {
  display: block;
  overflow-x: auto;
  margin: 0;
  padding: var(--spacing-lg, 16px);
  background: var(--color-code-surface, #18181B);
  color: var(--color-code-text, #E4E4E7);
  border: 1px solid var(--color-code-border, #27272A);
  border-radius: var(--radii-md, 8px);
  font-family: var(--font-mono, ui-monospace, monospace);
  font-size: 0.85rem;
  line-height: 1.6;
  white-space: pre;
  -webkit-text-size-adjust: 100%;
}
[data-fui-comp="ui-code-block"] .tok-kw     { color: #C792EA; }
[data-fui-comp="ui-code-block"] .tok-fn     { color: #82AAFF; }
[data-fui-comp="ui-code-block"] .tok-str    { color: #C3E88D; }
[data-fui-comp="ui-code-block"] .tok-num    { color: #F78C6C; }
[data-fui-comp="ui-code-block"] .tok-com    { color: #676E95; font-style: italic; }
[data-fui-comp="ui-code-block"] .tok-name   { color: #FFCB6B; }`
}

func sectionCSS(_ style.Theme) string {
	return `[data-fui-comp="ui-section"] {
  display: grid;
  gap: var(--spacing-md, 8px);
  margin: var(--spacing-xl, 24px) 0;
  border: 0;
}
[data-fui-comp="ui-section"] .ui-section__heading {
  margin: 0;
  font-size: 1.125rem;
  font-weight: 600;
  color: var(--color-text, #18181B);
}
[data-fui-comp="ui-section"] .ui-section__description {
  margin: 0;
  color: var(--color-text-muted, #52525B);
}
[data-fui-comp="ui-section"] .ui-section__body {
  display: grid;
  gap: var(--spacing-md, 8px);
}`
}

func formFieldCSS(_ style.Theme) string {
	return `[data-fui-comp="ui-form-field"] {
  display: grid;
  gap: var(--spacing-xs, 2px);
}
[data-fui-comp="ui-form-field"] .ui-form-field__label {
  font-weight: 500;
  font-size: 0.9rem;
  color: var(--color-text, #18181B);
}
[data-fui-comp="ui-form-field"] .ui-form-field__required {
  color: var(--color-danger, #DC2626);
  margin-inline-start: 2px;
}
[data-fui-comp="ui-form-field"] .ui-form-field__help {
  margin: 0;
  font-size: 0.85rem;
  color: var(--color-text-muted, #52525B);
}
[data-fui-comp="ui-form-field"] .ui-form-field__error {
  margin: 0;
  font-size: 0.85rem;
  color: var(--color-danger, #DC2626);
}
[data-fui-comp="ui-form-field"].is-error input,
[data-fui-comp="ui-form-field"].is-error textarea,
[data-fui-comp="ui-form-field"].is-error select {
  /* Non-color affordance: stack an inset 1px ring so the error state
     reads as a thicker border without bumping border-width and
     shifting the input's internal text by 1px on every validation
     toggle (WCAG 1.4.1 — info conveyed by more than color alone). */
  border-color: var(--color-danger, #DC2626);
  box-shadow: inset 0 0 0 1px var(--color-danger, #DC2626);
}
[data-fui-comp="ui-form-field"] input,
[data-fui-comp="ui-form-field"] textarea,
[data-fui-comp="ui-form-field"] select {
  /* Token-scaled touch target (see ui-button). */
  min-height: var(--spacing-touch-target);
  padding: 10px var(--spacing-md, 8px);
  border: 1px solid var(--color-border, #E4E4E7);
  border-radius: var(--radii-md, 8px);
  background: var(--color-surface, #FFFFFF);
  color: var(--color-text, #18181B);
  font: inherit;
  font-size: 0.95rem;
}
[data-fui-comp="ui-form-field"] input:focus-visible,
[data-fui-comp="ui-form-field"] textarea:focus-visible,
[data-fui-comp="ui-form-field"] select:focus-visible {
  outline: 2px solid var(--color-primary, #4F46E5);
  outline-offset: 1px;
}`
}

func formSectionCSS(_ style.Theme) string {
	return `[data-fui-comp="ui-form-section"] {
  display: grid;
  gap: var(--spacing-lg, 16px);
  padding: var(--spacing-lg, 16px);
  border: 1px solid var(--color-border, #E4E4E7);
  border-radius: var(--radii-md, 8px);
  background: var(--color-surface, #FFFFFF);
}
[data-fui-comp="ui-form-section"] .ui-form-section__heading {
  margin: 0;
  font-size: 1rem;
  font-weight: 600;
  color: var(--color-text, #18181B);
}
[data-fui-comp="ui-form-section"] .ui-form-section__description {
  margin: 0;
  font-size: 0.9rem;
  color: var(--color-text-muted, #52525B);
}
[data-fui-comp="ui-form-section"] .ui-form-section__fields {
  display: grid;
  gap: var(--spacing-md, 8px);
}`
}

func statusBadgeCSS(_ style.Theme) string {
	return `[data-fui-comp="ui-badge"] {
  display: inline-flex;
  align-items: center;
  padding: 2px var(--spacing-md, 8px);
  border-radius: var(--radii-full, 9999px);
  font-size: 0.75rem;
  font-weight: 600;
  letter-spacing: 0.02em;
  border: 1px solid transparent;
}
[data-fui-comp="ui-badge"].ui-badge--success {
  background: color-mix(in oklab, var(--color-success, #16A34A) 15%, var(--color-surface, #fff) 85%);
  color: var(--color-success, #16A34A);
  border-color: color-mix(in oklab, var(--color-success, #16A34A) 30%, var(--color-surface, #fff) 70%);
}
[data-fui-comp="ui-badge"].ui-badge--warning {
  background: color-mix(in oklab, var(--color-warning, #CA8A04) 15%, var(--color-surface, #fff) 85%);
  color: var(--color-warning, #CA8A04);
  border-color: color-mix(in oklab, var(--color-warning, #CA8A04) 30%, var(--color-surface, #fff) 70%);
}
[data-fui-comp="ui-badge"].ui-badge--danger {
  background: color-mix(in oklab, var(--color-danger, #DC2626) 15%, var(--color-surface, #fff) 85%);
  color: var(--color-danger, #DC2626);
  border-color: color-mix(in oklab, var(--color-danger, #DC2626) 30%, var(--color-surface, #fff) 70%);
}
[data-fui-comp="ui-badge"].ui-badge--info {
  background: color-mix(in oklab, var(--color-info, #2563EB) 15%, var(--color-surface, #fff) 85%);
  color: var(--color-info, #2563EB);
  border-color: color-mix(in oklab, var(--color-info, #2563EB) 30%, var(--color-surface, #fff) 70%);
}
[data-fui-comp="ui-badge"].ui-badge--neutral {
  background: var(--color-surface-soft, #F4F4F5);
  color: var(--color-text-muted, #52525B);
  border-color: var(--color-border, #E4E4E7);
}`
}

func emptyStateCSS(_ style.Theme) string {
	return `[data-fui-comp="ui-empty-state"] {
  display: grid;
  gap: var(--spacing-md, 8px);
  justify-items: center;
  text-align: center;
  padding: var(--spacing-3xl, 48px) var(--spacing-lg, 16px);
  background: var(--color-surface-soft, #F4F4F5);
  border: 1px dashed var(--color-border, #E4E4E7);
  border-radius: var(--radii-lg, 12px);
}
[data-fui-comp="ui-empty-state"] .ui-empty-state__title {
  margin: 0;
  font-size: 1.05rem;
  font-weight: 600;
  color: var(--color-text, #18181B);
}
[data-fui-comp="ui-empty-state"] .ui-empty-state__description {
  margin: 0;
  color: var(--color-text-muted, #52525B);
  max-inline-size: 36ch;
}
[data-fui-comp="ui-empty-state"] .ui-empty-state__action { margin-top: var(--spacing-sm, 4px); }`
}

func calloutCSS(_ style.Theme) string {
	return `[data-fui-comp="ui-callout"] {
  display: grid;
  gap: var(--spacing-xs, 2px);
  padding: var(--spacing-md, 8px) var(--spacing-lg, 16px);
  border: 1px solid var(--color-border, #E4E4E7);
  border-inline-start-width: 4px;
  border-radius: var(--radii-md, 8px);
  background: var(--color-surface, #FFFFFF);
}
[data-fui-comp="ui-callout"] .ui-callout__title {
  font-size: 0.9rem;
  font-weight: 700;
  color: var(--color-text, #18181B);
}
[data-fui-comp="ui-callout"] .ui-callout__body {
  font-size: 0.9rem;
  color: var(--color-text-muted, #52525B);
}
[data-fui-comp="ui-callout"].ui-callout--info    { border-inline-start-color: var(--color-info, #2563EB); }
[data-fui-comp="ui-callout"].ui-callout--success { border-inline-start-color: var(--color-success, #16A34A); }
[data-fui-comp="ui-callout"].ui-callout--warning { border-inline-start-color: var(--color-warning, #CA8A04); }
[data-fui-comp="ui-callout"].ui-callout--danger  { border-inline-start-color: var(--color-danger, #DC2626); }
[data-fui-comp="ui-callout"].ui-callout--neutral { border-inline-start-color: var(--color-border-strong, #A1A1AA); }`
}

func statCardCSS(_ style.Theme) string {
	return `[data-fui-comp="ui-stat-card"] {
  display: grid;
  gap: var(--spacing-xs, 2px);
  padding: var(--spacing-lg, 16px);
  background: var(--color-surface, #FFFFFF);
  border: 1px solid var(--color-border, #E4E4E7);
  border-radius: var(--radii-md, 8px);
}
[data-fui-comp="ui-stat-card"] .ui-stat-card__label {
  margin: 0;
  font-size: 0.8rem;
  font-weight: 600;
  text-transform: uppercase;
  letter-spacing: 0.06em;
  color: var(--color-text-muted, #52525B);
}
[data-fui-comp="ui-stat-card"] .ui-stat-card__value {
  margin: 0;
  font-size: 1.75rem;
  font-weight: 700;
  line-height: 1;
  color: var(--color-text, #18181B);
}
[data-fui-comp="ui-stat-card"] .ui-stat-card__trend {
  margin: 0;
  font-size: 0.85rem;
  font-weight: 600;
}
[data-fui-comp="ui-stat-card"] .ui-stat-card__trend--up   { color: var(--color-success, #16A34A); }
[data-fui-comp="ui-stat-card"] .ui-stat-card__trend--down { color: var(--color-danger, #DC2626); }
[data-fui-comp="ui-stat-card"] .ui-stat-card__trend--flat { color: var(--color-text-muted, #52525B); }`
}

func avatarCSS(_ style.Theme) string {
	return `[data-fui-comp="ui-avatar"] {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  border-radius: var(--radii-full, 9999px);
  background: var(--color-surface-soft, #F4F4F5);
  color: var(--color-text-muted, #52525B);
  font-weight: 600;
  font-size: 0.8rem;
  overflow: hidden;
  flex-shrink: 0;
  inline-size: 2.5rem;
  block-size:  2.5rem;
}
[data-fui-comp="ui-avatar"].ui-avatar--sm { inline-size: 1.5rem; block-size: 1.5rem; font-size: 0.7rem; }
[data-fui-comp="ui-avatar"].ui-avatar--lg { inline-size: 3rem;   block-size: 3rem;   font-size: 0.95rem; }
[data-fui-comp="ui-avatar"].ui-avatar--xl { inline-size: 4rem;   block-size: 4rem;   font-size: 1.1rem; }
[data-fui-comp="ui-avatar"] .ui-avatar__img {
  width: 100%;
  height: 100%;
  object-fit: cover;
}
[data-fui-comp="ui-avatar"] .ui-avatar__initials {
  letter-spacing: 0.04em;
}`
}

func formCSS(_ style.Theme) string {
	return `[data-fui-comp="ui-form"] { display: grid; gap: var(--spacing-lg, 16px); }
[data-fui-comp="ui-form"] .ui-form__fields { display: grid; gap: var(--spacing-md, 8px); }
[data-fui-comp="ui-form"] .ui-form__actions {
  display: flex;
  justify-content: flex-end;
  gap: var(--spacing-sm, 4px);
}`
}

func notificationCSS(_ style.Theme) string {
	return `[data-fui-comp="ui-notification"] {
  display: grid;
  grid-template-columns: auto 1fr auto;
  align-items: start;
  gap: var(--spacing-md, 8px);
  padding: var(--spacing-md, 8px) var(--spacing-lg, 16px);
  border-radius: var(--radii-md, 8px);
  background: var(--color-surface, #FFFFFF);
  border: 1px solid var(--color-border, #E4E4E7);
  border-inline-start: 4px solid var(--color-info, #2563EB);
  box-shadow: 0 4px 12px rgba(0,0,0,0.06);
  max-inline-size: 28rem;
}
[data-fui-comp="ui-notification"] .ui-notification__icon {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  inline-size: 1.5rem;
  block-size: 1.5rem;
  border-radius: var(--radii-full, 9999px);
  color: var(--color-primary-fg, #FFFFFF);
  background: var(--color-info, #2563EB);
  font-weight: 700;
  font-size: 0.85rem;
  line-height: 1;
}
[data-fui-comp="ui-notification"] .ui-notification__text { display: grid; gap: var(--spacing-xs, 2px); }
[data-fui-comp="ui-notification"] .ui-notification__title {
  font-size: 0.95rem;
  font-weight: 700;
  color: var(--color-text, #18181B);
}
[data-fui-comp="ui-notification"] .ui-notification__body {
  margin: 0;
  font-size: 0.9rem;
  color: var(--color-text-muted, #52525B);
}
[data-fui-comp="ui-notification"] .ui-notification__dismiss {
  align-self: start;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  /* WCAG 2.5.5 — 44×44 tap target. The 24×24 in chaos sweep was
     <30% of WCAG's 1936px² floor. */
  min-inline-size: var(--spacing-touch-target);
  min-block-size: var(--spacing-touch-target);
  border-radius: var(--radii-full, 9999px);
  font-size: 1.1rem;
  line-height: 1;
  color: var(--color-text-muted, #52525B);
  text-decoration: none;
}
[data-fui-comp="ui-notification"] .ui-notification__dismiss:hover {
  background: var(--color-surface-soft, #F4F4F5);
  color: var(--color-text, #18181B);
  text-decoration: none;
}
[data-fui-comp="ui-notification"].ui-notification--success { border-inline-start-color: var(--color-success, #16A34A); }
[data-fui-comp="ui-notification"].ui-notification--success .ui-notification__icon { background: var(--color-success, #16A34A); }
[data-fui-comp="ui-notification"].ui-notification--warning { border-inline-start-color: var(--color-warning, #CA8A04); }
[data-fui-comp="ui-notification"].ui-notification--warning .ui-notification__icon { background: var(--color-warning, #CA8A04); }
[data-fui-comp="ui-notification"].ui-notification--danger  { border-inline-start-color: var(--color-danger, #DC2626); }
[data-fui-comp="ui-notification"].ui-notification--danger  .ui-notification__icon { background: var(--color-danger, #DC2626); }
[data-fui-comp="ui-notification"].ui-notification--info    { border-inline-start-color: var(--color-info, #2563EB); }
[data-fui-comp="ui-notification"].ui-notification--info    .ui-notification__icon { background: var(--color-info, #2563EB); }
[data-fui-comp="ui-notification"].ui-notification--neutral { border-inline-start-color: var(--color-border-strong, #A1A1AA); }
[data-fui-comp="ui-notification"].ui-notification--neutral .ui-notification__icon {
  background: var(--color-surface-soft, #F4F4F5);
  color: var(--color-text-muted, #52525B);
}
[data-fui-comp="ui-notification"].ui-notification--floating {
  position: fixed;
  z-index: 1000;
  box-shadow: 0 12px 32px rgba(0, 0, 0, 0.18);
  animation: ui-notification-slide-in 220ms ease-out;
}
[data-fui-comp="ui-notification"].ui-notification--at-top-right    { top: 1rem; right: 1rem; }
[data-fui-comp="ui-notification"].ui-notification--at-top-left     { top: 1rem; left: 1rem; }
[data-fui-comp="ui-notification"].ui-notification--at-bottom-right { bottom: 1rem; right: 1rem; }
[data-fui-comp="ui-notification"].ui-notification--at-bottom-left  { bottom: 1rem; left: 1rem; }
@keyframes ui-notification-slide-in {
  from { opacity: 0; transform: translateY(-12px); }
  to   { opacity: 1; transform: translateY(0); }
}
[data-fui-comp="ui-notification"].ui-notification--at-bottom-right,
[data-fui-comp="ui-notification"].ui-notification--at-bottom-left {
  animation-name: ui-notification-slide-in-up;
}
@keyframes ui-notification-slide-in-up {
  from { opacity: 0; transform: translateY(12px); }
  to   { opacity: 1; transform: translateY(0); }
}
@media (prefers-reduced-motion: reduce) {
  [data-fui-comp="ui-notification"].ui-notification--floating { animation: none; }
}`
}

// toastStackCSS styles the vertical stack of toast items rendered by
// preset.ToastStack. The stack itself is `display: grid` so successive
// items reflow with a height transition; each item slides + fades in.
// All animation values come from theme tokens so a single theme tweak
// retunes every toast at once.
func toastStackCSS(_ style.Theme) string {
	return `[data-fui-comp="ui-toast-stack"] {
  display: grid;
  gap: var(--spacing-md, 8px);
  pointer-events: none;
  max-width: min(360px, calc(100vw - 2rem));
}
[data-fui-comp="ui-toast-stack"] .ui-toast-stack__item {
  pointer-events: auto;
  animation: ui-toast-stack-in var(--duration-toast-enter, 220ms)
    var(--easing-ease-out, cubic-bezier(0.16, 1, 0.3, 1));
  will-change: transform, opacity;
}
[data-fui-comp="ui-toast-stack"] .ui-toast-stack__item.is-leaving {
  animation: ui-toast-stack-out var(--duration-toast-exit, 180ms)
    var(--easing-ease-in, cubic-bezier(0.4, 0, 1, 1)) forwards;
}
@keyframes ui-toast-stack-in {
  from { opacity: 0; transform: translateY(-8px) scale(0.98); }
  to   { opacity: 1; transform: translateY(0)    scale(1);    }
}
@keyframes ui-toast-stack-out {
  from { opacity: 1; transform: translateY(0)   scale(1);    }
  to   { opacity: 0; transform: translateY(-6px) scale(0.98); }
}
@media (prefers-reduced-motion: reduce) {
  [data-fui-comp="ui-toast-stack"] .ui-toast-stack__item,
  [data-fui-comp="ui-toast-stack"] .ui-toast-stack__item.is-leaving {
    animation: none;
  }
}`
}

func dataTableCSS(_ style.Theme) string {
	return `[data-fui-comp="ui-data-table"] { display: grid; gap: var(--spacing-md, 8px); }
[data-fui-comp="ui-data-table"] .ui-data-table__scroll {
  overflow-x: auto;
  border: 1px solid var(--color-border, #E4E4E7);
  border-radius: var(--radii-md, 8px);
  background: var(--color-surface, #FFFFFF);
}
[data-fui-comp="ui-data-table"] .ui-data-table__table {
  width: 100%;
  border-collapse: collapse;
  font-size: 0.95rem;
}
[data-fui-comp="ui-data-table"] .ui-data-table__caption {
  text-align: start;
  padding: var(--spacing-sm, 4px) var(--spacing-lg, 16px);
  font-size: 0.8rem;
  font-weight: 600;
  text-transform: uppercase;
  letter-spacing: 0.06em;
  color: var(--color-text-muted, #52525B);
  background: var(--color-surface-soft, #F4F4F5);
  border-bottom: 1px solid var(--color-border, #E4E4E7);
  caption-side: top;
}
[data-fui-comp="ui-data-table"] .ui-data-table__table th,
[data-fui-comp="ui-data-table"] .ui-data-table__table td {
  padding: var(--spacing-sm, 4px) var(--spacing-lg, 16px);
  text-align: start;
  vertical-align: middle;
  border-bottom: 1px solid var(--color-border, #E4E4E7);
}
[data-fui-comp="ui-data-table"] .ui-data-table__table tbody tr:last-child td {
  border-bottom: 0;
}
[data-fui-comp="ui-data-table"] .ui-data-table__table th {
  font-weight: 600;
  color: var(--color-text-muted, #52525B);
  background: var(--color-surface-soft, #F4F4F5);
  font-size: 0.8rem;
  text-transform: uppercase;
  letter-spacing: 0.04em;
}
[data-fui-comp="ui-data-table"] .ui-data-table__table tbody tr:hover {
  background: var(--color-surface-soft, #F4F4F5);
}
[data-fui-comp="ui-data-table"] .ui-data-table__table .is-align-end   { text-align: end; }
[data-fui-comp="ui-data-table"] .ui-data-table__table .is-align-center { text-align: center; }
[data-fui-comp="ui-data-table"] .ui-data-table__sort,
[data-fui-comp="ui-data-table"] button.ui-data-table__sort {
  display: inline-flex;
  align-items: center;
  /* Token-scaled tap target. Sort headers are the most-tapped
     element in a data table on mobile. Both axes — short column
     labels like "Email" (38px wide) failed the 44px width floor. */
  min-block-size: var(--spacing-touch-target);
  min-inline-size: var(--spacing-touch-target);
  gap: 0.25rem;
  background: transparent;
  border: 0;
  padding: 0 0.25rem;
  color: inherit;
  font: inherit;
  text-decoration: none;
  cursor: pointer;
}
[data-fui-comp="ui-data-table"] .ui-data-table__sort:hover {
  color: var(--color-text, #18181B);
  text-decoration: none;
}
[data-fui-comp="ui-data-table"] .ui-data-table__sort-indicator {
  font-size: 0.7em;
  color: var(--color-primary, #4F46E5);
}
[data-fui-comp="ui-data-table"] .ui-data-table__table th[aria-sort="ascending"],
[data-fui-comp="ui-data-table"] .ui-data-table__table th[aria-sort="descending"] {
  color: var(--color-primary, #4F46E5);
}
[data-fui-comp="ui-data-table"] .ui-data-table__footer {
  display: flex;
  justify-content: flex-end;
}`
}
