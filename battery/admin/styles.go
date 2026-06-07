package admin

// The admin ships its own stylesheet so it looks finished in ANY host — a bare
// uihost only emits theme TOKENS, not element styling. Every value here is a
// theme variable (--color-*, --spacing-*, --radii-*, --font-*) with a sane
// fallback, so an app restyles the whole back-office by setting its theme; no
// app-local CSS required. The sheet is a registered component (served as
// /__gofastr/comp/ui-admin.css and injected after host CSS, so it wins
// equal-specificity), loaded automatically wherever the `ui-admin` marker
// appears — every admin screen is wrapped via adminStyle.WrapHTML.
//
// Layout knobs an app can override on its theme without touching this file:
//   --admin-rail   width of the desktop nav rail (default 15rem)
//   --admin-gutter content padding (default clamp(...))

import (
	"github.com/DonaldMurillo/gofastr/core-ui/registry"
	"github.com/DonaldMurillo/gofastr/core-ui/style"
	"github.com/DonaldMurillo/gofastr/core/render"
)

// adminStyle registers the admin's stylesheet (side-effect at package init);
// the host serves it wherever the `ui-admin` marker appears.
var _ = registry.RegisterStyle("ui-admin", adminCSS)

// shell wraps a screen root in a fresh element carrying the `ui-admin` marker
// so ui-admin.css loads. We can't reuse WrapHTML here because the screen root
// (a ui.Container) already carries its own data-fui-comp marker, and a tag
// can only advertise one component.
func (b *Battery) shell(root render.HTML) render.HTML {
	return render.Tag("div", map[string]string{"data-fui-comp": "ui-admin", "class": "admin-root"}, root)
}

