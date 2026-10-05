package ui

// ─── Skeleton compositions ──────────────────────────────────────────
//
// Loading-state presets over headless.Skeleton: one primitive render
// per preset, so exactly one polite "Loading…" announcement and one
// hidden set of bars — never a live region per line. The preset shapes
// (which line is the title, which the footer, where the circle sits)
// are the stylesheet's, keyed on the primitive's own hooks
// (data-hui-lines, data-hui-skeleton-last) and position.

import (
	"context"

	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core-ui/registry"
	"github.com/DonaldMurillo/gofastr/core-ui/style"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework/headless"
	"github.com/DonaldMurillo/gofastr/framework/i18nui"
)

// skeletonLabel resolves the preset's announcement: the caller's
// Label, or the framework's "Loading…" through i18n.
func skeletonLabel(label string, ctx context.Context) string {
	if label != "" {
		return label
	}
	if ctx == nil {
		ctx = context.Background()
	}
	return i18nui.T(ctx, i18nui.KeyLoading)
}

// skeletonClasses dresses the bars every preset draws.
func skeletonClasses(root string) headless.Classes {
	return headless.Classes{
		headless.PartRoot:           root,
		headless.PartSkeleton:       "fui-skeleton__line",
		headless.PartVisuallyHidden: "fui-visually-hidden",
	}
}

// SkeletonCardConfig configures a SkeletonCard.
type SkeletonCardConfig struct {
	// BodyLines is the number of skeleton lines rendered in the body.
	// Defaults to 2 when zero.
	BodyLines int
	// ShowFooter renders a hairline-divided skeleton footer line.
	ShowFooter bool
	// Label is the announcement the preset makes once, politely.
	// Defaults to the reader's "Loading…".
	Label string
	// Ctx carries the per-request context used to resolve the default
	// label. When nil, English fallback applies.
	Ctx   context.Context
	ID    string
	Class string

	// ExtraAttrs forwards additional attributes (data-* test hooks,
	// analytics markers) to the placeholder's root element. Keys the
	// component owns are dropped: class and id (use Class / ID),
	// style, data-cui-* and aria-hidden (skeletons are always
	// presentational; the one announcement is the primitive's).
	ExtraAttrs html.Attrs
}

// skeletonCardClasses dresses the card preset. The root wears the
// card chrome beside its own preset class, exactly as the preset
// always did.
var skeletonCardClasses = skeletonClasses("fui-card fui-skeleton-card")

// SkeletonCard renders a card-shaped loading placeholder on
// headless.Skeleton: a title line, a body line-stack, and an optional
// footer line — one announcement, bars the stylesheet sizes by
// position.
func SkeletonCard(cfg SkeletonCardConfig) render.HTML {
	bodyLines := cfg.BodyLines
	if bodyLines <= 0 {
		bodyLines = 2
	}
	lines := 1 + bodyLines
	cls := cfg.Class
	if cfg.ShowFooter {
		lines++
		// The footer line is the marked last line; the modifier is
		// what tells the sheet the mark means "footer" rather than
		// "the short last line of a paragraph".
		cls = joinNonEmpty("fui-skeleton-card--footed", cls)
	}
	return skeletonPresetsStyle.WrapHTML(headless.Skeleton(headless.SkeletonProps{
		Label:      skeletonLabel(cfg.Label, cfg.Ctx),
		Lines:      lines,
		ID:         cfg.ID,
		ExtraAttrs: headless.Safe(cfg.ExtraAttrs, "class", "id", "aria-hidden"),
		Parts:      rootClassParts(cls),
	}, skeletonCardClasses))
}

// SkeletonRowConfig configures a SkeletonRow.
type SkeletonRowConfig struct {
	// HideChevron drops the trailing chevron skeleton: use for plain
	// label/value rows that aren't drill-down navigable.
	HideChevron bool
	// Label is the announcement the preset makes once, politely.
	// Defaults to the reader's "Loading…".
	Label string
	// Ctx carries the per-request context used to resolve the default
	// label. When nil, English fallback applies.
	Ctx   context.Context
	ID    string
	Class string

	// ExtraAttrs forwards additional attributes (data-* test hooks,
	// analytics markers) to the placeholder's root element. Keys the
	// component owns are dropped: class and id (use Class / ID),
	// style, data-cui-* and aria-hidden.
	ExtraAttrs html.Attrs
}

