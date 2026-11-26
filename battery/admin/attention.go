package admin

import (
	"context"
	"fmt"
	"log/slog"
	"net/url"
	"slices"
	"strconv"

	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/framework/i18nui"
	"github.com/DonaldMurillo/gofastr/framework/ui"
)

// Watch is a list view whose rows need someone, previewed in the dashboard's
// Needs attention panel.
type Watch struct {
	// Entity is an exposed entity's name.
	Entity string

	// View is a key of the entity's Display.Views.
	View string

	// Columns replaces the entity's list columns in the preview; empty keeps
	// them.
	Columns []string

	// Rows is how many rows the preview shows: 5 when zero, at most 20.
	Rows int
}

const (
	watchRows    = 5
	maxWatchRows = 20
)

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

func watchedView(e *entity.Entity, key string) (entity.ListView, bool) {
	i := slices.IndexFunc(listViews(e), func(v entity.ListView) bool { return v.Key == key })
	if key == "" || i < 0 {
		return entity.ListView{}, false
	}
	return listViews(e)[i], true
}

func (b *Battery) attentionCard(ctx context.Context) render.HTML {
	var lists []render.HTML
	failed := false
	for i, w := range b.cfg.Attention {
		e, _ := b.exposedNamed(w.Entity)
		v, _ := watchedView(e, w.View)
		cctx, cancel := context.WithTimeout(ctx, countDeadline)
		n, ok, err := b.ui.CountView(cctx, w.Entity, w.View)
		cancel()
		if err != nil {
			slog.WarnContext(ctx, "admin: attention count", "entity", w.Entity, "view", w.View, "error", err)
			failed = true
		}
		if !ok || n == 0 {
			continue
		}
		rows := w.Rows
		if rows == 0 {
			rows = watchRows
		}
		href := b.entityBase(e) + "?view=" + url.QueryEscape(w.View)
		heading := i18nui.AdminAttentionList.Text(ctx, map[string]any{
			"entity": b.plural(ctx, e),
			"view":   i18nui.ViewLabel(ctx, nil, w.Entity, v.Key, v.Label),
		})
		lists = append(lists, b.ui.List(w.Entity).Base(b.entityBase(e)).Key("attn"+strconv.Itoa(i)).
			View(w.View).Columns(w.Columns...).Top(rows).Embedded().Flush().NoCreate().Heading(heading, 3).
			Actions(headerLink(i18nui.AdminViewAll.Text(ctx), href)).RenderCtx(ctx))
	}
	if b.cfg.Queue != nil && platformCaller(ctx) {
		if jobs := b.failedJobs(ctx); jobs != "" {
			lists = append(lists, jobs)
		}
	}
	title := i18nui.AdminAttention.Text(ctx)
	if failed {
		lists = append([]render.HTML{ui.Callout(ui.CalloutConfig{Variant: ui.StatusWarning},
			render.Text(i18nui.AdminAttentionIncomplete.Text(ctx)))}, lists...)
	}
	if len(lists) == 0 {
		return ui.Card(ui.CardConfig{Heading: title, HeadingLevel: 2},
			ui.EmptyState(ui.EmptyStateConfig{Title: i18nui.AdminAttentionClear.Text(ctx), HeadingLevel: 3}))
	}
	return ui.Card(ui.CardConfig{Heading: title, HeadingLevel: 2}, ui.Stack(ui.StackConfig{Gap: ui.GapLG}, lists...))
}
