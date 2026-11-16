package ui

import (
	"context"
	"strings"

	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework/headless"
	"github.com/DonaldMurillo/gofastr/framework/i18nui"
)

// ThemeToggleVariant selects the visual variant of the toggle button.
type ThemeToggleVariant string

const (
	ThemeToggleIcon  ThemeToggleVariant = "icon"  // a sun/moon icon button
	ThemeToggleLabel ThemeToggleVariant = "label" // a "Light" / "Dark" text button
	ThemeTogglePill  ThemeToggleVariant = "pill"  // a light/auto/dark segmented pill
)

// ThemeToggleConfig configures a dark/light mode toggle, which stores
// the choice in localStorage["gofastr.colorScheme"].
type ThemeToggleConfig struct {
	// Variant selects the visual style. Default ThemeToggleIcon.
	Variant ThemeToggleVariant
	// Fill stretches the pill across its container. Pill only.
	Fill  bool
	ID    string
	Class string
	// ExtraAttrs lands on the root; class, id, data-cui-*, type, role and aria-label are dropped.
	ExtraAttrs html.Attrs
	// LightLabel overrides the light-mode label. Default "Light".
	LightLabel string
	// DarkLabel overrides the dark-mode label. Default "Dark".
	DarkLabel string
	// AutoLabel overrides the pill's auto-mode label. Default "Auto".
	AutoLabel string
	// Ctx is the request context the labels are said in.
	Ctx context.Context
}

// ThemeToggle renders a color scheme toggle. The button variants cycle
// dark, light and auto on click, skipping a step that would not change
// what the page shows; the pill offers all three.
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
	default:
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
