package ui

import (
	"strconv"

	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core-ui/registry"
	"github.com/DonaldMurillo/gofastr/core-ui/style"
	"github.com/DonaldMurillo/gofastr/core/render"
)

// ─── AvatarGroup ────────────────────────────────────────────────────
//
// Overlapping stack of avatars with a "+N" overflow indicator. The
// stack uses CSS negative margins (no inline styles — strict-CSP
// safe) and propagates the group Size to its children unless the
// individual AvatarConfig already set one.

// AvatarGroupConfig configures an avatar group / stack.
type AvatarGroupConfig struct {
	// Avatars is the source list — at least one. Order matters: the
	// first element renders on top.
	Avatars []AvatarConfig

	// Max caps how many avatars render before the "+N" indicator
	// replaces the remainder. Default 5.
	Max int

	// Size propagates to each child Avatar unless the child has its
	// own Size set explicitly. Default AvatarMd.
	Size AvatarSize

	// Label is the aria-label on the group element. Default "Avatars".
	Label string

	// ShowNames wraps each Avatar in a Tooltip so hover / keyboard
	// focus reveals the avatar's Name. Useful for team-roster stacks
	// where the SR-only initials aren't enough for sighted users.
	ShowNames bool

	ID    string
	Class string
}

// AvatarGroup renders an overlapping stack of avatars. When
// len(Avatars) > Max, only the first Max render and a trailing
// "+N" pill announces the remainder via aria-label.
func AvatarGroup(cfg AvatarGroupConfig) render.HTML {
	if len(cfg.Avatars) == 0 {
		panic("ui: AvatarGroup requires at least one Avatar")
	}
	max := cfg.Max
	if max <= 0 {
		max = 5
	}
	label := cfg.Label
	if label == "" {
		label = "Avatars"
	}

	cls := "ui-avatar-group"
	if cfg.Size != AvatarMd {
		cls += " ui-avatar-group--" + string(cfg.Size)
	}
	if cfg.Class != "" {
		cls += " " + cfg.Class
	}

	visible := cfg.Avatars
	overflow := 0
	if len(visible) > max {
		visible = cfg.Avatars[:max]
		overflow = len(cfg.Avatars) - max
	}

	items := make([]render.HTML, 0, len(visible)+1)
	for _, a := range visible {
		if a.Size == AvatarMd && cfg.Size != AvatarMd {
			a.Size = cfg.Size
		}
		if a.Class == "" {
			a.Class = "ui-avatar-group__item"
		} else {
			a.Class = "ui-avatar-group__item " + a.Class
		}
		av := Avatar(a)
		if cfg.ShowNames {
			av = Tooltip(TooltipConfig{Text: a.Name}, av)
		}
		items = append(items, av)
	}
	if overflow > 0 {
		more := strconv.Itoa(overflow)
		items = append(items, html.Span(html.TextConfig{
			Class: "ui-avatar-group__overflow",
			Attrs: html.Attrs{"aria-label": more + " more"},
		},
			html.Span(html.TextConfig{
				Attrs: html.Attrs{"aria-hidden": "true"},
			}, render.Text("+"+more)),
		))
	}

	return avatarGroupStyle.WrapHTML(html.Div(html.DivConfig{
		Class:     cls,
		ID:        cfg.ID,
		Role:      "group",
		AriaLabel: label,
	}, items...))
}

var avatarGroupStyle = registry.RegisterStyle("ui-avatar-group", avatarGroupCSS)

func avatarGroupCSS(_ style.Theme) string {
	// Overlap is sized per-variant so the stack looks tight regardless
	// of Avatar size — roughly 40% of each avatar's width tucks under
	// the previous one. Stacking order (z-index via :nth-child reverse)
	// keeps the first avatar on top, which matches the natural reading
	// order ("Ada, then Grace, then …").
	return `[data-fui-comp="ui-avatar-group"] {
  display: inline-flex;
  align-items: center;
  flex-direction: row;
  isolation: isolate;
}
[data-fui-comp="ui-avatar-group"] > *:not(:first-child) {
  margin-inline-start: -1rem; /* default md: ~40% overlap on 2.5rem avatars */
}
[data-fui-comp="ui-avatar-group"].ui-avatar-group--sm > *:not(:first-child) {
  margin-inline-start: -0.6rem;
}
[data-fui-comp="ui-avatar-group"].ui-avatar-group--lg > *:not(:first-child) {
  margin-inline-start: -1.25rem;
}
[data-fui-comp="ui-avatar-group"].ui-avatar-group--xl > *:not(:first-child) {
  margin-inline-start: -1.6rem;
}
/* Reverse z-index so earlier siblings sit on top of later ones — the
   first avatar is the most prominent. */
[data-fui-comp="ui-avatar-group"] > :nth-child(1) { z-index: 6; }
[data-fui-comp="ui-avatar-group"] > :nth-child(2) { z-index: 5; }
[data-fui-comp="ui-avatar-group"] > :nth-child(3) { z-index: 4; }
[data-fui-comp="ui-avatar-group"] > :nth-child(4) { z-index: 3; }
[data-fui-comp="ui-avatar-group"] > :nth-child(5) { z-index: 2; }
[data-fui-comp="ui-avatar-group"] > :nth-child(6) { z-index: 1; }
[data-fui-comp="ui-avatar-group"] > *:hover,
[data-fui-comp="ui-avatar-group"] > *:focus-within {
  z-index: 10; /* surface the focused/hovered chip above siblings */
}
[data-fui-comp="ui-avatar-group"] .ui-avatar {
  border: 2px solid var(--color-surface, #fff);
  box-sizing: content-box;
}
[data-fui-comp="ui-avatar-group"] .ui-avatar-group__overflow {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  inline-size: 2.5rem;
  block-size: 2.5rem;
  border-radius: 9999px;
  background: var(--color-muted, #e5e5e5);
  color: var(--color-text, #111);
  font-size: 0.8rem;
  font-weight: 600;
  border: 2px solid var(--color-surface, #fff);
}
[data-fui-comp="ui-avatar-group"].ui-avatar-group--sm .ui-avatar-group__overflow {
  inline-size: 1.5rem; block-size: 1.5rem; font-size: 0.65rem;
}
[data-fui-comp="ui-avatar-group"].ui-avatar-group--lg .ui-avatar-group__overflow {
  inline-size: 3rem; block-size: 3rem; font-size: 0.9rem;
}
[data-fui-comp="ui-avatar-group"].ui-avatar-group--xl .ui-avatar-group__overflow {
  inline-size: 4rem; block-size: 4rem; font-size: 1rem;
}
`
}