// SkeletonRow renders a list-row loading placeholder on
// headless.Skeleton: a label line on the left, a value line on the
// right, and a trailing chevron the stylesheet draws.
func SkeletonRow(cfg SkeletonRowConfig) render.HTML {
	cls := cfg.Class
	if !cfg.HideChevron {
		// The chevron is a ::after the sheet draws as the third grid
		// column; the plain modifier drops it.
		cls = joinNonEmpty("fui-skeleton-row--chevron", cls)
	}
	return skeletonPresetsStyle.WrapHTML(headless.Skeleton(headless.SkeletonProps{
		Label:      skeletonLabel(cfg.Label, cfg.Ctx),
		Lines:      2,
		ID:         cfg.ID,
		ExtraAttrs: headless.Safe(cfg.ExtraAttrs, "class", "id", "aria-hidden"),
		Parts:      rootClassParts(cls),
	}, skeletonRowClasses))
}

// SkeletonTimelineConfig configures a SkeletonTimeline.
type SkeletonTimelineConfig struct {
	// Rows is the number of event-shaped rows to draw (dot, name
	// line, two text lines each). Default 3, the shape of a short
	// activity feed.
	Rows int
	// Label is the polite announcement. Default "Loading…".
	Label string
	// Ctx carries the per-request context used to resolve the default
	// label. When nil, English fallback applies.
	Ctx   context.Context
	ID    string
	Class string

	// ExtraAttrs forwards additional attributes (data-* test hooks,
	// analytics markers) to the placeholder's root element. Keys the
	// component owns are dropped: class and id (use Class / ID),
	// style, data-cui-* and aria-hidden.
	ExtraAttrs html.Attrs
}

var skeletonTimelineClasses = skeletonClasses("fui-skeleton-timeline")

// SkeletonTimeline renders a timeline-shaped loading placeholder on
// headless.Skeleton: one row per event — a dot on the rail, a name
// line, and two text lines — the shape ui.Timeline arrivals have, so
// the placeholder promises exactly what lands. One announcement, one
// hidden set of bars.
func SkeletonTimeline(cfg SkeletonTimelineConfig) render.HTML {
	rows := cfg.Rows
	if rows <= 0 {
		rows = 3
	}
	return skeletonPresetsStyle.WrapHTML(headless.Skeleton(headless.SkeletonProps{
		Label:      skeletonLabel(cfg.Label, cfg.Ctx),
		Lines:      rows * 4,
		ID:         cfg.ID,
		ExtraAttrs: headless.Safe(cfg.ExtraAttrs, "class", "id", "aria-hidden"),
		Parts:      rootClassParts(cfg.Class),
	}, skeletonTimelineClasses))
}

var skeletonRowClasses = skeletonClasses("fui-skeleton-row")

// SkeletonAvatarConfig configures a SkeletonAvatar.
type SkeletonAvatarConfig struct {
	// HideSubline collapses the two stacked lines into one.
	HideSubline bool
	// Label is the announcement the preset makes once, politely.
	// Defaults to the reader's "Loading…".
	Label string
	// Ctx carries the per-request context used to resolve the default
	// label. When nil, English fallback applies.
	Ctx   context.Context
	ID    string
	Class string

	// ExtraAttrs forwards additional attributes (data-* test hooks,
	// analytics markers) to the placeholder's root element. Keys the
	// component owns are dropped: class and id (use Class / ID),
	// style, data-cui-* and aria-hidden.
	ExtraAttrs html.Attrs
}

var skeletonAvatarClasses = skeletonClasses("fui-skeleton-avatar")

// SkeletonAvatar renders an avatar-with-text loading placeholder on
// headless.Skeleton: a circle on the left with the text lines on the
// right, the second line dropped when HideSubline is true. The
// circle's diameter is the --ui-skeleton-avatar-size knob (2.5rem,
// with a stylesheet override on the preset class as the other way a
// custom size is set, never an inline style a strict CSP drops).
func SkeletonAvatar(cfg SkeletonAvatarConfig) render.HTML {
	lines := 3 // circle + name + subline
	if cfg.HideSubline {
		lines = 2 // circle + name
	}
	return skeletonPresetsStyle.WrapHTML(headless.Skeleton(headless.SkeletonProps{
		Label:      skeletonLabel(cfg.Label, cfg.Ctx),
		Lines:      lines,
		ID:         cfg.ID,
		ExtraAttrs: headless.Safe(cfg.ExtraAttrs, "class", "id", "aria-hidden"),
		Parts:      rootClassParts(cfg.Class),
	}, skeletonAvatarClasses))
}

