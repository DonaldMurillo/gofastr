package ui

import (
	"context"

	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core-ui/registry"
	"github.com/DonaldMurillo/gofastr/core-ui/style"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework/headless"
	"github.com/DonaldMurillo/gofastr/framework/i18nui"
)

func skeletonLabel(label string, ctx context.Context) string {
	if label != "" {
		return label
	}
	if ctx == nil {
		ctx = context.Background()
	}
	return i18nui.Loading.Text(ctx)
}

func skeletonClasses(root string) headless.Classes {
	return headless.Classes{
		headless.PartRoot:           root,
		headless.PartSkeleton:       "fui-skeleton__line",
		headless.PartVisuallyHidden: "fui-visually-hidden",
	}
}

// SkeletonCardConfig configures a SkeletonCard.
type SkeletonCardConfig struct {
	// BodyLines is the number of body lines. Defaults to 2.
	BodyLines int
	// ShowFooter adds a footer line below a hairline.
	ShowFooter bool
	// Label is the one polite announcement. Defaults to "Loading…".
	Label string
	// Ctx resolves the default label; nil uses English.
	Ctx   context.Context
	ID    string
	Class string

	// ExtraAttrs forwards attributes to the root; class, id, style, data-cui-* and aria-hidden are dropped.
	ExtraAttrs html.Attrs
}

var skeletonCardClasses = skeletonClasses("fui-card fui-skeleton-card")

// SkeletonCard renders a card-shaped loading placeholder: a title, body lines and an optional footer.
func SkeletonCard(cfg SkeletonCardConfig) render.HTML {
	bodyLines := cfg.BodyLines
	if bodyLines <= 0 {
		bodyLines = 2
	}
	lines := 1 + bodyLines
	cls := cfg.Class
	if cfg.ShowFooter {
		lines++
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
	// HideChevron drops the trailing chevron, for a row that links nowhere.
	HideChevron bool
	// Label is the one polite announcement. Defaults to "Loading…".
	Label string
	// Ctx resolves the default label; nil uses English.
	Ctx   context.Context
	ID    string
	Class string

	// ExtraAttrs forwards attributes to the root; class, id, style, data-cui-* and aria-hidden are dropped.
	ExtraAttrs html.Attrs
}

// SkeletonRow renders a list-row loading placeholder: a label, a value and a chevron.
func SkeletonRow(cfg SkeletonRowConfig) render.HTML {
	cls := cfg.Class
	if !cfg.HideChevron {
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
	// Rows is the number of event rows, a dot and three lines each. Defaults to 3.
	Rows int
	// Label is the one polite announcement. Defaults to "Loading…".
	Label string
	// Ctx resolves the default label; nil uses English.
	Ctx   context.Context
	ID    string
	Class string

	// ExtraAttrs forwards attributes to the root; class, id, style, data-cui-* and aria-hidden are dropped.
	ExtraAttrs html.Attrs
}

var skeletonTimelineClasses = skeletonClasses("fui-skeleton-timeline")

// SkeletonTimeline renders a loading placeholder in the shape of a ui.Timeline's events.
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
	// Label is the one polite announcement. Defaults to "Loading…".
	Label string
	// Ctx resolves the default label; nil uses English.
	Ctx   context.Context
	ID    string
	Class string

	// ExtraAttrs forwards attributes to the root; class, id, style, data-cui-* and aria-hidden are dropped.
	ExtraAttrs html.Attrs
}

var skeletonAvatarClasses = skeletonClasses("fui-skeleton-avatar")

// SkeletonAvatar renders a loading placeholder of a circle beside a name line and a subline.
func SkeletonAvatar(cfg SkeletonAvatarConfig) render.HTML {
	lines := 3
	if cfg.HideSubline {
		lines = 2
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
	// Label is the one polite announcement. Defaults to "Loading…".
	Label string
	// Ctx resolves the default label; nil uses English.
	Ctx   context.Context
	ID    string
	Class string

	// ExtraAttrs forwards attributes to the root; class, id, style, data-cui-* and aria-hidden are dropped.
	ExtraAttrs html.Attrs
}

var skeletonLineClasses = skeletonClasses("fui-skeleton-line")

// SkeletonLine renders one bar capped at a short run of text's width, for a one-line label.
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

const skeletonPresetsCSS = `
[data-cui-comp="ui-skeleton-presets"] .fui-visually-hidden {
  position: absolute;
  inline-size: 1px;
  block-size: 1px;
  padding: 0;
  margin: -1px;
  overflow: hidden;
  clip-path: inset(50%);
  white-space: nowrap;
}
/* The one bar style every preset draws: the shimmer the retired
   core-ui pattern owned, moved here unchanged. */
.fui-skeleton__line {
  display: block;
  background: linear-gradient(
    90deg,
    var(--color-border, #E4E4E7) 0%,
    color-mix(in oklab, var(--color-border, #E4E4E7) 60%, var(--color-surface, #FFFFFF) 40%) 50%,
    var(--color-border, #E4E4E7) 100%
  );
  background-size: 200% 100%;
  animation: fui-skeleton-shimmer 1.4s var(--easing-ease-in-out, ease-in-out) infinite;
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
  border-block-start: var(--stroke-thin, 1px) solid var(--color-border, #E4E4E7);
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
  border-block-end: var(--stroke-thin, 1px) solid var(--color-border, #E4E4E7);
}
.fui-skeleton-row--chevron { grid-template-columns: 1fr auto auto; }
.fui-skeleton-row > .fui-skeleton__line:first-child { inline-size: 40%; }
.fui-skeleton-row > .fui-skeleton__line:nth-child(2) { inline-size: 25%; }
.fui-skeleton-row--chevron::after {
  content: "";
  display: inline-block;
  inline-size: var(--ui-skeleton-row-chevron-size, 0.5rem);
  block-size: var(--ui-skeleton-row-chevron-size, 0.5rem);
  border-block-start: var(--stroke-thick, 2px) solid var(--color-border, #E4E4E7);
  border-inline-end: var(--stroke-thick, 2px) solid var(--color-border, #E4E4E7);
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
   preset's own scoped visually-hidden rule hides it). */
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
