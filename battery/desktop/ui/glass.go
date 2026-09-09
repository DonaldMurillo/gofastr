package desktopui

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
	// are dropped: class and data-fui-comp.
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
	return `[data-fui-comp="desktopui-glass"] {
  border-radius: var(--radii-lg, 12px);
  /* Translucent fill: the surface under the blur, not an opaque paint.
     measured, unverified. */
  background: color-mix(in srgb, var(--color-surface, #FFFFFF) 64%, transparent);
  -webkit-backdrop-filter: blur(20px) saturate(180%);
  backdrop-filter: blur(20px) saturate(180%);
  /* The 1px inset rim highlight, light from above. */
  box-shadow: inset 0 0 0 1px rgba(255, 255, 255, 0.16);
}
[data-fui-comp="desktopui-glass"].desktopui-glass--thick {
  border-radius: var(--radii-xl, 16px);
  /* Thicker material for sheets and popovers: better contrast for fine
     features (HIG Materials). measured, unverified. */
  background: color-mix(in srgb, var(--color-surface, #FFFFFF) 82%, transparent);
  -webkit-backdrop-filter: blur(40px) saturate(200%);
  backdrop-filter: blur(40px) saturate(200%);
  box-shadow: inset 0 0 0 1px rgba(255, 255, 255, 0.18),
    var(--shadow-lg, 0 10px 15px -3px rgba(0, 0, 0, 0.10), 0 4px 6px -2px rgba(0, 0, 0, 0.05));
}
/* Reduce Transparency: the shell pushes the state as a class because
   WebKit has no prefers-reduced-transparency query. Opaque Surface, no
   filter, the rim stays as the edge. */
html.desktop-reduce-transparency [data-fui-comp="desktopui-glass"],
html.desktop-reduce-transparency [data-fui-comp="desktopui-glass"].desktopui-glass--thick {
  background: var(--color-surface, #FFFFFF);
  -webkit-backdrop-filter: none;
  backdrop-filter: none;
}
/* Inactive window: the fill flattens toward opaque and the accent dims
   to TextMuted for the subtree, the same read a native material gives
   when its window resigns key. */
html.desktop-inactive [data-fui-comp="desktopui-glass"],
html.desktop-inactive [data-fui-comp="desktopui-glass"].desktopui-glass--thick {
  background: color-mix(in srgb, var(--color-surface, #FFFFFF) 88%, transparent);
  --color-accent: var(--color-text-muted, #52525B);
}
/* The shadow and the blur do not animate: HIG says avoid animating into
   and out of blurs, and there is no transition to suppress anyway. */
@media (prefers-reduced-motion: reduce) {
  [data-fui-comp="desktopui-glass"] { transition: none; }
}
`
}