func adminCSS(_ style.Theme) string {
	return `
/* ── shell ─────────────────────────────────────────────────────────────────
   The nav is an interactive.SectionMenu: a sticky rail ≥900px, a mounted
   slide-in drawer (via a trigger button) <900px. So the grid is two-column at
   ≥900px (rail + content) and single-column below, matching the SectionMenu
   breakpoint. The rail column just provides a surface + divider; SectionMenu
   handles its own sticky/scroll. */
.layout-admin { min-block-size: 100dvh; background: var(--color-background, #0c0c0d); }
.layout-admin .layout-body {
  display: grid;
  grid-template-columns: 1fr;
  min-block-size: 100dvh;
}
.layout-admin .layout-body > nav {
  padding: var(--spacing-md, 12px) var(--admin-gutter, clamp(1rem, 0.5rem + 3vw, 3rem));
  border-block-end: 1px solid var(--color-border, #2a2b2e);
}
.layout-admin .layout-content,
.layout-admin > .layout-body > main {
  min-inline-size: 0;
  padding: clamp(1.25rem, 1rem + 2vw, 2.5rem) var(--admin-gutter, clamp(1rem, 0.5rem + 3vw, 3rem));
}
.layout-admin .admin-entity { max-inline-size: 72rem; }

@media (min-width: 900px) {
  .layout-admin .layout-body { grid-template-columns: var(--admin-rail, 16rem) minmax(0, 1fr); }
  .layout-admin .layout-body > nav {
    background: var(--color-surface, #17181a);
    border-block-end: 0;
    border-inline-end: 1px solid var(--color-border, #2a2b2e);
    padding: clamp(1.25rem, 1rem + 1vw, 1.75rem) var(--spacing-lg, 16px);
  }
}

/* ── page header rhythm ────────────────────────────────────────────────── */
.layout-admin .admin-entity > .ui-page-header { margin-block-end: clamp(1rem, 0.5rem + 2vw, 1.75rem); }

/* ── toolbar: search + result summary ──────────────────────────────────── */
.admin-toolbar {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: var(--spacing-md, 12px);
  margin-block-end: var(--spacing-lg, 16px);
}
.admin-toolbar [data-fui-comp="ui-search-input"] { flex: 1 1 16rem; max-inline-size: 26rem; }
.admin-toolbar .ui-search-input,
.admin-toolbar .ui-search-input__form { inline-size: 100%; }

/* Sort control — a native <details> dropdown so it works on every viewport
   (the mobile card layout hides the clickable column headers). */
.admin-sort { position: relative; }
.admin-sort__summary {
  list-style: none;
  cursor: pointer;
  display: inline-flex; align-items: center; gap: 0.4rem;
  padding-inline: 0.9rem;
  min-block-size: var(--spacing-touch-target, 44px);
  border: 1px solid var(--color-border, #2a2b2e);
  border-radius: var(--radii-md, 8px);
  background: var(--color-surface, #17181a);
  color: var(--color-text, #f2f2f3);
  font-size: 0.9rem; font-weight: 500;
  white-space: nowrap;
}
.admin-sort__summary::-webkit-details-marker { display: none; }
.admin-sort__summary::after { content: "▾"; color: var(--color-text-muted, #a8a8ad); font-size: 0.8em; }
.admin-sort__summary:hover { border-color: var(--color-border-strong, #3d3e42); }
.admin-sort__menu {
  position: absolute; z-index: 30;
  inset-block-start: calc(100% + 0.35rem); inset-inline-start: 0;
  min-inline-size: 13rem; max-inline-size: min(20rem, calc(100vw - 2rem));
  max-block-size: 60vh; overflow-y: auto;
  display: grid; gap: 1px; padding: 0.35rem;
  background: var(--color-surface, #17181a);
  border: 1px solid var(--color-border, #2a2b2e);
  border-radius: var(--radii-md, 8px);
  box-shadow: var(--shadow-md, 0 12px 32px rgba(0,0,0,0.4));
}
.admin-sort__opt {
  padding: 0.5rem 0.65rem; border-radius: var(--radii-md, 6px);
  color: var(--color-text, #f2f2f3); text-decoration: none; font-size: 0.9rem;
  white-space: nowrap;
}
.admin-sort__opt:hover { background: var(--color-surface-soft, #202123); }
.admin-sort__opt[aria-current="true"] { color: var(--color-primary, #f0b429); font-weight: 600; }

/* Active-search chip — a quiet pill, not a stripe. Shows the term + count
   and a real link back to the unfiltered list (so clearing always works). */
.admin-filter {
  display: inline-flex;
  align-items: center;
  gap: var(--spacing-sm, 8px);
  padding: 0.35rem 0.35rem 0.35rem 0.75rem;
  border: 1px solid var(--color-border, #2a2b2e);
  border-radius: 999px;
  background: var(--color-surface, #17181a);
  color: var(--color-text-muted, #a8a8ad);
  font-size: 0.875rem;
}
.admin-filter strong { color: var(--color-text, #f2f2f3); font-weight: 600; }
.admin-filter__clear {
  display: inline-flex; align-items: center; justify-content: center;
  inline-size: 1.4rem; block-size: 1.4rem; border-radius: 999px;
  color: var(--color-text-muted, #a8a8ad); text-decoration: none; line-height: 1;
  transition: background 120ms ease, color 120ms ease;
}
.admin-filter__clear:hover { background: var(--color-border, #2a2b2e); color: var(--color-text, #f2f2f3); }

/* ── list cells ────────────────────────────────────────────────────────── */
/* IDs are reference data, not the headline — render them quiet + monospace
   and clipped so they never dominate the row. */
.admin-id {
  font-family: var(--font-mono, ui-monospace, SFMono-Regular, "JetBrains Mono", monospace);
  font-size: 0.8125rem;
  color: var(--color-text-subtle, #818187);
  max-inline-size: 12ch;
  overflow: hidden; text-overflow: ellipsis; white-space: nowrap;
  display: inline-block; vertical-align: bottom;
}
/* Booleans read as a glanceable pill, not the word "false". */
.admin-bool {
  display: inline-flex; align-items: center; gap: 0.35rem;
  font-size: 0.8125rem; font-weight: 500;
  padding: 0.15rem 0.55rem; border-radius: 999px;
  border: 1px solid var(--color-border, #2a2b2e);
  color: var(--color-text-muted, #a8a8ad);
}
.admin-bool::before {
  content: ""; inline-size: 0.5rem; block-size: 0.5rem; border-radius: 999px;
  background: var(--color-border-strong, #3d3e42);
}
.admin-bool[data-on="true"] { color: var(--color-text, #f2f2f3); }
.admin-bool[data-on="true"]::before { background: var(--color-success, #57b894); }
/* Truncate long free-text cells so the table keeps its shape. */
.admin-truncate {
  display: inline-block; max-inline-size: 24ch;
  overflow: hidden; text-overflow: ellipsis; white-space: nowrap; vertical-align: bottom;
}
.admin-muted { color: var(--color-text-subtle, #818187); }

/* ── row actions: a quiet cluster, destructive action de-emphasised ─────── */
.admin-row-actions {
  display: inline-flex; align-items: center; gap: var(--spacing-sm, 8px);
  justify-content: flex-end;
}
.admin-row-actions .ui-button--danger {
  background: transparent;
  color: var(--color-danger, #e5645f);
  border: 1px solid color-mix(in oklab, var(--color-danger, #e5645f) 40%, transparent);
}
.admin-row-actions .ui-button--danger:hover {
  background: color-mix(in oklab, var(--color-danger, #e5645f) 14%, transparent);
}

/* ── detail / show: a clean two-column definition grid ──────────────────── */
.admin-detail {
  display: grid;
  /* Label column sizes to its widest label (capped) instead of a fixed wide
     column — no dead space between a short label and its value. */
  grid-template-columns: minmax(6rem, max-content) minmax(0, 1fr);
  gap: 0;
  margin: 0;
  border: 1px solid var(--color-border, #2a2b2e);
  border-radius: var(--radii-lg, 12px);
  overflow: hidden;
  background: var(--color-surface, #17181a);
}
.admin-detail__label {
  padding: 0.85rem 1rem;
  font-size: 0.75rem; text-transform: uppercase; letter-spacing: 0.05em;
  color: var(--color-text-subtle, #818187);
  background: color-mix(in oklab, var(--color-surface, #17181a) 60%, var(--color-background, #0c0c0d));
  border-block-start: 1px solid var(--color-border, #2a2b2e);
}
.admin-detail__value {
  padding: 0.85rem 1rem;
  color: var(--color-text, #f2f2f3);
  border-block-start: 1px solid var(--color-border, #2a2b2e);
  word-break: break-word;
}
.admin-detail__label:first-of-type,
.admin-detail__value:nth-of-type(1) { border-block-start: 0; }
.admin-detail__value .admin-id { max-inline-size: none; }

@media (max-width: 33rem) {
  .admin-detail { grid-template-columns: 1fr; }
  .admin-detail__label { padding-block-end: 0.15rem; border-block-start: 1px solid var(--color-border, #2a2b2e); }
  .admin-detail__value { padding-block-start: 0.15rem; border-block-start: 0; }
}

/* ── header action clusters (detail Edit/Back, etc.) ────────────────────── */
.admin-actions { display: inline-flex; align-items: center; gap: var(--spacing-md, 12px); flex-wrap: wrap; }

/* ── result count footer ────────────────────────────────────────────────── */
.admin-listfoot { margin-block-start: var(--spacing-md, 12px); display: flex; justify-content: flex-end; }
.admin-count { color: var(--color-text-subtle, #818187); font-size: 0.8125rem; }

/* ── typed cell renderers (image / file / json / prose / mono) ──────────── */
.admin-thumb {
  inline-size: 2.5rem; block-size: 2.5rem; object-fit: cover;
  border-radius: var(--radii-md, 6px); border: 1px solid var(--color-border, #2a2b2e);
  background: var(--color-surface-soft, #202123); vertical-align: middle;
}
.admin-thumb--lg { inline-size: 7.5rem; block-size: 7.5rem; }
.admin-file { color: var(--color-primary, #f0b429); text-decoration: none; }
.admin-file:hover { text-decoration: underline; }
.admin-mono {
  font-family: var(--font-mono, ui-monospace, SFMono-Regular, "JetBrains Mono", monospace);
  font-size: 0.8125rem; color: var(--color-text-muted, #a8a8ad);
}
.admin-json {
  margin: 0; padding: 0.75rem 1rem;
  background: color-mix(in oklab, var(--color-surface, #17181a) 50%, var(--color-background, #0c0c0d));
  border-radius: var(--radii-md, 8px);
  font-family: var(--font-mono, ui-monospace, SFMono-Regular, "JetBrains Mono", monospace);
  font-size: 0.8125rem; line-height: 1.5;
  overflow: auto; max-block-size: 20rem;
}
.admin-prose { white-space: pre-wrap; line-height: 1.6; }
`
}
