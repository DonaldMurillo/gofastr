package ui

import (
	"strconv"

	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core-ui/registry"
	"github.com/DonaldMurillo/gofastr/core-ui/style"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework/headless"
)

// ─── AvatarGroup ────────────────────────────────────────────────────
//
// Overlapping stack of avatars with a "+N" overflow indicator. The
// stack uses CSS negative margins (no inline styles, strict-CSP
// safe) and propagates the group Size to its children unless the
// individual AvatarConfig already set one.

// AvatarGroupConfig configures an avatar group / stack.
type AvatarGroupConfig struct {
	// Avatars is the source list: at least one. Order matters: the
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

	// ExtraAttrs forwards additional attributes (data-* test hooks,
	// analytics markers, ARIA overrides) to the group's root element.
	// Keys the component owns are dropped: class and id (use Class /
	// ID), data-cui-*, role, and aria-label (use Label).
	ExtraAttrs html.Attrs
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

	cls := "fui-avatar-group"
	if cfg.Size != AvatarMd {
		cls += " fui-avatar-group--" + string(cfg.Size)
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
			a.Class = "fui-avatar-group__item"
		} else {
			a.Class = "fui-avatar-group__item " + a.Class
		}
		av := Avatar(a)
		if cfg.ShowNames {
			av = Tooltip(TooltipConfig{Text: a.Name}, av)
		}
		// AvatarConfig draws from strings only, so the rendered Avatar
		// (and the Tooltip wrapping it, when ShowNames is set) holds
		// none of a caller's markup here; its root becomes a nested
		// item rather than one an owner could place, so it is
		// collapsed into a single mark, as combobox/fileupload do for
		// their own nested headless calls.
		items = append(items, headless.Own(av))
	}
	if overflow > 0 {
		more := strconv.Itoa(overflow)
		// role=img + aria-label is the canonical way to attach an
		// accessible name to a graphical element. axe rejects bare
		// aria-label on <span> (the implicit role doesn't accept it);
		// either drop aria-label OR promote the span to a role that
		// supports the attribute. role=img reads "image, N more" to
		// screen readers, close enough to the visual "+N" chip.
		items = append(items, html.Span(html.TextConfig{
			Class: "fui-avatar-group__overflow",
			ExtraAttrs: html.Attrs{
				"role":       "img",
				"aria-label": more + " more",
			},
		},
			html.Span(html.TextConfig{
				ExtraAttrs: html.Attrs{"aria-hidden": "true"},
			}, render.Text("+"+more)),
		))
	}

	return avatarGroupStyle.WrapHTML(html.Div(html.DivConfig{
		Class:      cls,
		ID:         cfg.ID,
		Role:       "group",
		AriaLabel:  label,
		ExtraAttrs: html.SafeExtraAttrs(cfg.ExtraAttrs, "role", "aria-label"),
	}, items...))
}

var avatarGroupStyle = registry.RegisterStyle("ui-avatar-group", avatarGroupCSS)

func avatarGroupCSS(_ style.Theme) string {
	// Overlap is sized per-variant so the stack looks tight regardless
	// of Avatar size: about a quarter of each avatar's width (ring
	// included) tucks under the previous one, the shadcn spacing. Less
	// read as a row of loose circles rather than a stack. Stacking order (z-index via :nth-child reverse)
	// keeps the first avatar on top, which matches the natural reading
	// order ("Ada, then Grace, then …").
	//
	// Knobs: the group reads the avatar component's size knobs
	// (--ui-avatar-size-sm/md/lg/xl, declared with the avatar sheet)
	// so the overlap (25% of the size) and the "+N" overflow pill
	// track the avatars they sit beside.
	return `[data-cui-comp="ui-avatar-group"] {
  display: inline-flex;
  align-items: center;
  flex-direction: row;
  isolation: isolate;
}
[data-cui-comp="ui-avatar-group"] > *:not(:first-child) {
  margin-inline-start: calc(var(--ui-avatar-size, 2.5rem) * -0.25); /* default md: ~25% of a 2.5rem avatar and its ring */
}
[data-cui-comp="ui-avatar-group"].fui-avatar-group--sm > *:not(:first-child) {
  margin-inline-start: calc(var(--ui-avatar-size-sm, 1.5rem) * -0.25);
}
[data-cui-comp="ui-avatar-group"].fui-avatar-group--lg > *:not(:first-child) {
  margin-inline-start: calc(var(--ui-avatar-size-lg, 3rem) * -0.25);
}
[data-cui-comp="ui-avatar-group"].fui-avatar-group--xl > *:not(:first-child) {
  margin-inline-start: calc(var(--ui-avatar-size-xl, 4rem) * -0.25);
}
/* Reverse z-index so earlier siblings sit on top of later ones — the
   first avatar is the most prominent. */
[data-cui-comp="ui-avatar-group"] > :nth-child(1) { z-index: 6; }
[data-cui-comp="ui-avatar-group"] > :nth-child(2) { z-index: 5; }
[data-cui-comp="ui-avatar-group"] > :nth-child(3) { z-index: 4; }
[data-cui-comp="ui-avatar-group"] > :nth-child(4) { z-index: 3; }
[data-cui-comp="ui-avatar-group"] > :nth-child(5) { z-index: 2; }
[data-cui-comp="ui-avatar-group"] > :nth-child(6) { z-index: 1; }
[data-cui-comp="ui-avatar-group"] > *:hover,
[data-cui-comp="ui-avatar-group"] > *:focus-within {
  z-index: 10; /* surface the focused/hovered chip above siblings */
}
[data-cui-comp="ui-avatar-group"] .fui-avatar {
  border: var(--stroke-thick, 2px) solid var(--color-surface, #fff);
  box-sizing: content-box;
}
[data-cui-comp="ui-avatar-group"] .fui-avatar-group__overflow {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  inline-size: var(--ui-avatar-size, 2.5rem);
  block-size: var(--ui-avatar-size, 2.5rem);
  border-radius: var(--radii-full, 9999px);
  background: var(--color-surface-soft, #e5e5e5);
  color: var(--color-text, #111);
  font-size: var(--text-xs, 0.75rem);
  font-weight: var(--font-weight-semibold);
  line-height: 1;
  border: var(--stroke-thick, 2px) solid var(--color-surface, #fff);
}
[data-cui-comp="ui-avatar-group"].fui-avatar-group--sm .fui-avatar-group__overflow {
  inline-size: var(--ui-avatar-size-sm, 1.5rem); block-size: var(--ui-avatar-size-sm, 1.5rem); font-size: calc(var(--text-xs, 0.75rem) * 0.867);
}
[data-cui-comp="ui-avatar-group"].fui-avatar-group--lg .fui-avatar-group__overflow {
  inline-size: var(--ui-avatar-size-lg, 3rem); block-size: var(--ui-avatar-size-lg, 3rem); font-size: var(--text-sm, 0.875rem);
}
[data-cui-comp="ui-avatar-group"].fui-avatar-group--xl .fui-avatar-group__overflow {
  inline-size: var(--ui-avatar-size-xl, 4rem); block-size: var(--ui-avatar-size-xl, 4rem); font-size: var(--text-base, 1rem);
}
`
}
