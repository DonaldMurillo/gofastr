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
	// Ctx is the request's context: it carries the locale the URL
	// names, the app's locale set and the page the reader is on.
	// Required; without it the menu renders nothing.
	Ctx context.Context

	// Names overrides a locale's label, keyed by catalog tag ("es").
	// The default is the language's own name (i18n.LanguageName).
	Names map[string]string

	// ID, Position and ExtraAttrs are the Menu's.
	ID         string
	Position   MenuPosition
	ExtraAttrs html.Attrs
}

// LanguageMenu renders the language switcher of an app that names the
// language in the URL (framework.WithLocalePrefixes): a Menu whose
// trigger shows a globe and the current language, with one row per
// catalog locale linking this page in that language. Each language is
// named in itself, so a reader finds theirs whatever the page is in.
//
// Each row is a rel="alternate" hreflang link, which the response
// rewriter leaves unprefixed, and the runtime points those links at
// the current page after every client-side navigation, so a switcher
// in persistent chrome follows the reader. Following one loads a new
// document: another locale is another document.
//
// Without path locales on Ctx it renders nothing, so an app can place
// it in shared chrome before it routes by locale.
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
		// The router strips the prefix before the screen renders; a
		// wrapper that runs before it sees the prefixed path. The path
		// stays escaped: a decoded %3F would start a query string.
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
