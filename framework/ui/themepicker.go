package ui

import (
	"context"
	"strings"

	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core-ui/style"
	"github.com/DonaldMurillo/gofastr/core/i18n"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework/i18nui"
)

// ThemeChoice is one theme a ThemePicker offers: a label phrase and an
// override registered with style.RegisterThemeOverride.
type ThemeChoice struct {
	Label i18n.Phrase
	Theme style.ThemeRef
}

// ThemePickerConfig configures a page-theme picker.
type ThemePickerConfig struct {
	// Themes are the overrides offered beside the default, in display order.
	Themes []ThemeChoice
	// DefaultLabel names the app's own theme. Default "Default".
	DefaultLabel string
	// Fill stretches the pill across its container.
	Fill  bool
	ID    string
	Class string
	// ExtraAttrs lands on the root; class, id, data-cui-*, data-hui-theme-*, role and aria-label are dropped.
	ExtraAttrs html.Attrs
	// Ctx is the request context the labels are said in.
	Ctx context.Context
}

// ThemePicker renders a segmented choice between the app's theme and
// registered overrides, drawn as ThemeToggle's pill. Picking one puts
// its ThemeRef.Class on <html> and stores it in
// localStorage["gofastr.theme"]; Default removes it.
func ThemePicker(cfg ThemePickerConfig) render.HTML {
	ctx := cfg.Ctx
	if ctx == nil {
		ctx = context.Background()
	}
	defaultLabel := cfg.DefaultLabel
	if defaultLabel == "" {
		defaultLabel = i18nui.ThemeDefault.Text(ctx)
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
	rootAttrs["class"] = strings.TrimSpace("fui-theme-toggle fui-theme-toggle--pill " + modifierClass("fui-theme-toggle--fill", cfg.Fill) + " " + cfg.Class)
	rootAttrs["data-hui-theme-picker"] = ""
	rootAttrs["role"] = "radiogroup"
	rootAttrs["aria-label"] = i18nui.ThemePicker.Text(ctx)
	if cfg.ID != "" {
		rootAttrs["id"] = cfg.ID
	}

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
		opts = append(opts, option(c.Label.Text(ctx), c.Theme.Class()))
	}
	return themeToggleStyle.WrapHTML(render.Tag("div", rootAttrs, opts...))
}
