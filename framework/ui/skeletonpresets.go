package ui

import (
	"github.com/DonaldMurillo/gofastr/core-ui/patterns/skeleton"
	"github.com/DonaldMurillo/gofastr/core-ui/registry"
	"github.com/DonaldMurillo/gofastr/core-ui/style"
	"github.com/DonaldMurillo/gofastr/core/render"
)

// ─── Skeleton compositions ──────────────────────────────────────────
//
// Themed loading-state preset over `core-ui/patterns/skeleton`. The
// underlying skeleton primitive emits `aria-hidden="true"`, so these
// compositions inherit the same visual-only semantics — assistive tech
// announces the surrounding container's loading state, not the
// skeleton shapes.

// SkeletonCardConfig configures a SkeletonCard.
type SkeletonCardConfig struct {
	// BodyLines is the number of skeleton lines rendered in the body.
	// Defaults to 2 when zero.
	BodyLines int
	// ShowFooter renders a hairline-divided skeleton footer line.
	ShowFooter bool
	ID         string
	Class      string
}

// SkeletonCard renders a card-shaped loading placeholder: a title
// line, a body line-stack, and an optional footer line.
func SkeletonCard(cfg SkeletonCardConfig) render.HTML {
	if cfg.BodyLines <= 0 {
		cfg.BodyLines = 2
	}
	cls := "ui-card ui-skeleton-card"
	if cfg.Class != "" {
		cls += " " + cfg.Class
	}
	attrs := map[string]string{
		"class":       cls,
		"aria-hidden": "true",
	}
	if cfg.ID != "" {
		attrs["id"] = cfg.ID
	}

	// Width values are NOT set via Width: "50%" because that would emit
	// inline `style="inline-size:50%"` which strict CSP blocks. The
	// widths instead live on the preset class (.ui-skeleton-card__*).
	children := []render.HTML{
		skeleton.New(skeleton.Config{Variant: skeleton.Line, Class: "ui-skeleton-card__title"}),
		skeleton.New(skeleton.Config{Variant: skeleton.Line, Count: cfg.BodyLines, Class: "ui-skeleton-card__body"}),
	}
	if cfg.ShowFooter {
		children = append(children,
			skeleton.New(skeleton.Config{Variant: skeleton.Line, Class: "ui-skeleton-card__footer"}),
		)
	}
	return skeletonPresetsStyle.WrapHTML(render.Tag("div", attrs, children...))
}

// SkeletonRowConfig configures a SkeletonRow.
type SkeletonRowConfig struct {
	// HideChevron drops the trailing chevron skeleton — use for plain
	// label/value rows that aren't drill-down navigable.
	HideChevron bool
	ID          string
	Class       string
}

// SkeletonRow renders a list-row loading placeholder: a label line on
// the left, a value line on the right, and an optional trailing
// chevron square. Pairs with list/menu rows that drill into details.
func SkeletonRow(cfg SkeletonRowConfig) render.HTML {
	cls := "ui-skeleton-row"
	if cfg.Class != "" {
		cls += " " + cfg.Class
	}
	attrs := map[string]string{
		"class":       cls,
		"aria-hidden": "true",
	}
	if cfg.ID != "" {
		attrs["id"] = cfg.ID
	}

	children := []render.HTML{
		skeleton.New(skeleton.Config{Variant: skeleton.Line, Class: "ui-skeleton-row__label"}),
		skeleton.New(skeleton.Config{Variant: skeleton.Line, Class: "ui-skeleton-row__value"}),
	}
	if !cfg.HideChevron {
		children = append(children,
			render.Tag("span", map[string]string{
				"class":       "ui-skeleton-row__chevron",
				"aria-hidden": "true",
			}),
		)
	}
	return skeletonPresetsStyle.WrapHTML(render.Tag("div", attrs, children...))
}

// SkeletonAvatarConfig configures a SkeletonAvatar.
type SkeletonAvatarConfig struct {
	// Size overrides the circle diameter (CSS length, e.g. "3rem").
	// Defaults to the skeleton primitive's 2.5rem when empty.
	Size string
	// HideSubline collapses the two stacked lines into one.
	HideSubline bool
	ID          string
	Class       string
}

// SkeletonAvatar renders an avatar-with-text loading placeholder: a
// circle on the left with two stacked text lines on the right. The
// second line is dropped when HideSubline is true.
func SkeletonAvatar(cfg SkeletonAvatarConfig) render.HTML {
	cls := "ui-skeleton-avatar"
	if cfg.Class != "" {
		cls += " " + cfg.Class
	}
	attrs := map[string]string{
		"class":       cls,
		"aria-hidden": "true",
	}
	if cfg.ID != "" {
		attrs["id"] = cfg.ID
	}

	circle := skeleton.New(skeleton.Config{
		Variant: skeleton.Circle,
		Width:   cfg.Size,
		Class:   "ui-skeleton-avatar__circle",
	})

	textBlock := []render.HTML{
		skeleton.New(skeleton.Config{Variant: skeleton.Line, Class: "ui-skeleton-avatar__name"}),
	}
	if !cfg.HideSubline {
		textBlock = append(textBlock,
			skeleton.New(skeleton.Config{Variant: skeleton.Line, Class: "ui-skeleton-avatar__sub"}),
		)
	}

	return skeletonPresetsStyle.WrapHTML(render.Tag("div", attrs,
		circle,
		render.Tag("div", map[string]string{"class": "ui-skeleton-avatar__text"}, textBlock...),
	))
}

var skeletonPresetsStyle = registry.RegisterStyle("ui-skeleton-presets", func(_ style.Theme) string {
	return skeletonPresetsCSS
})

const skeletonPresetsCSS = `
.ui-skeleton-card {
  display: grid;
  gap: var(--spacing-md, 12px);
  padding: var(--spacing-lg, 16px);
}
/* Per-preset line widths. Defined on classes (instead of via the
   skeleton primitive's Width arg) so strict CSP doesn't strip them
   off the rendered element. */
.ui-skeleton-card__title  { inline-size: 50%; }
.ui-skeleton-card__footer { inline-size: 35%; }
.ui-skeleton-row__label   { inline-size: 40%; }
.ui-skeleton-row__value   { inline-size: 25%; }
.ui-skeleton-avatar__name { inline-size: 60%; }
.ui-skeleton-avatar__sub  { inline-size: 40%; }
.ui-skeleton-card__footer {
  margin-block-start: var(--spacing-sm, 8px);
  padding-block-start: var(--spacing-md, 12px);
  border-block-start: 1px solid var(--color-border, #E5E7EB);
}
.ui-skeleton-row {
  display: grid;
  grid-template-columns: 1fr auto auto;
  gap: var(--spacing-md, 12px);
  align-items: center;
  padding-block: var(--spacing-sm, 8px);
  padding-inline: var(--spacing-md, 12px);
  border-block-end: 1px solid var(--color-border, #E5E7EB);
}
.ui-skeleton-row__chevron {
  display: inline-block;
  inline-size: 0.5rem;
  block-size: 0.5rem;
  border-block-start: 2px solid var(--color-border, #E5E7EB);
  border-inline-end: 2px solid var(--color-border, #E5E7EB);
  transform: rotate(45deg);
  opacity: 0.6;
}
.ui-skeleton-avatar {
  display: grid;
  grid-template-columns: auto 1fr;
  gap: var(--spacing-md, 12px);
  align-items: center;
}
.ui-skeleton-avatar__text {
  display: grid;
  gap: var(--spacing-xs, 4px);
}
`
