package entityui

import (
	"context"

	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework/i18nui"
	"github.com/DonaldMurillo/gofastr/framework/ui"
)

// Panel turns on the full page's Copy link and Open in panel, which
// returns to the list at Base with this record open as a drawer over it.
// Turn it on only when the record's route opens as a drawer over Base
// (app.InterceptFrom). Off by default.
func (b *RecordBuilder) Panel() *RecordBuilder { b.panel = true; return b }

func (b *RecordBuilder) panelPage(ctx context.Context) bool {
	return b.panel && b.base != "" && !inDrawer(ctx)
}

func (b *RecordBuilder) pageTools(ctx context.Context, base string) []render.HTML {
	if !b.panelPage(ctx) {
		return nil
	}
	var out []render.HTML
	if span, items := b.copyLink(ctx); len(items) > 0 {
		out = append(out, span, ui.CopyButton(ui.CopyButtonConfig{
			Target:      items[0].Copy.Target,
			Label:       i18nui.CopyLink.Text(ctx),
			Icon:        "link",
			ToastOnCopy: true,
			ToastTitle:  i18nui.CopyCopied.Text(ctx),
			Ctx:         ctx,
		}))
	}
	return append(out, ui.LinkButton(ui.LinkButtonConfig{
		Label:      i18nui.DrawerOpenPanel.Text(ctx),
		Href:       base,
		Icon:       "minimize",
		Variant:    ui.ButtonSecondary,
		ExtraAttrs: html.Attrs{"data-cui-intercept-panel": currentURLPath(ctx)},
	}))
}
