package ui

// BaseCSS returns the stylesheet for every framework/ui component.
//
// Tokens consumed (resolved from framework/ui/theme via :root custom
// properties): --color-{background,surface,surface-soft,border,
// border-strong,text,text-muted,text-subtle,primary,primary-fg,accent,
// success,warning,danger,info}, --spacing-{xs,sm,md,lg,xl,2xl,3xl},
// --radii-{sm,md,lg,full}, --font-{body,heading,mono}.
func BaseCSS() string {
	return `
/* ─── PageHeader ─── */
.ui-page-header {
  display: flex;
  flex-wrap: wrap;
  align-items: flex-start;
  justify-content: space-between;
  gap: var(--spacing-lg, 16px);
  padding: var(--spacing-xl, 24px) 0 var(--spacing-lg, 16px);
  border-bottom: 1px solid var(--color-border, #E4E4E7);
}
.ui-page-header__text { display: grid; gap: var(--spacing-xs, 2px); }
.ui-page-header__eyebrow {
  margin: 0;
  font-size: 0.75rem;
  font-weight: 600;
  text-transform: uppercase;
  letter-spacing: 0.06em;
  color: var(--color-text-subtle, #A1A1AA);
}
.ui-page-header__title {
  margin: 0;
  font-size: 1.5rem;
  font-weight: 700;
  line-height: 1.25;
  color: var(--color-text, #18181B);
}
.ui-page-header__subtitle {
  margin: 0;
  color: var(--color-text-muted, #52525B);
}
.ui-page-header__actions {
  display: flex;
  flex-wrap: wrap;
  gap: var(--spacing-sm, 4px);
}

/* ─── Section ─── */
.ui-section {
  display: grid;
  gap: var(--spacing-md, 8px);
  margin: var(--spacing-xl, 24px) 0;
  border: 0;
}
.ui-section__heading {
  margin: 0;
  font-size: 1.125rem;
  font-weight: 600;
  color: var(--color-text, #18181B);
}
.ui-section__description {
  margin: 0;
  color: var(--color-text-muted, #52525B);
}
.ui-section__body {
  display: grid;
  gap: var(--spacing-md, 8px);
}

/* ─── FormField ─── */
.ui-form-field {
  display: grid;
  gap: var(--spacing-xs, 2px);
}
.ui-form-field__label {
  font-weight: 500;
  font-size: 0.9rem;
  color: var(--color-text, #18181B);
}
.ui-form-field__required {
  color: var(--color-danger, #DC2626);
  margin-inline-start: 2px;
}
.ui-form-field__help {
  margin: 0;
  font-size: 0.85rem;
  color: var(--color-text-muted, #52525B);
}
.ui-form-field__error {
  margin: 0;
  font-size: 0.85rem;
  color: var(--color-danger, #DC2626);
}
.ui-form-field.is-error input,
.ui-form-field.is-error textarea,
.ui-form-field.is-error select {
  border-color: var(--color-danger, #DC2626);
}
.ui-form-field input,
.ui-form-field textarea,
.ui-form-field select {
  padding: var(--spacing-sm, 4px) var(--spacing-md, 8px);
  border: 1px solid var(--color-border, #E4E4E7);
  border-radius: var(--radii-md, 8px);
  background: var(--color-surface, #FFFFFF);
  color: var(--color-text, #18181B);
  font: inherit;
  font-size: 0.95rem;
}
.ui-form-field input:focus-visible,
.ui-form-field textarea:focus-visible,
.ui-form-field select:focus-visible {
  outline: 2px solid var(--color-primary, #4F46E5);
  outline-offset: 1px;
}

/* ─── FormSection ─── */
.ui-form-section {
  display: grid;
  gap: var(--spacing-lg, 16px);
  padding: var(--spacing-lg, 16px);
  border: 1px solid var(--color-border, #E4E4E7);
  border-radius: var(--radii-md, 8px);
  background: var(--color-surface, #FFFFFF);
}
.ui-form-section__heading {
  margin: 0;
  font-size: 1rem;
  font-weight: 600;
  color: var(--color-text, #18181B);
}
.ui-form-section__description {
  margin: 0;
  font-size: 0.9rem;
  color: var(--color-text-muted, #52525B);
}
.ui-form-section__fields {
  display: grid;
  gap: var(--spacing-md, 8px);
}

/* ─── Button ─── */
.ui-button {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  gap: var(--spacing-sm, 4px);
  padding: var(--spacing-sm, 4px) var(--spacing-lg, 16px);
  border: 1px solid transparent;
  border-radius: var(--radii-md, 8px);
  font: inherit;
  font-size: 0.95rem;
  font-weight: 600;
  cursor: pointer;
  background: var(--color-primary, #4F46E5);
  color: var(--color-primary-fg, #FFFFFF);
  transition: filter 150ms ease;
}
.ui-button:hover { filter: brightness(0.95); }
.ui-button:focus-visible {
  outline: 2px solid var(--color-primary, #4F46E5);
  outline-offset: 2px;
}
.ui-button--danger {
  background: var(--color-danger, #DC2626);
  color: white;
}
.ui-button--danger:focus-visible {
  outline-color: var(--color-danger, #DC2626);
}

/* ─── StatusBadge ─── */
.ui-badge {
  display: inline-flex;
  align-items: center;
  padding: 2px var(--spacing-md, 8px);
  border-radius: var(--radii-full, 9999px);
  font-size: 0.75rem;
  font-weight: 600;
  letter-spacing: 0.02em;
  border: 1px solid transparent;
}
.ui-badge--success {
  background: color-mix(in oklab, var(--color-success, #16A34A) 15%, var(--color-surface, #fff) 85%);
  color: var(--color-success, #16A34A);
  border-color: color-mix(in oklab, var(--color-success, #16A34A) 30%, var(--color-surface, #fff) 70%);
}
.ui-badge--warning {
  background: color-mix(in oklab, var(--color-warning, #CA8A04) 15%, var(--color-surface, #fff) 85%);
  color: var(--color-warning, #CA8A04);
  border-color: color-mix(in oklab, var(--color-warning, #CA8A04) 30%, var(--color-surface, #fff) 70%);
}
.ui-badge--danger {
  background: color-mix(in oklab, var(--color-danger, #DC2626) 15%, var(--color-surface, #fff) 85%);
  color: var(--color-danger, #DC2626);
  border-color: color-mix(in oklab, var(--color-danger, #DC2626) 30%, var(--color-surface, #fff) 70%);
}
.ui-badge--info {
  background: color-mix(in oklab, var(--color-info, #2563EB) 15%, var(--color-surface, #fff) 85%);
  color: var(--color-info, #2563EB);
  border-color: color-mix(in oklab, var(--color-info, #2563EB) 30%, var(--color-surface, #fff) 70%);
}
.ui-badge--neutral {
  background: var(--color-surface-soft, #F4F4F5);
  color: var(--color-text-muted, #52525B);
  border-color: var(--color-border, #E4E4E7);
}

/* ─── EmptyState ─── */
.ui-empty-state {
  display: grid;
  gap: var(--spacing-md, 8px);
  justify-items: center;
  text-align: center;
  padding: var(--spacing-3xl, 48px) var(--spacing-lg, 16px);
  background: var(--color-surface-soft, #F4F4F5);
  border: 1px dashed var(--color-border, #E4E4E7);
  border-radius: var(--radii-lg, 12px);
}
.ui-empty-state__title {
  margin: 0;
  font-size: 1.05rem;
  font-weight: 600;
  color: var(--color-text, #18181B);
}
.ui-empty-state__description {
  margin: 0;
  color: var(--color-text-muted, #52525B);
  max-inline-size: 36ch;
}
.ui-empty-state__action { margin-top: var(--spacing-sm, 4px); }

/* ─── Callout ─── */
.ui-callout {
  display: grid;
  gap: var(--spacing-xs, 2px);
  padding: var(--spacing-md, 8px) var(--spacing-lg, 16px);
  border: 1px solid var(--color-border, #E4E4E7);
  border-inline-start-width: 4px;
  border-radius: var(--radii-md, 8px);
  background: var(--color-surface, #FFFFFF);
}
.ui-callout__title {
  font-size: 0.9rem;
  font-weight: 700;
  color: var(--color-text, #18181B);
}
.ui-callout__body {
  font-size: 0.9rem;
  color: var(--color-text-muted, #52525B);
}
.ui-callout--info    { border-inline-start-color: var(--color-info, #2563EB); }
.ui-callout--success { border-inline-start-color: var(--color-success, #16A34A); }
.ui-callout--warning { border-inline-start-color: var(--color-warning, #CA8A04); }
.ui-callout--danger  { border-inline-start-color: var(--color-danger, #DC2626); }
.ui-callout--neutral { border-inline-start-color: var(--color-border-strong, #A1A1AA); }

/* ─── StatCard ─── */
.ui-stat-card {
  display: grid;
  gap: var(--spacing-xs, 2px);
  padding: var(--spacing-lg, 16px);
  background: var(--color-surface, #FFFFFF);
  border: 1px solid var(--color-border, #E4E4E7);
  border-radius: var(--radii-md, 8px);
}
.ui-stat-card__label {
  margin: 0;
  font-size: 0.8rem;
  font-weight: 600;
  text-transform: uppercase;
  letter-spacing: 0.06em;
  color: var(--color-text-muted, #52525B);
}
.ui-stat-card__value {
  margin: 0;
  font-size: 1.75rem;
  font-weight: 700;
  line-height: 1;
  color: var(--color-text, #18181B);
}
.ui-stat-card__trend {
  margin: 0;
  font-size: 0.85rem;
  font-weight: 600;
}
.ui-stat-card__trend--up   { color: var(--color-success, #16A34A); }
.ui-stat-card__trend--down { color: var(--color-danger, #DC2626); }
.ui-stat-card__trend--flat { color: var(--color-text-muted, #52525B); }

/* ─── Avatar ─── */
.ui-avatar {
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
}
.ui-avatar__img {
  width: 100%;
  height: 100%;
  object-fit: cover;
}
.ui-avatar__initials {
  letter-spacing: 0.04em;
}

/* ─── Form ─── */
.ui-form { display: grid; gap: var(--spacing-lg, 16px); }
.ui-form__fields { display: grid; gap: var(--spacing-md, 8px); }
.ui-form__actions {
  display: flex;
  justify-content: flex-end;
  gap: var(--spacing-sm, 4px);
}

/* ─── Notification (toast row) ─── */
.ui-notification {
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
.ui-notification__icon {
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
.ui-notification__text { display: grid; gap: var(--spacing-xs, 2px); }
.ui-notification__title {
  font-size: 0.95rem;
  font-weight: 700;
  color: var(--color-text, #18181B);
}
.ui-notification__body {
  margin: 0;
  font-size: 0.9rem;
  color: var(--color-text-muted, #52525B);
}
.ui-notification__dismiss {
  align-self: start;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  inline-size: 1.5rem;
  block-size: 1.5rem;
  border-radius: var(--radii-full, 9999px);
  font-size: 1.1rem;
  line-height: 1;
  color: var(--color-text-muted, #52525B);
  text-decoration: none;
}
.ui-notification__dismiss:hover {
  background: var(--color-surface-soft, #F4F4F5);
  color: var(--color-text, #18181B);
  text-decoration: none;
}

.ui-notification--success { border-inline-start-color: var(--color-success, #16A34A); }
.ui-notification--success .ui-notification__icon { background: var(--color-success, #16A34A); }
.ui-notification--warning { border-inline-start-color: var(--color-warning, #CA8A04); }
.ui-notification--warning .ui-notification__icon { background: var(--color-warning, #CA8A04); }
.ui-notification--danger  { border-inline-start-color: var(--color-danger, #DC2626); }
.ui-notification--danger  .ui-notification__icon { background: var(--color-danger, #DC2626); }
.ui-notification--info    { border-inline-start-color: var(--color-info, #2563EB); }
.ui-notification--info    .ui-notification__icon { background: var(--color-info, #2563EB); }
.ui-notification--neutral { border-inline-start-color: var(--color-border-strong, #A1A1AA); }
.ui-notification--neutral .ui-notification__icon {
  background: var(--color-surface-soft, #F4F4F5);
  color: var(--color-text-muted, #52525B);
}

/* Floating positioning — when Position is set on Notification, it
   pins to a screen corner via position: fixed. Pure CSS, no JS. */
.ui-notification--floating {
  position: fixed;
  z-index: 1000;
  box-shadow: 0 12px 32px rgba(0, 0, 0, 0.18);
  animation: ui-notification-slide-in 220ms ease-out;
}
.ui-notification--at-top-right    { top: 1rem; right: 1rem; }
.ui-notification--at-top-left     { top: 1rem; left: 1rem; }
.ui-notification--at-bottom-right { bottom: 1rem; right: 1rem; }
.ui-notification--at-bottom-left  { bottom: 1rem; left: 1rem; }
@keyframes ui-notification-slide-in {
  from { opacity: 0; transform: translateY(-12px); }
  to   { opacity: 1; transform: translateY(0); }
}
.ui-notification--at-bottom-right,
.ui-notification--at-bottom-left {
  animation-name: ui-notification-slide-in-up;
}
@keyframes ui-notification-slide-in-up {
  from { opacity: 0; transform: translateY(12px); }
  to   { opacity: 1; transform: translateY(0); }
}
@media (prefers-reduced-motion: reduce) {
  .ui-notification--floating { animation: none; }
}

/* ─── DataTable ─── */
.ui-data-table { display: grid; gap: var(--spacing-md, 8px); }
.ui-data-table__scroll {
  overflow-x: auto;
  border: 1px solid var(--color-border, #E4E4E7);
  border-radius: var(--radii-md, 8px);
  background: var(--color-surface, #FFFFFF);
}
.ui-data-table__table {
  width: 100%;
  border-collapse: collapse;
  font-size: 0.95rem;
}
.ui-data-table__caption {
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
.ui-data-table__table th,
.ui-data-table__table td {
  padding: var(--spacing-sm, 4px) var(--spacing-lg, 16px);
  text-align: start;
  vertical-align: middle;
  border-bottom: 1px solid var(--color-border, #E4E4E7);
}
.ui-data-table__table tbody tr:last-child td {
  border-bottom: 0;
}
.ui-data-table__table th {
  font-weight: 600;
  color: var(--color-text-muted, #52525B);
  background: var(--color-surface-soft, #F4F4F5);
  font-size: 0.8rem;
  text-transform: uppercase;
  letter-spacing: 0.04em;
}
.ui-data-table__table tbody tr:hover {
  background: var(--color-surface-soft, #F4F4F5);
}
.ui-data-table__table .is-align-end   { text-align: end; }
.ui-data-table__table .is-align-center { text-align: center; }

.ui-data-table__sort {
  display: inline-flex;
  align-items: center;
  gap: 0.25rem;
  color: inherit;
  text-decoration: none;
}
.ui-data-table__sort:hover {
  color: var(--color-text, #18181B);
  text-decoration: none;
}
.ui-data-table__sort-indicator {
  font-size: 0.7em;
  color: var(--color-primary, #4F46E5);
}
.ui-data-table__table th[aria-sort="ascending"],
.ui-data-table__table th[aria-sort="descending"] {
  color: var(--color-primary, #4F46E5);
}
.ui-data-table__footer {
  display: flex;
  justify-content: flex-end;
}
.ui-data-table.is-empty { /* delegate to ui-empty-state */ }

/* ─── Visually hidden helper ─── */
.ui-visually-hidden {
  position: absolute !important;
  width: 1px; height: 1px;
  padding: 0; margin: -1px;
  overflow: hidden;
  clip: rect(0, 0, 0, 0);
  white-space: nowrap;
  border: 0;
}
`
}
