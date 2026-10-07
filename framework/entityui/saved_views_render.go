package entityui

import (
	"context"
	"errors"
	"log/slog"
	"net/url"
	"slices"
	"strings"

	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core-ui/interactive"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework/dsl"
	"github.com/DonaldMurillo/gofastr/framework/i18nui"
	"github.com/DonaldMurillo/gofastr/framework/ui"
)

// Saved views on the list. A saved view is the caller's own named
// narrowing — a filter text and a column list — kept by the host's
// SavedViewStore. It opens through the namespaced saved param; its
// filter and columns apply as if they were the filter and cols params,
// with an explicit param in the URL winning. The filter text is
// re-parsed and its fields re-checked on every open: a view that no
// longer parses, or names a field that is now Hidden or unknown, or
// carries a column that is no longer visible, draws the "no longer
// applies" callout and lists the All view — never an error page, and an
// unknown or foreign id draws the same callout, revealing nothing.

// openSaved resolves the saved-view state onto s: the caller's views
// for the strip, and the open one — its filter text stashed for narrow
// to merge, its columns applied when the URL names none. A view that no
// longer applies sets savedGone and nothing of it is used.
func (b *ListBuilder) openSaved(ctx context.Context, s *listState) error {
	m := s.m
	s.savedOn = b.saved && b.ui.views != nil && m.hasAPI
	if !s.savedOn {
		return nil
	}
	if views, err := b.ui.views.List(ctx, m.name); err == nil {
		s.savedViews = views
	} else {
		// A failed store is a degraded strip, not a failed screen.
		slog.WarnContext(ctx, "entityui: saved views read", "entity", m.name, "error", err)
	}
	id := strings.TrimSpace(s.q.Get(s.p.saved))
	if id == "" {
		return nil
	}
	v, err := b.ui.views.Get(ctx, m.name, id)
	if err != nil {
		// An unknown or foreign id is the same answer: the callout, and
		// nothing of the view applied — it reveals nothing.
		if errors.Is(err, ErrSavedViewNotFound) {
			s.savedGone = true
			return nil
		}
		slog.WarnContext(ctx, "entityui: saved view read", "entity", m.name, "error", err)
		return nil
	}
	// Re-checked, every time: the filter must still parse against the
	// entity's fields, and every column must still be a visible,
	// non-omitted field. One stale piece drops the whole view.
	if v.Filter != "" {
		if _, err := dsl.ParsePredicate(v.Filter, m.e.GetFields()); err != nil {
			s.savedGone = true
			return nil
		}
	}
	if len(v.Columns) > 0 && !s.validColsNames(v.Columns) {
		s.savedGone = true
		return nil
	}
	s.savedID = id
	s.savedName = v.Name
	if v.Filter != "" && !s.q.Has(s.p.filter) {
		s.savedFilter = v.Filter
	}
	if len(v.Columns) > 0 && !s.q.Has(s.p.cols) {
		s.setColumns(slices.Clone(v.Columns))
	}
	return nil
}

// savedGoneWarning is the callout an unopenable view earns: the view's
// narrowing is not applied, and the list below is the All view.
func savedGoneWarning(ctx context.Context) render.HTML {
	return ui.Callout(ui.CalloutConfig{
		Title:   i18nui.T(ctx, i18nui.KeyEntitySavedGoneTitle),
		Variant: ui.StatusWarning,
	}, render.Text(i18nui.T(ctx, i18nui.KeyEntitySavedGoneBody)))
}

// savedViewsOn reports whether this caller gets saved views at all. A
// caller with no user cannot save or keep views (the store refuses
// them), so neither the strip nor the save form draws: a builder never
// draws a write the handler would refuse.
func savedViewsOn(ctx context.Context, s *listState) bool {
	return s.savedOn && userID(ctx) != ""
}

// savedViewsStrip draws the caller's saved views: a link each, the open
// one marked current, and a delete form each. A caller with no views
// gets no strip; the save form is a list tool of its own (saveViewTool).
func (b *ListBuilder) savedViewsStrip(ctx context.Context, s *listState) render.HTML {
	if !savedViewsOn(ctx, s) || len(s.savedViews) == 0 {
		return ""
	}
	m := s.m
	back := listHref(s.path, s.q)
	parts := make([]render.HTML, 0, 2*len(s.savedViews)+2)
	for _, v := range s.savedViews {
		q := s.carry(s.p.saved, s.p.filter, s.p.cols, s.p.page)
		q.Set(s.p.saved, v.ID)
		attrs := html.Attrs{}
		if v.ID == s.savedID {
			attrs["aria-current"] = "true"
		}
		parts = append(parts, ui.Tag(ui.TagConfig{
			Label:      v.Name,
			Href:       listHref(s.path, q),
			ExtraAttrs: attrs,
			Ctx:        ctx,
		}))
		del := interactive.Post(m.api + "/_views/_delete/" + url.PathEscape(v.ID)).
			WithConfirmDialog(interactive.Confirm{
				Title:   i18nui.T(ctx, i18nui.KeyEntitySavedDeleteTitle),
				Message: i18nui.T(ctx, i18nui.KeyEntitySavedDeleteConfirm),
				Accept:  i18nui.T(ctx, i18nui.KeyEntitySavedDelete),
				Danger:  true,
			}).
			OnSuccessToast(i18nui.T(ctx, i18nui.KeyEntitySavedDeleted))
		parts = append(parts, ui.Form(ui.FormConfig{
			Action:     m.api + "/_views/_delete/" + url.PathEscape(v.ID),
			Method:     "POST",
			Ctx:        ctx,
			HideSubmit: true,
			ExtraAttrs: del.Attrs(),
		},
			hiddenInput("back", back),
			hiddenInput("key", s.key),
			ui.Button(ui.ButtonConfig{
				Label:   i18nui.TVars(ctx, i18nui.KeyEntitySavedDelete, map[string]string{"view": v.Name}),
				Variant: ui.ButtonGhost,
				Type:    "submit",
			}),
		))
	}
	return ui.Cluster(ui.ClusterConfig{Gap: ui.GapSM, Align: ui.AlignCenter}, parts...)
}

// saveViewTool is the "Save view" form inside a collapsible among the
// list's tools: a name, the active filter text and columns as hidden
// fields, and the list's URL to return to.
func (b *ListBuilder) saveViewTool(ctx context.Context, s *listState) render.HTML {
	if !savedViewsOn(ctx, s) {
		return ""
	}
	m := s.m
	return ui.Collapsible(ui.CollapsibleConfig{
		Summary: i18nui.T(ctx, i18nui.KeyEntitySavedSave),
		Name:    s.toolGroup(),
	}, ui.Form(ui.FormConfig{
		Action:      m.api + "/_views",
		Method:      "POST",
		Ctx:         ctx,
		SubmitLabel: i18nui.T(ctx, i18nui.KeyEntitySavedSave),
	},
		hiddenInput("back", listHref(s.path, s.q)),
		hiddenInput("key", s.key),
		hiddenInput("filter", s.filterText),
		hiddenInput("cols", strings.Join(s.columns, ",")),
		ui.TextField(ui.TextFieldConfig{
			Name:  "name",
			ID:    "eui-" + listIDSafe(s.key, m.name) + "-saveview",
			Label: i18nui.T(ctx, i18nui.KeyEntitySavedName),
		}),
	))
}
