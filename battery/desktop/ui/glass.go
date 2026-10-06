package desktopui

//gofastr:allow-file(GOFASTR1808) the desktop shell renders under its own theme
// (theme.go, radii 4/8/12/16), so these radius fallbacks restate that scale, not
// style.DefaultTheme's 6/8/10/14 the rule judges them against.

import (
	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core-ui/registry"
	"github.com/DonaldMurillo/gofastr/core-ui/style"
	"github.com/DonaldMurillo/gofastr/core/render"
)

// GlassConfig configures a Glass surface.
type GlassConfig struct {
	// Thick selects the sheet and popover variant: a more opaque fill,
	// a stronger blur, and the XL corner radius. The default is the
	// thin chrome variant for toolbars and floating controls.
	Thick bool

	// Class appends to the surface's class list.
	Class string

	// ExtraAttrs forwards additional attributes (data-* test hooks,
	// ARIA overrides) to the surface element. Keys the component owns
	// are dropped: class and data-cui-comp.
	ExtraAttrs html.Attrs
}

// Glass is the translucent surface the desktop theme styles its chrome
// with: backdrop blur + saturation, a translucent fill, and a 1px inset
// rim highlight. It is the honest CSS glass the plan allows. Real
// refraction needs an SVG reference filter inside backdrop-filter,
// which WebKit does not implement (bug 245510) and Firefox refuses, so
// none of that ships here. The fill values are measured, unverified.
//
// The two <html> classes the desktop runtime module sets are baked in:
//
//   - desktop-reduce-transparency: the surface goes opaque Surface with
//     no filter (WebKit has no prefers-reduced-transparency query, so
//     the shell pushes the state as a class).
//   - desktop-inactive: the fill flattens and --color-accent dims to
//     TextMuted for the whole subtree, mirroring how a native material
//     dims when its window stops being key.
//
// Glass renders a plain <div>; wrap it or pass children. It carries no
// landmark or role of its own.
func Glass(cfg GlassConfig, children ...render.HTML) render.HTML {
	cls := "desktopui-glass"
	if cfg.Thick {
		cls += " desktopui-glass--thick"
	}
	if cfg.Class != "" {
		cls += " " + cfg.Class
	}
	attrs := html.Attrs{"class": cls}
	for k, v := range html.SafeExtraAttrs(cfg.ExtraAttrs, "class") {
		attrs[k] = v
	}
	return glassStyle.WrapHTML(render.Tag("div", attrs, children...))
}

var glassStyle = registry.RegisterStyle("desktopui-glass", glassCSS)

func glassCSS(_ style.Theme) string {
	return `[data-cui-comp="desktopui-glass"] {
  border-radius: var(--radii-lg, 12px);
  /* Translucent fill: the surface under the blur, not an opaque paint.
     measured, unverified. */
  background: color-mix(in srgb, var(--color-surface, #FFFFFF) 64%, transparent);
  -webkit-backdrop-filter: blur(20px) saturate(180%);
  backdrop-filter: blur(20px) saturate(180%);
  /* The 1px inset rim highlight, light from above; a hairline edge in
     the label color; and a soft drop shadow. Native glass floats over
     varied content, so the blur alone separates it. Over a flat page
     the blur has nothing to show, and without the edge and the shadow
     the surface disappears into the page (the 2026-09-22 captures: a
     floating toolbar read as a bare button, an inspector as loose
     rows). measured, unverified. */
  box-shadow: inset 0 0 0 var(--stroke-thin, 1px) rgba(255, 255, 255, 0.16),
    0 0 0 calc(var(--stroke-thin, 1px) / 2) color-mix(in srgb, var(--color-text, #000000) 14%, transparent),
    var(--shadow-md, 0 4px 6px -1px rgba(0, 0, 0, 0.10), 0 2px 4px -2px rgba(0, 0, 0, 0.06));
}
[data-cui-comp="desktopui-glass"].desktopui-glass--thick {
  border-radius: var(--radii-xl, 16px);
  /* Thicker material for sheets and popovers: better contrast for fine
     features (HIG Materials). measured, unverified. */
  background: color-mix(in srgb, var(--color-surface, #FFFFFF) 82%, transparent);
  -webkit-backdrop-filter: blur(40px) saturate(200%);
  backdrop-filter: blur(40px) saturate(200%);
  box-shadow: inset 0 0 0 var(--stroke-thin, 1px) rgba(255, 255, 255, 0.18),
    0 0 0 calc(var(--stroke-thin, 1px) / 2) color-mix(in srgb, var(--color-text, #000000) 14%, transparent),
    var(--shadow-lg, 0 10px 15px -3px rgba(0, 0, 0, 0.10), 0 4px 6px -2px rgba(0, 0, 0, 0.05));
}
/* Reduce Transparency: the shell pushes the state as a class because
   WebKit has no prefers-reduced-transparency query. Opaque Surface, no
   filter, the rim stays as the edge. */
html.desktop-reduce-transparency [data-cui-comp="desktopui-glass"],
html.desktop-reduce-transparency [data-cui-comp="desktopui-glass"].desktopui-glass--thick {
  background: var(--color-surface, #FFFFFF);
  -webkit-backdrop-filter: none;
  backdrop-filter: none;
}
/* Inactive window: the fill flattens toward opaque and the accent dims
   to TextMuted for the subtree, the same read a native material gives
   when its window resigns key. */
html.desktop-inactive [data-cui-comp="desktopui-glass"],
html.desktop-inactive [data-cui-comp="desktopui-glass"].desktopui-glass--thick {
  background: color-mix(in srgb, var(--color-surface, #FFFFFF) 88%, transparent);
  --color-accent: var(--color-text-muted, #52525B);
}
/* The shadow and the blur do not animate: HIG says avoid animating into
   and out of blurs, and there is no transition to suppress anyway. */
@media (prefers-reduced-motion: reduce) {
  [data-cui-comp="desktopui-glass"] { transition: none; }
}
`
}
