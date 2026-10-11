package admin

import (
	"context"
	"net/http"
	"net/url"
	"strings"

	appui "github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework/i18nui"
	"github.com/DonaldMurillo/gofastr/framework/ui"
)

// The command palette and its scriptless twin, the search page: jump to
// a page or an entity, create one, or open a record found by title.

const (
	// maxPaletteBody caps a palette search post: it carries one query.
	maxPaletteBody int64 = 4 << 10
	// maxPaletteQuery caps the query a search reads.
	maxPaletteQuery = 200
	// paletteRecords is how many records one entity contributes.
	paletteRecords = 5
	// paletteCap caps one answer.
	paletteCap = 20
)

// handlePalette answers a palette search with option rows.
func (b *Battery) handlePalette(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxPaletteBody)
	if err := r.ParseForm(); err != nil {
		http.Error(w, http.StatusText(http.StatusBadRequest), http.StatusBadRequest)
		return
	}
	ctx := appui.WithRequest(r.Context(), r)
	cmds := b.paletteCommands(ctx, r.PostFormValue("q"))
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write([]byte(ui.PaletteResults(ctx, paletteName, cmds, i18nui.T(ctx, i18nui.KeyAdminPaletteEmpty))))
}

// renderSearch is the search page: the palette's results for ?q= as
// links, for a reader without script.
func (b *Battery) renderSearch(ctx context.Context, _ map[string]string) render.HTML {
	q := ""
	if r := appui.RequestFromContext(ctx); r != nil {
		q = r.URL.Query().Get("q")
	}
	label := i18nui.T(ctx, i18nui.KeyAdminSearch)
	parts := []render.HTML{
		ui.PageHeader(ui.PageHeaderConfig{Title: i18nui.T(ctx, i18nui.KeyAdminSearch)}),
		ui.FilterToolbar(ui.FilterToolbarConfig{
			Action:    b.cfg.PathPrefix + "/search",
			Search:    &ui.FilterSearch{Name: "q", Value: q, Placeholder: label, Label: label},
			HideReset: true,
			Ctx:       ctx,
		}),
	}
	cmds := b.paletteCommands(ctx, q)
	if len(cmds) == 0 {
		parts = append(parts, ui.EmptyState(ui.EmptyStateConfig{Title: i18nui.T(ctx, i18nui.KeyAdminPaletteEmpty), HeadingLevel: 2}))
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

// paletteCommands are the destinations matching q, at most paletteCap:
// pages, entity lists, create links and the app's commands whose label
// holds q, then records whose title matches it. An empty q lists every
// destination and no record.
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
	page := i18nui.T(ctx, i18nui.KeyAdminPalettePage)
	add(ui.PaletteCommand{Label: i18nui.T(ctx, i18nui.KeyAdminDashboard), Href: b.cfg.PathPrefix, Meta: page})
	for _, it := range b.opsItems(ctx) {
		add(ui.PaletteCommand{Label: it.Label, Href: it.Href, Meta: page})
	}
	for _, p := range b.cfg.Pages {
		if b.pageAllows(ctx, p) {
			add(ui.PaletteCommand{Label: p.Title, Href: b.cfg.PathPrefix + p.Path, Meta: page})
		}
	}
	for _, e := range b.ents {
		plural := b.plural(ctx, e)
		add(ui.PaletteCommand{Label: plural, Href: b.entityBase(e), Meta: i18nui.T(ctx, i18nui.KeyAdminEntities)})
		add(ui.PaletteCommand{
			Label: i18nui.TVars(ctx, i18nui.KeyAdminPaletteNew, map[string]string{"entity": b.singular(ctx, e)}),
			Href:  b.entityBase(e) + "/create",
			Meta:  plural,
		})
	}
	for _, c := range b.cfg.Commands {
		add(c)
	}
	if q == "" || !b.authorized(ctx) {
		return out
	}
	// The admin's own read, elevated past each entity's Access check;
	// scope, the Decider and each row's gate still bind.
	rctx := b.elevate(ctx)
	for _, e := range b.ents {
		for _, m := range b.ui.SearchRecords(rctx, e.GetName(), q, paletteRecords) {
			if len(out) >= paletteCap {
				return out
			}
			out = append(out, ui.PaletteCommand{
				Label: m.Title,
				Href:  b.entityBase(e) + "/" + url.PathEscape(m.ID),
				Meta:  b.singular(ctx, e),
			})
		}
	}
	return out
}
