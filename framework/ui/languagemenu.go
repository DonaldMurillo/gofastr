package ui

import (
	"context"

	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core/i18n"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework/headless"
	"github.com/DonaldMurillo/gofastr/framework/i18nui"
)

// LanguageMenuConfig configures a LanguageMenu.
type LanguageMenuConfig struct {
	// Ctx is the request: its locale, locale set and page. Required.
	Ctx context.Context
	// Names overrides a locale's label by tag; the default is its own name.
	Names map[string]string

	// ID, Position and ExtraAttrs are the Menu's.
	ID         string
	Position   MenuPosition
	ExtraAttrs html.Attrs
}

// LanguageMenu renders a Menu linking this page in each path locale
// (framework.WithLocalePrefixes); without path locales it renders nothing.
func LanguageMenu(cfg LanguageMenuConfig) render.HTML {
	if cfg.Ctx == nil {
		return ""
	}
	set, current, ok := i18n.PathLocale(cfg.Ctx)
	if !ok {
		return ""
	}
	page := "/"
	if r := app.RequestFromContext(cfg.Ctx); r != nil {
		p := r.URL.EscapedPath()
		if tag, rest := set.Split(p); tag != "" {
			page = rest
		} else if p != "" {
			page = p
		}
		if r.URL.RawQuery != "" {
			page += "?" + r.URL.RawQuery
		}
	}
	name := func(tag string) string {
		if n := cfg.Names[tag]; n != "" {
			return n
		}
		return i18n.LanguageName(tag)
	}
	var items []MenuItem
	for _, tag := range set.All() {
		items = append(items, MenuItem{
			Label:   name(tag),
			Href:    set.Path(tag, page),
			Radio:   "language",
			Checked: tag == current,
			ExtraAttrs: map[string]string{
				"rel":      "alternate",
				"hreflang": tag,
				"lang":     tag,
			},
		})
	}
	trigger := headless.Own(render.Join(
		Icon("globe", IconConfig{Size: "16", ExtraAttrs: html.Attrs{"aria-hidden": "true"}}),
		html.Span(html.TextConfig{Class: "fui-visually-hidden"}, render.Text(i18nui.LanguageMenu.Text(cfg.Ctx))),
		html.Span(html.TextConfig{ExtraAttrs: html.Attrs{"lang": current}}, render.Text(name(current))),
	))
	return Menu(MenuConfig{
		ID:          cfg.ID,
		Label:       i18nui.LanguageMenu.Text(cfg.Ctx),
		TriggerHTML: trigger,
		Items:       items,
		Position:    cfg.Position,
		ExtraAttrs:  html.SafeExtraAttrs(cfg.ExtraAttrs),
	})
}
