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

func (b *ListBuilder) openSaved(ctx context.Context, s *listState) error {
	m := s.m
	s.savedOn = b.saved && b.ui.views != nil && m.hasAPI
	if !s.savedOn {
		return nil
	}
	if views, err := b.ui.views.List(ctx, m.name); err == nil {
		s.savedViews = views
	} else {
		slog.WarnContext(ctx, "entityui: saved views read", "entity", m.name, "error", err)
	}
	id := strings.TrimSpace(s.q.Get(s.p.saved))
	if id == "" {
		return nil
	}
	v, err := b.ui.views.Get(ctx, m.name, id)
	if err != nil {
		if errors.Is(err, ErrSavedViewNotFound) {
			s.savedGone = true
			return nil
		}
		slog.WarnContext(ctx, "entityui: saved view read", "entity", m.name, "error", err)
		return nil
	}
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
		var cols []string
		for _, c := range v.Columns {
			if slices.Contains(s.available, c) {
				cols = append(cols, c)
			}
		}
		if len(cols) > 0 {
			s.setColumns(cols)
		}
	}
	return nil
}

func savedGoneWarning(ctx context.Context) render.HTML {
	return ui.Callout(ui.CalloutConfig{
		Title:   i18nui.EntitySavedGoneTitle.Text(ctx),
		Variant: ui.StatusWarning,
	}, render.Text(i18nui.EntitySavedGoneBody.Text(ctx)))
}

func savedViewsOn(ctx context.Context, s *listState) bool {
	return s.savedOn && userID(ctx) != ""
}

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
		Ctx:   ctx,
		Label: i18nui.EntitySavedSave.Text(ctx),
		Icon:  "bookmark",
		Align: ui.DropdownEnd,
		ID:    "eui-" + listIDSafe(s.key, m.name) + "-saveview-pop",
		Content: ui.Form(ui.FormConfig{
			Action:      m.api + "/_views",
			Method:      "POST",
			Ctx:         ctx,
			SubmitLabel: i18nui.EntitySavedSave.Text(ctx),
		},
			hiddenInput("back", listHref(s.path, s.q)),
			hiddenInput("key", s.key),
			hiddenInput("filter", s.filterText),
			hiddenInput("cols", strings.Join(s.columns, ",")),
			ui.TextField(ui.TextFieldConfig{Ctx: ctx,
				Name:     "name",
				ID:       "eui-" + listIDSafe(s.key, m.name) + "-saveview",
				Label:    i18nui.EntitySavedName.Text(ctx),
				Required: true,
			}),
		),
	})
}

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
	action := m.api + "/_views/_delete?id=" + url.QueryEscape(s.savedID)
	back := listHref(s.path, s.carry(s.p.saved, s.p.filter, s.p.cols, s.p.page))
	del := interactive.Post(action).
		WithConfirmDialog(interactive.Confirm{
			Title:   i18nui.EntitySavedDeleteTitle.Text(ctx),
			Message: i18nui.EntitySavedDeleteConfirm.Text(ctx),
			Accept:  i18nui.EntitySavedDelete.Text(ctx),
			Danger:  true,
		}).
		OnSuccess(interactive.Navigate(back))
	return ui.Form(ui.FormConfig{
		Action:     action,
		Method:     "POST",
		Ctx:        ctx,
		HideSubmit: true,
		ExtraAttrs: del.Attrs(),
	},
		hiddenInput("back", back),
		hiddenInput("key", s.key),
		ui.Button(ui.ButtonConfig{
			Ctx:     ctx,
			Label:   i18nui.EntitySavedDelete.Text(ctx, map[string]any{"view": name}),
			Variant: ui.ButtonGhost,
			Size:    ui.ButtonSizeSmall,
			Type:    "submit",
		}),
	)
}
