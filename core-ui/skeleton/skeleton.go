// Package skeleton provides shimmer placeholders rendered with pure CSS.
//
// Use a skeleton during server-side data loads or while a signal-driven
// island fetches replacement HTML — render the skeleton in the initial
// HTML, then swap it for the real content when the data resolves.
//
// All variants are visually-only: aria-hidden="true" so screen readers
// skip them and assistive tech announces the surrounding container's
// loading state instead.
package skeleton

import (
	"fmt"
	"strings"

	"github.com/gofastr/gofastr/core/render"
)

// Variant selects the skeleton shape.
type Variant string

const (
	Line   Variant = "line"   // single text line
	Block  Variant = "block"  // rectangular block (paragraphs, cards)
	Circle Variant = "circle" // circular (avatars)
)

// Config configures a skeleton.
type Config struct {
	Variant Variant // default: Line
	Width   string  // CSS length, e.g. "100%", "12rem". Defaults vary by variant.
	Height  string  // CSS length. Default depends on variant.
	Count   int     // number of repeated lines (Line variant only). Default: 1.
	Class   string
	ID      string
}

// New renders one skeleton shape per Config. For multi-line skeletons,
// pass Count > 1 with Variant=Line.
func New(cfg Config) render.HTML {
	v := cfg.Variant
	if v == "" {
		v = Line
	}
	count := cfg.Count
	if count <= 0 {
		count = 1
	}

	if count == 1 {
		return shape(v, cfg)
	}

	wrapCls := "skeleton-stack"
	if cfg.Class != "" {
		wrapCls = wrapCls + " " + cfg.Class
	}
	wrapAttrs := map[string]string{"class": wrapCls, "aria-hidden": "true"}
	if cfg.ID != "" {
		wrapAttrs["id"] = cfg.ID
	}
	children := make([]render.HTML, count)
	for i := 0; i < count; i++ {
		// Last line is shorter (visually mimics a paragraph).
		w := cfg.Width
		if w == "" && i == count-1 {
			w = "65%"
		}
		c := cfg
		c.Class = ""
		c.ID = ""
		c.Width = w
		children[i] = shape(v, c)
	}
	return render.Tag("div", wrapAttrs, children...)
}

func shape(v Variant, cfg Config) render.HTML {
	classes := []string{"skeleton", "skeleton-" + string(v)}
	if cfg.Class != "" {
		classes = append(classes, cfg.Class)
	}
	attrs := map[string]string{
		"class":       strings.Join(classes, " "),
		"aria-hidden": "true",
	}
	if cfg.ID != "" {
		attrs["id"] = cfg.ID
	}
	style := skeletonStyle(v, cfg.Width, cfg.Height)
	if style != "" {
		attrs["style"] = style
	}
	return render.Tag("div", attrs)
}

func skeletonStyle(v Variant, w, h string) string {
	var parts []string
	if w != "" {
		parts = append(parts, fmt.Sprintf("inline-size:%s", w))
	}
	if h != "" {
		parts = append(parts, fmt.Sprintf("block-size:%s", h))
	}
	if v == Circle {
		side := "2.5rem"
		if w != "" {
			side = w
		}
		if h != "" {
			side = h
		}
		parts = []string{
			fmt.Sprintf("inline-size:%s", side),
			fmt.Sprintf("block-size:%s", side),
		}
	}
	return strings.Join(parts, ";")
}

// BaseCSS returns the stylesheet for skeletons. Tokens consumed:
// --color-border, --color-surface, --radii-sm, --radii-full, --spacing-sm.
func BaseCSS() string {
	return `
.skeleton {
  display: block;
  background: linear-gradient(
    90deg,
    var(--color-border, #E5E7EB) 0%,
    color-mix(in oklab, var(--color-border, #E5E7EB) 60%, var(--color-surface, #FFFFFF) 40%) 50%,
    var(--color-border, #E5E7EB) 100%
  );
  background-size: 200% 100%;
  animation: skeleton-shimmer 1.4s ease-in-out infinite;
  border-radius: var(--radii-sm, 4px);
}
.skeleton-line {
  block-size: 0.85rem;
  inline-size: 100%;
  border-radius: var(--radii-full, 9999px);
}
.skeleton-block {
  block-size: 6rem;
  inline-size: 100%;
}
.skeleton-circle {
  inline-size: 2.5rem;
  block-size: 2.5rem;
  border-radius: var(--radii-full, 9999px);
}
.skeleton-stack {
  display: grid;
  gap: var(--spacing-sm, 4px);
}
@keyframes skeleton-shimmer {
  0%   { background-position: 200% 0; }
  100% { background-position: -200% 0; }
}
@media (prefers-reduced-motion: reduce) {
  .skeleton { animation: none; }
}
`
}
