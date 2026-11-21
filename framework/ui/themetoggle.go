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
	// ExtraAttrs lands on the root; class, id, data-cui-*, data-hui-theme-*, type, role and aria-label are dropped.
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
	extra := html.SafeExtraAttrs(cfg.ExtraAttrs, "type", "role", "aria-label")
	if variant == ThemeTogglePill {
		return renderThemeTogglePill(cfg, extra)
	}
	return themeToggleStyle.WrapHTML(renderThemeToggleButton(cfg, extra, variant))
}

func renderThemeToggleButton(cfg ThemeToggleConfig, extra html.Attrs, variant ThemeToggleVariant) render.HTML {
	ctx := cfg.Ctx
	if ctx == nil {
		ctx = context.Background()
	}
	attrs := themeControlAttrs(extra, cfg.ID, i18nui.ThemeToggle.Text(ctx), "data-hui-theme-toggle")
	attrs["type"] = "button"
	attrs["data-hui-theme-cycle"] = ""

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

func renderThemeTogglePill(cfg ThemeToggleConfig, extra html.Attrs) render.HTML {
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

	attrs := themeControlAttrs(extra, cfg.ID, i18nui.ThemeColorScheme.Text(ctx), "data-hui-theme-toggle")
	const hook = "data-hui-theme-option"
	return themeRadioGroup(attrs, cfg.Class, cfg.Fill,
		themeRadioOption(lightLabel, hook, "light", false),
		themeRadioOption(autoLabel, hook, "auto", true),
		themeRadioOption(darkLabel, hook, "dark", false),
	)
}

func themeControlAttrs(attrs html.Attrs, id, label, hook string) html.Attrs {
	if attrs == nil {
		attrs = html.Attrs{}
	}
	for k := range attrs {
		if strings.HasPrefix(strings.ToLower(k), "data-hui-theme-") {
			delete(attrs, k)
		}
	}
	attrs[hook] = ""
	attrs["aria-label"] = label
	if id != "" {
		attrs["id"] = id
	}
	return attrs
}

func themeRadioGroup(attrs html.Attrs, class string, fill bool, options ...render.HTML) render.HTML {
	cls := "fui-theme-toggle fui-theme-toggle--pill"
	if fill {
		cls += " fui-theme-toggle--fill"
	}
	attrs["class"] = strings.TrimSpace(cls + " " + class)
	attrs["role"] = "radiogroup"
	return themeToggleStyle.WrapHTML(render.Tag("div", attrs, options...))
}

func themeRadioOption(label, hook, value string, selected bool) render.HTML {
	checked, tab := "false", "-1"
	if selected {
		checked, tab = "true", "0"
	}
	return render.Tag("button", html.Attrs{
		"type":              "button",
		"class":             "fui-theme-toggle__option",
		"data-cui-internal": "",
		hook:                value,
		"aria-checked":      checked,
		"tabindex":          tab,
		"role":              "radio",
	}, render.Text(label))
}
