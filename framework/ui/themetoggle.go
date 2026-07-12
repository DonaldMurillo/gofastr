package ui

import (
	"context"

	"github.com/DonaldMurillo/gofastr/core/render"
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
// The component emits data-fui-theme-toggle so a small runtime
// module can attach the click logic via event delegation.
type ThemeToggleConfig struct {
	// Variant selects the visual style.
	// Defaults to ThemeToggleIcon when empty.
	Variant ThemeToggleVariant

	// ID is an optional id for the root element.
	ID string

	// Class is an optional extra CSS class.
	Class string

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
// On click, the button cycles through dark → light → auto and writes
// the choice to localStorage. The colorscheme.js bootstrap script
// picks up the change and swaps data-color-scheme on <html> so all
// theme tokens update immediately.
func ThemeToggle(cfg ThemeToggleConfig) render.HTML {
	variant := cfg.Variant
	if variant == "" {
		variant = ThemeToggleIcon
	}
	cls := "ui-theme-toggle"
	if cfg.Class != "" {
		cls += " " + cfg.Class
	}

	switch variant {
	case ThemeTogglePill:
		return themeToggleStyle.WrapHTML(renderThemeTogglePill(cfg, cls))
	default:
		// icon and label both render a single button
		return themeToggleStyle.WrapHTML(renderThemeToggleButton(cfg, cls, variant))
	}
}

// sunSVG is the sun icon shown when in dark mode (click → switch to light).
const sunSVG = `<svg class="ui-theme-toggle__sun" xmlns="http://www.w3.org/2000/svg" width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><circle cx="12" cy="12" r="5"/><line x1="12" y1="1" x2="12" y2="3"/><line x1="12" y1="21" x2="12" y2="23"/><line x1="4.22" y1="4.22" x2="5.64" y2="5.64"/><line x1="18.36" y1="18.36" x2="19.78" y2="19.78"/><line x1="1" y1="12" x2="3" y2="12"/><line x1="21" y1="12" x2="23" y2="12"/><line x1="4.22" y1="19.78" x2="5.64" y2="18.36"/><line x1="18.36" y1="5.64" x2="19.78" y2="4.22"/></svg>`

// moonSVG is the moon icon shown when in light mode (click → switch to dark).
const moonSVG = `<svg class="ui-theme-toggle__moon" xmlns="http://www.w3.org/2000/svg" width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M21 12.79A9 9 0 1 1 11.21 3 7 7 0 0 0 21 12.79z"/></svg>`

func renderThemeToggleButton(cfg ThemeToggleConfig, cls string, variant ThemeToggleVariant) render.HTML {
	ctx := cfg.Ctx
	if ctx == nil {
		ctx = context.Background()
	}
	attrs := map[string]string{
		"type":                  "button",
		"data-fui-theme-toggle": "",
		"aria-label":            i18nui.T(ctx, i18nui.KeyThemeToggle),
	}
	if cfg.ID != "" {
		attrs["id"] = cfg.ID
	}

	switch variant {
	case ThemeToggleIcon:
		attrs["class"] = cls + " ui-theme-toggle--icon"
		return render.Tag("button", attrs,
			render.Raw(sunSVG),
			render.Raw(moonSVG),
		)
	default: // label
		lightLabel := cfg.LightLabel
		if lightLabel == "" {
			lightLabel = i18nui.T(ctx, i18nui.KeyThemeLight)
		}
		darkLabel := cfg.DarkLabel
		if darkLabel == "" {
			darkLabel = i18nui.T(ctx, i18nui.KeyThemeDark)
		}
		attrs["class"] = cls + " ui-theme-toggle--label"
		return render.Tag("button", attrs,
			render.Tag("span", map[string]string{"class": "ui-theme-toggle__light"}, render.Text(lightLabel)),
			render.Tag("span", map[string]string{"class": "ui-theme-toggle__dark"}, render.Text(darkLabel)),
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
		lightLabel = i18nui.T(ctx, i18nui.KeyThemeLight)
	}
	darkLabel := cfg.DarkLabel
	if darkLabel == "" {
		darkLabel = i18nui.T(ctx, i18nui.KeyThemeDark)
	}
	autoLabel := cfg.AutoLabel
	if autoLabel == "" {
		autoLabel = i18nui.T(ctx, i18nui.KeyThemeAuto)
	}

	rootAttrs := map[string]string{
		"class":                 cls + " ui-theme-toggle--pill",
		"data-fui-theme-toggle": "pill",
		"role":                  "radiogroup",
		"aria-label":            i18nui.T(ctx, i18nui.KeyThemeColorScheme),
	}
	if cfg.ID != "" {
		rootAttrs["id"] = cfg.ID
	}

	optBtn := func(label, opt string) render.HTML {
		return render.Tag("button", map[string]string{
			"type":                      "button",
			"class":                     "ui-theme-toggle__opt",
			"data-fui-theme-toggle-opt": opt,
			"aria-checked":              "false",
			"role":                      "radio",
		}, render.Text(label))
	}

	return render.Tag("div", rootAttrs,
		optBtn(lightLabel, "light"),
		optBtn(autoLabel, "auto"),
		optBtn(darkLabel, "dark"),
	)
}
