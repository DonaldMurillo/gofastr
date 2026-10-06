package ui

import (
	"context"
	"strings"

	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core-ui/style"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework/i18nui"
)

// ─── ThemePicker ────────────────────────────────────────────────────

// ThemeChoice is one theme a ThemePicker offers: the option's label
// and the override registered with style.RegisterThemeOverride.
type ThemeChoice struct {
	Label string
	Theme style.ThemeRef
}

// ThemePickerConfig configures a page-theme picker.
type ThemePickerConfig struct {
	// Themes are the registered overrides offered beside the default,
	// in display order.
	Themes []ThemeChoice

	// DefaultLabel names the option that shows the app's own theme.
	// Defaults to "Default".
	DefaultLabel string

	// ID is an optional id for the root element.
	ID string

	// Class is an optional extra CSS class.
	Class string

	// ExtraAttrs forwards additional attributes (data-* test hooks,
	// analytics markers) to the root. Keys the component owns are
	// dropped: class and id (use Class / ID), data-cui-*, the
	// data-hui-theme-* hooks, role, and aria-label (i18n-resolved).
	ExtraAttrs html.Attrs

	// Ctx carries the per-request context used to resolve the labels.
	// When nil, English fallbacks apply.
	Ctx context.Context
}

// ThemePicker renders a segmented choice between the app's theme and
// registered overrides, drawn as ThemeToggle's pill. Picking an option
// puts that override's class (style.ThemeRef.Class) on <html>, so the
// whole page — body background, component options and knobs included —
// draws in it; Default removes the class. The choice persists in
// localStorage["gofastr.theme"], and the colour-scheme bootstrap
// re-applies it before first paint on the next load.
//
// It composes with ThemeToggle: the override's DarkColors follow the
// document's scheme, and an override without them stays light.
//
//	var Brutal = style.RegisterThemeOverride(brutalTheme)
//
//	ui.ThemePicker(ui.ThemePickerConfig{
//	    Themes: []ui.ThemeChoice{{Label: "Brutal", Theme: Brutal}},
//	})
func ThemePicker(cfg ThemePickerConfig) render.HTML {
	ctx := cfg.Ctx
	if ctx == nil {
		ctx = context.Background()
	}
	defaultLabel := cfg.DefaultLabel
	if defaultLabel == "" {
		defaultLabel = i18nui.T(ctx, i18nui.KeyThemeDefault)
	}

	rootAttrs := html.SafeExtraAttrs(cfg.ExtraAttrs, "role", "aria-label")
	if rootAttrs == nil {
		rootAttrs = map[string]string{}
	}
	for k := range rootAttrs {
		if strings.HasPrefix(strings.ToLower(k), "data-hui-theme-") {
			delete(rootAttrs, k)
		}
	}
	rootAttrs["class"] = strings.TrimSpace("fui-theme-toggle fui-theme-toggle--pill " + cfg.Class)
	rootAttrs["data-hui-theme-picker"] = ""
	rootAttrs["role"] = "radiogroup"
	rootAttrs["aria-label"] = i18nui.T(ctx, i18nui.KeyThemePicker)
	if cfg.ID != "" {
		rootAttrs["id"] = cfg.ID
	}

	// Default ships checked and is the one Tab stop, the first-visit
	// state; the stored choice lives in the browser, and
	// headless-navigation's arrival pass re-checks from it.
	option := func(label, class string) render.HTML {
		checked, tab := "false", "-1"
		if class == "" {
			checked, tab = "true", "0"
		}
		return render.Tag("button", map[string]string{
			"type":                "button",
			"class":               "fui-theme-toggle__option",
			"data-cui-internal":   "",
			"data-hui-theme-pick": class,
			"aria-checked":        checked,
			"tabindex":            tab,
			"role":                "radio",
		}, render.Text(label))
	}
	opts := make([]render.HTML, 0, len(cfg.Themes)+1)
	opts = append(opts, option(defaultLabel, ""))
	for _, c := range cfg.Themes {
		opts = append(opts, option(c.Label, c.Theme.Class()))
	}
	return themeToggleStyle.WrapHTML(render.Tag("div", rootAttrs, opts...))
}
