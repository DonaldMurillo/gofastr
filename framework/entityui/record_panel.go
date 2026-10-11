package entityui

import (
	"context"

	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework/i18nui"
	"github.com/DonaldMurillo/gofastr/framework/ui"
)

// Panel turns on the full page's way back to the drawer. Drawn as its
// own page (a direct load, or the drawer's Open as page), the record's
// header offers Copy link and Open in panel: the second returns to the
// list at the builder's Base with this record open as a drawer over it
// (data-cui-intercept-panel). Turn it on only when the record's route
// opens as a drawer over Base (app.InterceptFrom); over any other page
// the runtime treats the link as a plain move to the list. The drawer,
// which has its own bar, draws neither. Off by default.
func (b *RecordBuilder) Panel() *RecordBuilder { b.panel = true; return b }

// panelPage reports whether the header draws the page tools: Panel
// on, a Base to return to, and the record drawn as its own page.
func (b *RecordBuilder) panelPage(ctx context.Context) bool {
	return b.panel && b.base != "" && !inDrawer(ctx)
}

// pageTools are the full page's Copy link and Open in panel, or none
// when panelPage is false. Copy link moves here out of the record's
// menu, the way the drawer's bar takes it.
func (b *RecordBuilder) pageTools(ctx context.Context, base string) []render.HTML {
	if !b.panelPage(ctx) {
		return nil
	}
	var out []render.HTML
	if span, items := b.copyLink(ctx); len(items) > 0 {
		out = append(out, span, ui.CopyButton(ui.CopyButtonConfig{
			Target:      items[0].Copy.Target,
			Label:       i18nui.T(ctx, i18nui.KeyCopyLink),
			Icon:        "link",
			ToastOnCopy: true,
			ToastTitle:  i18nui.T(ctx, i18nui.KeyCopyCopied),
			Ctx:         ctx,
		}))
	}
	return append(out, ui.LinkButton(ui.LinkButtonConfig{
		Label:      i18nui.T(ctx, i18nui.KeyDrawerOpenPanel),
		Href:       base,
		Icon:       "minimize",
		Variant:    ui.ButtonSecondary,
		ExtraAttrs: html.Attrs{"data-cui-intercept-panel": currentURLPath(ctx)},
	}))
}