// SkeletonLineConfig configures a SkeletonLine.
type SkeletonLineConfig struct {
	// Label is the announcement the preset makes once, politely.
	// Defaults to the reader's "Loading…".
	Label string
	// Ctx carries the per-request context used to resolve the default
	// label. When nil, English fallback applies.
	Ctx   context.Context
	ID    string
	Class string

	// ExtraAttrs forwards additional attributes (data-* test hooks,
	// analytics markers) to the placeholder's root element. Keys the
	// component owns are dropped: class and id (use Class / ID),
	// style, data-cui-* and aria-hidden.
	ExtraAttrs html.Attrs
}

var skeletonLineClasses = skeletonClasses("fui-skeleton-line")

// SkeletonLine renders ONE bar on headless.Skeleton — the loading
// twin of a short text run (a breadcrumb trail, a one-line label),
// where Card/Row/Timeline would promise a shape that never arrives.
// One announcement, one bar, capped at the width a run of text
// occupies so it never reads as a full-width block.
func SkeletonLine(cfg SkeletonLineConfig) render.HTML {
	return skeletonPresetsStyle.WrapHTML(headless.Skeleton(headless.SkeletonProps{
		Label:      skeletonLabel(cfg.Label, cfg.Ctx),
		Lines:      1,
		ID:         cfg.ID,
		ExtraAttrs: headless.Safe(cfg.ExtraAttrs, "class", "id", "aria-hidden"),
		Parts:      rootClassParts(cfg.Class),
	}, skeletonLineClasses))
}

var skeletonPresetsStyle = registry.RegisterStyle("ui-skeleton-presets", func(_ style.Theme) string {
	return skeletonPresetsCSS
})

// Knobs: --ui-skeleton-bar-height (0.85rem, every preset's bar),
// --ui-skeleton-row-chevron-size (0.5rem), --ui-skeleton-avatar-size
// (2.5rem, the circle; the preset's min-height and text padding are
// calc() over it), --ui-skeleton-timeline-dot-size (12px) and
// --ui-skeleton-line-max-width (12rem, the single-bar cap).

