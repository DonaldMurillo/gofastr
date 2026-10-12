package entityui

import (
	"context"
	"errors"
	"log/slog"
	"net/url"
	"slices"
	"strings"

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

// viewTools are the view strip's end: "Save view" while the list shows
// something a saved view would keep that is not already one, and
// "Delete view" while a saved view is open.
func (b *ListBuilder) viewTools(ctx context.Context, s *listState) render.HTML {
	if !savedViewsOn(ctx, s) {
		return ""
	}
	var out []render.HTML
	if save := b.saveViewTool(ctx, s); save != "" {
		out = append(out, save)
	}
	if del := b.deleteViewTool(ctx, s); del != "" {
		out = append(out, del)
	}
	return actionCluster(out)
}

// saveViewTool is the "Save view" dropdown: a name field and Save, the
// active filter text and columns as hidden fields, and the list's URL
// to return to. A saved view keeps the filter and the columns, so the
// tool shows only when one of them is set and is not simply an open
// saved view's own.
func (b *ListBuilder) saveViewTool(ctx context.Context, s *listState) render.HTML {
	changed := s.q.Has(s.p.filter) || s.q.Has(s.p.cols)
	if s.savedID == "" {
		changed = s.filterText != "" || s.q.Has(s.p.cols)
	}
	if !changed {
		return ""
	}
	m := s.m
	return ui.Dropdown(ui.DropdownConfig{
		Label: i18nui.T(ctx, i18nui.KeyEntitySavedSave),
		Icon:  "bookmark",
		Align: ui.DropdownEnd,
		ID:    "eui-" + listIDSafe(s.key, m.name) + "-saveview-pop",
		Content: ui.Form(ui.FormConfig{
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
				Name:     "name",
				ID:       "eui-" + listIDSafe(s.key, m.name) + "-saveview",
				Label:    i18nui.T(ctx, i18nui.KeyEntitySavedName),
				Required: true,
			}),
		),
	})
}

// deleteViewTool is the open saved view's delete form, behind the
// confirm dialog; it returns to the list without the view.
func (b *ListBuilder) deleteViewTool(ctx context.Context, s *listState) render.HTML {
	if s.savedID == "" {
		return ""
	}
	name := ""
	for _, v := range s.savedViews {
		if v.ID == s.savedID {
			name = v.Name
		}
	}
	if name == "" {
		return ""
	}
	m := s.m
	action := m.api + "/_views/_delete/" + url.PathEscape(s.savedID)
	del := interactive.Post(action).
		WithConfirmDialog(interactive.Confirm{
			Title:   i18nui.T(ctx, i18nui.KeyEntitySavedDeleteTitle),
			Message: i18nui.T(ctx, i18nui.KeyEntitySavedDeleteConfirm),
			Accept:  i18nui.T(ctx, i18nui.KeyEntitySavedDelete),
			Danger:  true,
		}).
		OnSuccessToast(i18nui.T(ctx, i18nui.KeyEntitySavedDeleted))
	return ui.Form(ui.FormConfig{
		Action:     action,
		Method:     "POST",
		Ctx:        ctx,
		HideSubmit: true,
		ExtraAttrs: del.Attrs(),
	},
		hiddenInput("back", listHref(s.path, s.carry(s.p.saved, s.p.filter, s.p.cols, s.p.page))),
		hiddenInput("key", s.key),
		ui.Button(ui.ButtonConfig{
			Label:   i18nui.TVars(ctx, i18nui.KeyEntitySavedDelete, map[string]string{"view": name}),
			Variant: ui.ButtonGhost,
			Size:    ui.ButtonSizeSmall,
			Type:    "submit",
		}),
	)
}
