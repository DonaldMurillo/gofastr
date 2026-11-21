package admin

import (
	"context"
	"net/http"
	"strings"

	appui "github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core/i18n"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework/i18nui"
	"github.com/DonaldMurillo/gofastr/framework/ui"
	"github.com/DonaldMurillo/gofastr/internal/listfilter"
)

const (
	maxPaletteBody  int64 = 4 << 10
	maxPaletteQuery       = 200
	paletteRecords        = 5
	paletteCap            = 20
)

func (b *Battery) handlePalette(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxPaletteBody)
	if err := r.ParseForm(); err != nil {
		i18n.HTTPError(w, r, http.StatusBadRequest, i18n.StatusPhrase(http.StatusBadRequest))
		return
	}
	ctx := appui.WithRequest(r.Context(), r)
	cmds := b.paletteCommands(ctx, r.PostFormValue("q"))
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write([]byte(ui.PaletteResults(ctx, paletteName, cmds, i18nui.AdminPaletteEmpty.Text(ctx))))
}

func (b *Battery) renderSearch(ctx context.Context, _ map[string]string) render.HTML {
	q := ""
	dropped := false
	if r := appui.RequestFromContext(ctx); r != nil {
		q = r.URL.Query().Get("q")
		dropped = listfilter.DroppedNarrowing(r.URL.RawQuery, func(name string) bool { return name == "q" })
	}
	label := i18nui.AdminSearch.Text(ctx)
	parts := []render.HTML{
		ui.PageHeader(ui.PageHeaderConfig{Title: i18nui.AdminSearch.Text(ctx)}),
		ui.FilterToolbar(ui.FilterToolbarConfig{
			Action:    b.cfg.PathPrefix + "/search",
			Search:    &ui.FilterSearch{Name: "q", Value: q, Placeholder: label, Label: label},
			HideReset: true,
			Ctx:       ctx,
		}),
	}
	if dropped {
		parts = append(parts, listfilter.Warning(ctx))
	}
	cmds := b.paletteCommands(ctx, q)
	if len(cmds) == 0 {
		parts = append(parts, ui.EmptyState(ui.EmptyStateConfig{Title: i18nui.AdminPaletteEmpty.Text(ctx), HeadingLevel: 2}))
		return ui.Stack(ui.StackConfig{Gap: ui.GapLG}, parts...)
	}
	rows := make([]render.HTML, len(cmds))
	for i, c := range cmds {
		rows[i] = ui.Cluster(ui.ClusterConfig{Gap: ui.GapSM, Align: ui.AlignCenter},
			ui.Link(ui.LinkConfig{Href: c.Href, Text: c.Label}), ui.Muted(render.Text(c.Meta)))
	}
	parts = append(parts, ui.Stack(ui.StackConfig{Gap: ui.GapSM}, rows...))
	return ui.Stack(ui.StackConfig{Gap: ui.GapLG}, parts...)
}

func (b *Battery) paletteCommands(ctx context.Context, q string) []ui.PaletteCommand {
	q = strings.TrimSpace(q)
	if r := []rune(q); len(r) > maxPaletteQuery {
		q = string(r[:maxPaletteQuery])
	}
	needle := strings.ToLower(q)
	var out []ui.PaletteCommand
	add := func(c ui.PaletteCommand) {
		if len(out) < paletteCap && (needle == "" || strings.Contains(strings.ToLower(c.Label), needle)) {
			out = append(out, c)
		}
	}
	page := i18nui.AdminPalettePage.Text(ctx)
	add(ui.PaletteCommand{Label: i18nui.AdminDashboard.Text(ctx), Href: b.cfg.PathPrefix, Meta: page})
	for _, it := range b.opsItems(ctx) {
		add(ui.PaletteCommand{Label: it.Label, Href: it.Href, Meta: page})
	}
	for _, p := range b.cfg.Pages {
		if b.pageAllows(ctx, p) {
			add(ui.PaletteCommand{Label: p.Title.Text(ctx), Href: b.cfg.PathPrefix + p.Path, Meta: page})
		}
	}
	for _, e := range b.ents {
		plural := b.plural(ctx, e)
		add(ui.PaletteCommand{Label: plural, Href: b.entityBase(e), Meta: i18nui.AdminEntities.Text(ctx)})
		if b.authorized(ctx) && b.canCreateEntity(b.elevate(ctx), e) {
			add(ui.PaletteCommand{
				Label: i18nui.AdminPaletteNew.Text(ctx, map[string]any{"entity": b.singular(ctx, e)}),
				Href:  b.entityBase(e) + "/create",
				Meta:  plural,
			})
		}
	}
	for _, c := range b.cfg.Commands {
		add(c)
	}
	if q == "" || !b.authorized(ctx) {
		return out
	}
	rctx := b.elevate(ctx)
	for _, e := range b.ents {
		for _, m := range b.ui.SearchRecords(rctx, e.GetName(), q, paletteRecords) {
			if len(out) >= paletteCap {
				return out
			}
			href := b.recordHref(e, m.ID)
			if href == "" {
				continue
			}
			out = append(out, ui.PaletteCommand{
				Label: m.Title,
				Href:  href,
				Meta:  b.singular(ctx, e),
			})
		}
	}
	return out
}