const skeletonPresetsCSS = `
/* The one bar style every preset draws: the shimmer the retired
   core-ui pattern owned, moved here unchanged. */
.fui-skeleton__line {
  display: block;
  background: linear-gradient(
    90deg,
    var(--color-border, #E5E7EB) 0%,
    color-mix(in oklab, var(--color-border, #E5E7EB) 60%, var(--color-surface, #FFFFFF) 40%) 50%,
    var(--color-border, #E5E7EB) 100%
  );
  background-size: 200% 100%;
  animation: fui-skeleton-shimmer 1.4s ease-in-out infinite;
  border-radius: var(--radii-full, 9999px);
  block-size: var(--ui-skeleton-bar-height, 0.85rem);
  inline-size: 100%;
}
@keyframes fui-skeleton-shimmer {
  0%   { background-position: 200% 0; }
  100% { background-position: -200% 0; }
}
@media (prefers-reduced-motion: reduce) {
  .fui-skeleton__line { animation: none; }
}

/* Card: the first line is the title, the marked last line is the
   footer, and the middle lines are the body. The rhythm is the
   retired pattern's, proved by the before and after captures: the
   pattern drew the body lines inside their own stack (one tight gap)
   while the title and the footer sat a full step away. The flat line
   list reproduces that arithmetic on the same tokens: the grid gap
   is the stack's tight gap, the first body line buys the difference
   up to a full step, and the footer buys the separation the pattern
   gave it (a full step above its own margin). */
.fui-skeleton-card {
  display: grid;
  gap: var(--spacing-sm, 4px);
  padding: var(--spacing-lg, 16px);
}
.fui-skeleton-card > .fui-skeleton__line:first-child { inline-size: 50%; }
.fui-skeleton-card > .fui-skeleton__line:nth-child(2) {
  margin-block-start: calc(var(--spacing-md, 8px) - var(--spacing-sm, 4px));
}
/* The pattern's short last line: the final line of a paragraph reads
   as text, not as a block. */
.fui-skeleton-card > .fui-skeleton__line[data-hui-skeleton-last]:nth-child(n+3) {
  inline-size: 65%;
}
.fui-skeleton-card--footed > .fui-skeleton__line[data-hui-skeleton-last] {
  inline-size: 35%;
  margin-block-start: var(--spacing-md, 8px);
  padding-block-start: var(--spacing-md, 8px);
  border-block-start: var(--stroke-thin, 1px) solid var(--color-border, #E5E7EB);
}
/* A footed card's body keeps its own short last line: the footer is
   a separate rhythm below it, and the mark names the footer, not the
   body's end. The primitive's announcement is the root's last child,
   so the footer is the second-last and the body's last line the
   third-last. */
.fui-skeleton-card--footed > .fui-skeleton__line:nth-last-child(3):nth-child(n+3) {
  inline-size: 65%;
}

/* Row: a label line, a value line, and — unless the plain modifier
   is set — a chevron the sheet itself draws as the last grid
   column. */
.fui-skeleton-row {
  display: grid;
  grid-template-columns: 1fr auto;
  gap: var(--spacing-md, 8px);
  align-items: center;
  padding-block: var(--spacing-sm, 4px);
  padding-inline: var(--spacing-md, 8px);
  border-block-end: var(--stroke-thin, 1px) solid var(--color-border, #E5E7EB);
}
.fui-skeleton-row--chevron { grid-template-columns: 1fr auto auto; }
.fui-skeleton-row > .fui-skeleton__line:first-child { inline-size: 40%; }
.fui-skeleton-row > .fui-skeleton__line:nth-child(2) { inline-size: 25%; }
.fui-skeleton-row--chevron::after {
  content: "";
  display: inline-block;
  inline-size: var(--ui-skeleton-row-chevron-size, 0.5rem);
  block-size: var(--ui-skeleton-row-chevron-size, 0.5rem);
  border-block-start: var(--stroke-thick, 2px) solid var(--color-border, #E5E7EB);
  border-inline-end: var(--stroke-thick, 2px) solid var(--color-border, #E5E7EB);
  transform: rotate(45deg);
  opacity: var(--opacity-muted, 0.6);
}

/* Avatar: the first line is the circle, the rest are the text
   column. The circle is pinned out of the flow so the two text lines
   stack at their own tight rhythm and the pair centres beside it —
   the geometry the retired pattern drew with a nested text block. */
.fui-skeleton-avatar {
  position: relative;
  display: grid;
  row-gap: var(--spacing-xs, 2px);
  align-content: center;
  min-block-size: var(--ui-skeleton-avatar-size, 2.5rem);
  padding-inline-start: calc(var(--ui-skeleton-avatar-size, 2.5rem) + var(--spacing-md, 8px));
}
.fui-skeleton-avatar > .fui-skeleton__line:first-child {
  position: absolute;
  inset-block-start: 0;
  inline-size: var(--ui-skeleton-avatar-size, 2.5rem);
  block-size: var(--ui-skeleton-avatar-size, 2.5rem);
  border-radius: var(--radii-full, 9999px);
}
.fui-skeleton-avatar > .fui-skeleton__line:nth-child(2) { inline-size: 60%; }
.fui-skeleton-avatar > .fui-skeleton__line:nth-child(3) { inline-size: 40%; }

/* Timeline: one row per event — dot on the rail, name line, two
   text lines — the shape ui.Timeline arrivals have. Every row is
   four bars in the flat list; the grid spans the dot over the
   row's three text lines (grid-template-columns matches the real
   timeline's 16px rail + 1fr content columns). The visually-hidden
   announcement rides after the bars and is no grid item (the
   primitive's sheet hides it). */
.fui-skeleton-timeline {
  display: grid;
  grid-template-columns: var(--spacing-lg, 16px) 1fr;
  column-gap: var(--spacing-md, 8px);
  row-gap: var(--spacing-xs, 2px);
  align-content: start;
}
.fui-skeleton-timeline > .fui-skeleton__line:nth-child(4n+1) {
  grid-row: span 3;
  justify-self: center;
  align-self: start;
  inline-size: var(--ui-skeleton-timeline-dot-size, 12px);
  block-size: var(--ui-skeleton-timeline-dot-size, 12px);
  border-radius: var(--radii-full, 9999px);
  margin-block-start: var(--spacing-xs, 2px);
}
.fui-skeleton-timeline > .fui-skeleton__line:nth-child(4n+2) { grid-column: 2; inline-size: 60%; }
.fui-skeleton-timeline > .fui-skeleton__line:nth-child(4n+3) { grid-column: 2; }
.fui-skeleton-timeline > .fui-skeleton__line:nth-child(4n+4) { grid-column: 2; inline-size: 65%; margin-block-end: var(--spacing-lg, 16px); }
.fui-skeleton-timeline > .fui-skeleton__line[data-hui-skeleton-last] { margin-block-end: 0; }

/* Line: ONE bar — the loading twin of a short text run (a breadcrumb
   trail, a one-line label), where Card/Row/Timeline would promise a
   shape that never arrives. Capped so it reads as a run of text, not
   a full-width block. */
.fui-skeleton-line { display: block; }
.fui-skeleton-line > .fui-skeleton__line { inline-size: min(100%, var(--ui-skeleton-line-max-width, 12rem)); }
`
