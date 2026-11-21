package ui

import (
	"context"

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

	extra := html.SafeExtraAttrs(cfg.ExtraAttrs, "role", "aria-label")
	rootAttrs := themeControlAttrs(extra, cfg.ID, i18nui.ThemePicker.Text(ctx), "data-hui-theme-picker")
	const hook = "data-hui-theme-pick"
	opts := make([]render.HTML, 0, len(cfg.Themes)+1)
	opts = append(opts, themeRadioOption(defaultLabel, hook, "", true))
	for _, c := range cfg.Themes {
		opts = append(opts, themeRadioOption(c.Label.Text(ctx), hook, c.Theme.Class(), false))
	}
	return themeRadioGroup(rootAttrs, cfg.Class, cfg.Fill, opts...)
}
