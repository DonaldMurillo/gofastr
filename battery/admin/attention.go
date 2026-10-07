package admin

import (
	"context"
	"fmt"
	"net/url"
	"slices"
	"strconv"

	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/framework/i18nui"
	"github.com/DonaldMurillo/gofastr/framework/ui"
)

// Watch is a list view whose rows need someone: past-due invoices,
// failed payments. The dashboard's Needs attention panel previews the
// first rows of every watched view that has any.
type Watch struct {
	// Entity is an exposed entity's name.
	Entity string

	// View is a key of the entity's Display.Views: its Where picks the
	// rows and its Sort orders them.
	View string

	// Columns replaces the entity's list columns in the preview; empty
	// keeps them.
	Columns []string

	// Rows is how many rows the preview shows: 5 when zero, at most 20.
	Rows int
}

// watchRows is how many rows a watch previews by default, and maxWatchRows
// the most it may ask for: a dashboard panel, not a list.
const (
	watchRows    = 5
	maxWatchRows = 20
)

// checkWatch refuses a watch that names what the admin cannot draw.
func (b *Battery) checkWatch(w Watch) error {
	e, ok := b.exposedNamed(w.Entity)
	if !ok {
		return fmt.Errorf("watches %q, which the admin does not expose", w.Entity)
	}
	if _, ok := watchedView(e, w.View); !ok {
		return fmt.Errorf("watches view %q, which %s does not declare", w.View, w.Entity)
	}
	for _, c := range w.Columns {
		if !slices.ContainsFunc(e.GetFields(), func(f schema.Field) bool { return f.Name == c && !f.Hidden }) {
			return fmt.Errorf("shows column %q, which %s does not have", c, w.Entity)
		}
	}
	if w.Rows < 0 || w.Rows > maxWatchRows {
		return fmt.Errorf("asks for %d rows; 0 (the default %d) to %d", w.Rows, watchRows, maxWatchRows)
	}
	return nil
}

// watchedView is the declared view the watch names.
func watchedView(e *entity.Entity, key string) (entity.ListView, bool) {
	i := slices.IndexFunc(listViews(e), func(v entity.ListView) bool { return v.Key == key })
	if key == "" || i < 0 {
		return entity.ListView{}, false
	}
	return listViews(e)[i], true
}

// attentionCard is the Needs attention panel: a preview of each watched
// view with rows, read in the admin's scope, or a line saying there are
// none. A view the count cannot read draws nothing.
func (b *Battery) attentionCard(ctx context.Context) render.HTML {
	var lists []render.HTML
	for i, w := range b.cfg.Attention {
		e, _ := b.exposedNamed(w.Entity)
		v, _ := watchedView(e, w.View)
		cctx, cancel := context.WithTimeout(ctx, countDeadline)
		n, ok := b.ui.Count(cctx, w.Entity, v.Where)
		cancel()
		if !ok || n == "0" {
			continue
		}
		rows := w.Rows
		if rows == 0 {
			rows = watchRows
		}
		href := b.entityBase(e) + "?view=" + url.QueryEscape(w.View)
		heading := i18nui.TVars(ctx, i18nui.KeyAdminAttentionList, map[string]string{
			"entity": b.plural(ctx, e),
			"view":   i18nui.ViewLabel(ctx, nil, w.Entity, v.Key, v.Label),
		})
		lists = append(lists, b.ui.List(w.Entity).Base(b.entityBase(e)).Key("attn"+strconv.Itoa(i)).
			View(w.View).Columns(w.Columns...).Top(rows).Embedded().NoCreate().Heading(heading, 3).
			Actions(headerLink(i18nui.T(ctx, i18nui.KeyAdminViewAll), href)).RenderCtx(ctx))
	}
	title := i18nui.T(ctx, i18nui.KeyAdminAttention)
	if len(lists) == 0 {
		return ui.Card(ui.CardConfig{Heading: title, HeadingLevel: 2},
			ui.EmptyState(ui.EmptyStateConfig{Title: i18nui.T(ctx, i18nui.KeyAdminAttentionClear), HeadingLevel: 3}))
	}
	return ui.Card(ui.CardConfig{Heading: title, HeadingLevel: 2}, ui.Stack(ui.StackConfig{Gap: ui.GapLG}, lists...))
}
