package ui

import (
	"context"

	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core-ui/registry"
	"github.com/DonaldMurillo/gofastr/core-ui/style"
	"github.com/DonaldMurillo/gofastr/core/i18n"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework/headless"
	"github.com/DonaldMurillo/gofastr/framework/i18nui"
)

// AvatarGroupConfig configures an avatar group / stack.
type AvatarGroupConfig struct {
	// Avatars is the source list (at least one); the first renders on top.
	Avatars []AvatarConfig
	// Max caps how many avatars render before "+N". Default 5.
	Max int
	// Size applies to each child Avatar that leaves its own Size unset.
	Size AvatarSize
	// Label is the group's aria-label. Default "Avatars", in Ctx's language.
	Label string
	// Ctx is the request context the label and the overflow count are said in.
	Ctx context.Context
	// ShowNames wraps each Avatar in a Tooltip showing its Name.
	ShowNames bool
	ID        string
	Class     string
	// ExtraAttrs lands on the root; class, id, data-cui-*, role and aria-label are dropped.
	ExtraAttrs html.Attrs
}

// AvatarGroup renders an overlapping stack of avatars, the first Max of
// them and a "+N" pill for the rest. It panics with no Avatars.
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
		label = i18nui.AvatarGroupLabel.Text(cfg.Ctx)
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
		if a.Ctx == nil {
			a.Ctx = cfg.Ctx
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
		items = append(items, headless.Own(av))
	}
	if overflow > 0 {
		items = append(items, html.Span(html.TextConfig{
			Class: "fui-avatar-group__overflow",
			ExtraAttrs: html.Attrs{
				"role":       "img",
				"aria-label": i18nui.AvatarGroupMore.Text(cfg.Ctx, map[string]any{"count": overflow}),
			},
		},
			html.Span(html.TextConfig{
				ExtraAttrs: html.Attrs{"aria-hidden": "true"},
			}, render.Text("+"+i18n.Number(cfg.Ctx, overflow))),
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
  background: var(--color-surface-soft, #F4F4F5);
  color: var(--color-text, #09090B);
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
