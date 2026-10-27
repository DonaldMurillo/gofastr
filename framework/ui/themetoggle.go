package ui

import (
	"context"
	"strings"

	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework/headless"
	"github.com/DonaldMurillo/gofastr/framework/i18nui"
)

// ─── ThemeToggle ────────────────────────────────────────────────────

// ThemeToggleVariant selects the visual variant of the toggle button.
type ThemeToggleVariant string

const (
	// ThemeToggleIcon renders a sun/moon icon button.
	ThemeToggleIcon ThemeToggleVariant = "icon"
	// ThemeToggleLabel renders a text button ("Light" / "Dark").
	ThemeToggleLabel ThemeToggleVariant = "label"
	// ThemeTogglePill renders a segmented pill with light/auto/dark.
	ThemeTogglePill ThemeToggleVariant = "pill"
)

// ThemeToggleConfig configures a dark/light mode toggle button.
//
// The toggle writes to localStorage["gofastr.colorScheme"] via the
// existing colorscheme.js bootstrap, which applies the change
// immediately. No page reload needed.
//
// The component emits the data-hui-theme-toggle / -option / -cycle
// hooks so the headless-navigation module can attach the click logic
// via event delegation.
type ThemeToggleConfig struct {
	// Variant selects the visual style.
	// Defaults to ThemeToggleIcon when empty.
	Variant ThemeToggleVariant
	// Fill stretches the pill across its container, the options sharing
	// the width (a panel's full-width switch). Pill only.
	Fill bool

	// ID is an optional id for the root element.
	ID string

	// Class is an optional extra CSS class.
	Class string

	// ExtraAttrs forwards additional attributes (data-* test hooks,
	// analytics markers) to the toggle's root element, whichever
	// shape it takes (<button> for icon/label, <div> for pill). Keys
	// the component owns are dropped: class and id (use Class / ID),
	// data-cui-* (the colorscheme runtime wiring), type, role, and
	// aria-label (i18n-resolved).
	ExtraAttrs html.Attrs

	// LightLabel overrides the light-mode label for label/pill variants.
	// Defaults to "Light".
	LightLabel string

	// DarkLabel overrides the dark-mode label for label/pill variants.
	// Defaults to "Dark".
	DarkLabel string

	// AutoLabel overrides the auto-mode label for pill variant.
	// Defaults to "Auto".
	AutoLabel string
	// Ctx carries the per-request context used to resolve the light/dark/
	// auto labels and aria-labels. When nil, English fallbacks apply.
	Ctx context.Context
}

// ThemeToggle renders a dark/light color scheme toggle button.
//
// On click, the button cycles through dark → light → auto, skipping a
// step that would not change what the page shows (on a dark OS, auto
// goes straight to light), and writes the choice to localStorage. The
// colorscheme.js bootstrap script
// picks up the change and swaps data-color-scheme on <html> so all
// theme tokens update immediately.
func ThemeToggle(cfg ThemeToggleConfig) render.HTML {
	variant := cfg.Variant
	if variant == "" {
		variant = ThemeToggleIcon
	}
	cls := ""

	switch variant {
	case ThemeTogglePill:
		return themeToggleStyle.WrapHTML(renderThemeTogglePill(cfg, cls))
	default:
		// icon and label both render a single button
		return themeToggleStyle.WrapHTML(renderThemeToggleButton(cfg, cls, variant))
	}
}

func renderThemeToggleButton(cfg ThemeToggleConfig, cls string, variant ThemeToggleVariant) render.HTML {
	ctx := cfg.Ctx
	if ctx == nil {
		ctx = context.Background()
	}
	attrs := html.SafeExtraAttrs(cfg.ExtraAttrs, "type", "role", "aria-label")
	if attrs == nil {
		attrs = map[string]string{}
	}
	attrs["type"] = "button"
	// The loader marker: headless-navigation's module is demand-loaded
	// by the [data-hui-theme-toggle] scan, and the click logic lives
	// in that module. Without the marker on the button variants' own
	// root, a page whose only theme control is an icon toggle never
	// loads the module and the button does nothing.
	attrs["data-hui-theme-toggle"] = ""
	attrs["data-hui-theme-cycle"] = ""
	attrs["aria-label"] = i18nui.ThemeToggle.Text(ctx)
	if cfg.ID != "" {
		attrs["id"] = cfg.ID
	}

	switch variant {
	case ThemeToggleIcon:
		attrs["class"] = strings.TrimSpace("fui-theme-toggle fui-theme-toggle--icon " + cfg.Class)
		return render.Tag("button", attrs,
			headless.Own(Icon("sun", IconConfig{Size: "18", Class: "fui-theme-toggle__sun"})),
			headless.Own(Icon("moon", IconConfig{Size: "18", Class: "fui-theme-toggle__moon"})),
		)
	default: // label
		lightLabel := cfg.LightLabel
		if lightLabel == "" {
			lightLabel = i18nui.ThemeLight.Text(ctx)
		}
		darkLabel := cfg.DarkLabel
		if darkLabel == "" {
			darkLabel = i18nui.ThemeDark.Text(ctx)
		}
		attrs["class"] = strings.TrimSpace("fui-theme-toggle fui-theme-toggle--label " + cfg.Class)
		return render.Tag("button", attrs,
			render.Tag("span", map[string]string{"class": "fui-theme-toggle__light"}, render.Text(lightLabel)),
			render.Tag("span", map[string]string{"class": "fui-theme-toggle__dark"}, render.Text(darkLabel)),
		)
	}
}

func renderThemeTogglePill(cfg ThemeToggleConfig, cls string) render.HTML {
	ctx := cfg.Ctx
	if ctx == nil {
		ctx = context.Background()
	}
	lightLabel := cfg.LightLabel
	if lightLabel == "" {
		lightLabel = i18nui.ThemeLight.Text(ctx)
	}
	darkLabel := cfg.DarkLabel
	if darkLabel == "" {
		darkLabel = i18nui.ThemeDark.Text(ctx)
	}
	autoLabel := cfg.AutoLabel
	if autoLabel == "" {
		autoLabel = i18nui.ThemeAuto.Text(ctx)
	}

	rootAttrs := html.SafeExtraAttrs(cfg.ExtraAttrs, "type", "role", "aria-label")
	if rootAttrs == nil {
		rootAttrs = map[string]string{}
	}
	rootAttrs["class"] = "fui-theme-toggle fui-theme-toggle--pill " + modifierClass("fui-theme-toggle--fill", cfg.Fill) + " " + cfg.Class
	rootAttrs["data-hui-theme-toggle"] = ""
	rootAttrs["role"] = "radiogroup"
	rootAttrs["aria-label"] = i18nui.ThemeColorScheme.Text(ctx)
	if cfg.ID != "" {
		rootAttrs["id"] = cfg.ID
	}

	// Auto ships checked and is the one Tab stop, the first-visit
	// state; the runtime re-checks from the stored scheme.
	optBtn := func(label, opt string) render.HTML {
		checked, tab := "false", "-1"
		if opt == "auto" {
			checked, tab = "true", "0"
		}
		return render.Tag("button", map[string]string{
			"type":                  "button",
			"class":                 "fui-theme-toggle__option",
			"data-cui-internal":     "",
			"data-hui-theme-option": opt,
			"aria-checked":          checked,
			"tabindex":              tab,
			"role":                  "radio",
		}, render.Text(label))
	}

	return render.Tag("div", rootAttrs,
		optBtn(lightLabel, "light"),
		optBtn(autoLabel, "auto"),
		optBtn(darkLabel, "dark"),
	)
}
