package ui

import (
	"context"
	"hash/fnv"
	"strconv"

	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core-ui/registry"
	"github.com/DonaldMurillo/gofastr/core-ui/style"
	"github.com/DonaldMurillo/gofastr/core/i18n"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework/headless"
	"github.com/DonaldMurillo/gofastr/framework/i18nui"
)

// DrawerBarConfig configures the bar atop an intercepted drawer (app.OverlayFromContext).
type DrawerBarConfig struct {
	// Path is the layer's URL path, shown in mono. Required.
	Path string
	// Prev and Next step to the neighbouring record; with one set, the other draws disabled.
	Prev, Next string
	// CopyURL adds a button that copies this absolute address.
	CopyURL string
	// PageURL adds a link that opens it as the full page.
	PageURL string
	// Actions trail the bar's own controls.
	Actions []render.HTML
	// Ctx is the labels' language; nil is English.
	Ctx context.Context
	// ExtraAttrs land on the root; class, id and data-cui-* are dropped.
	ExtraAttrs html.Attrs
}

// DrawerBar renders the bar.
func DrawerBar(cfg DrawerBarConfig) render.HTML {
	if cfg.Path == "" {
		panic("ui: DrawerBar requires Path")
	}
	ctx := cfg.Ctx
	if ctx == nil {
		ctx = context.Background()
	}
	kids := []render.HTML{
		Button(ButtonConfig{
			Ctx:        ctx,
			Label:      i18nui.DialogClose.Text(ctx),
			Icon:       "close",
			IconOnly:   true,
			Variant:    ButtonGhost,
			Size:       ButtonSizeSmall,
			ExtraAttrs: html.Attrs{"data-cui-intercept-close": ""},
		}),
		html.Code(html.TextConfig{Class: "fui-drawer-bar__path", ExtraAttrs: html.Attrs{"title": cfg.Path}}, render.Text(cfg.Path)),
	}
	if cfg.Prev != "" || cfg.Next != "" {
		kids = append(kids,
			drawerStep(ctx, cfg.Prev, "chevron-up", i18nui.DrawerPrev),
			drawerStep(ctx, cfg.Next, "chevron-down", i18nui.DrawerNext))
	}
	if cfg.CopyURL != "" {
		h := fnv.New32a()
		h.Write([]byte(cfg.Path))
		id := "fui-drawer-url-" + strconv.FormatUint(uint64(h.Sum32()), 36)
		kids = append(kids,
			html.Span(html.TextConfig{ID: id, Class: "fui-visually-hidden"}, render.Text(cfg.CopyURL)),
			CopyButton(CopyButtonConfig{
				Target:      id,
				IconOnly:    true,
				Icon:        "link",
				Ghost:       true,
				AriaLabel:   i18nui.CopyLink.Text(ctx),
				ToastOnCopy: true,
				ToastTitle:  i18nui.CopyCopied.Text(ctx),
				Ctx:         ctx,
			}),
		)
	}
	if cfg.PageURL != "" {
		kids = append(kids, LinkButton(LinkButtonConfig{
			Label:      i18nui.DrawerOpenPage.Text(ctx),
			Href:       cfg.PageURL,
			Icon:       "maximize",
			IconOnly:   true,
			Variant:    ButtonGhost,
			Size:       ButtonSizeSmall,
			ExtraAttrs: html.Attrs{"data-cui-intercept-page": ""},
		}))
	}
	kids = append([]render.HTML{headless.Own(render.Join(kids...))}, cfg.Actions...)
	return drawerBarStyle.WrapHTML(html.Div(html.DivConfig{Class: "fui-drawer-bar", ExtraAttrs: html.SafeExtraAttrs(cfg.ExtraAttrs)}, kids...))
}

func drawerStep(ctx context.Context, href, icon string, key i18n.Phrase) render.HTML {
	label := key.Text(ctx)
	if href == "" {
		return Button(ButtonConfig{Ctx: ctx, Label: label, Icon: icon, IconOnly: true, Variant: ButtonGhost, Size: ButtonSizeSmall, Disabled: true})
	}
	return LinkButton(LinkButtonConfig{
		Label:      label,
		Href:       href,
		Icon:       icon,
		IconOnly:   true,
		Variant:    ButtonGhost,
		Size:       ButtonSizeSmall,
		ExtraAttrs: html.Attrs{"data-cui-intercept-swap": ""},
	})
}

var drawerBarStyle = registry.RegisterStyle("ui-drawer-bar", drawerBarCSS)

func drawerBarCSS(_ style.Theme) string {
	return `[data-cui-comp="ui-drawer-bar"] {
  position: sticky;
  inset-block-start: calc(-1 * var(--cui-intercept-pad, 0px));
  z-index: 1;
  display: flex;
  align-items: center;
  gap: var(--spacing-xs, 2px);
  margin: calc(-1 * var(--cui-intercept-pad, 0px)) calc(-1 * var(--cui-intercept-pad, 0px)) var(--spacing-lg, 16px);
  padding: var(--spacing-sm, 4px) calc(var(--cui-intercept-pad, 0px) - var(--spacing-sm, 4px));
  border-block-end: var(--stroke-thin, 1px) solid var(--color-border, #e4e4e7);
  background: var(--color-surface, #fff);
}
[data-cui-comp="ui-drawer-bar"] .fui-drawer-bar__path {
  flex: 1 1 auto;
  min-inline-size: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  padding-inline: var(--spacing-sm, 4px);
  background: none;
  color: var(--color-text-muted, #52525b);
  font-family: var(--font-mono, ui-monospace, monospace);
  font-size: var(--text-xs, 0.75rem);
}
`
}
