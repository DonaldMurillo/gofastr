package entityui

import (
	"context"
	"strconv"

	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core-ui/interactive"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework/i18nui"
	"github.com/DonaldMurillo/gofastr/framework/ui"
)

type listBulk struct {
	form    string
	actions []bulkAction
}

func (b *ListBuilder) bulkFor(ctx context.Context, s *listState) *listBulk {
	if !b.bulk || !bulkOn(s.m) || s.deletedView {
		return nil
	}
	actions := b.ui.bulkActions(ctx, s.m)
	if len(actions) == 0 {
		return nil
	}
	return &listBulk{form: "eui-" + listIDSafe(s.key, s.m.name) + "-bulk", actions: actions}
}

func (b *ListBuilder) bulkBar(ctx context.Context, s *listState, lb *listBulk, rows []map[string]any, total int, known bool) render.HTML {
	if len(rows) == 0 {
		return ""
	}
	m := s.m
	floating := s.as != "cards"
	actionOpts := make([]ui.SelectOption, 0, len(lb.actions))
	for _, a := range lb.actions {
		actionOpts = append(actionOpts, ui.SelectOption{Value: a.key, Text: a.label})
	}
	var scopes []ui.SelectOption
	if s.as != "cards" {
		scopes = append(scopes, ui.SelectOption{Value: bulkScopeSelected, Text: i18nui.EntityBulkSelected.Text(ctx)})
	}
	scopes = append(scopes, ui.SelectOption{Value: bulkScopePage, Text: i18nui.EntityBulkPage.Text(ctx, map[string]any{
		"count": len(rows),
	})})
	var match string
	if known && !s.filterBad && len(b.where) == 0 && s.savedID == "" && total > len(rows) {
		if ids, err := b.ui.matchIDs(ctx, m, s.key, s.carry()); err == nil {
			match = matchDigest(ids)
		}
	}
	every := match != ""
	if every {
		scopes = append(scopes, ui.SelectOption{Value: bulkScopeEvery, Text: i18nui.EntityBulkEvery.Text(ctx, map[string]any{
			"count": total,
		})})
	}

	fields := make([]render.HTML, 0, len(rows)+6)
	for _, row := range rows {
		fields = append(fields, hiddenInput("page", m.rowID(row)))
	}
	if every {
		fields = append(fields, hiddenInput("match", match))
	}
	if b.undo && m.e.Config.Scope.SoftDelete {
		fields = append(fields, hiddenInput("undo", "1"), hiddenInput("back", listHref(s.path, s.q)))
	}
	fields = append(fields,
		hiddenInput("key", s.key),
		hiddenInput("query", s.carry().Encode()),
		ui.Cluster(ui.ClusterConfig{Gap: ui.GapSM, Align: ui.AlignEnd},
			ui.Select(ui.SelectConfig{Ctx: ctx,
				Name:        "action",
				ID:          lb.form + "-action",
				Label:       i18nui.EntityBulkAction.Text(ctx),
				LabelHidden: floating,
				Options:     actionOpts,
			}),
			ui.Select(ui.SelectConfig{Ctx: ctx,
				Name:        "scope",
				ID:          lb.form + "-scope",
				Label:       i18nui.EntityBulkScope.Text(ctx),
				LabelHidden: floating,
				Options:     scopes,
			}),
			ui.Button(ui.ButtonConfig{
				Ctx:     ctx,
				Label:   i18nui.EntityBulkApply.Text(ctx),
				Type:    "submit",
				Variant: ui.ButtonSecondary,
			}),
		),
	)
	rpc := interactive.Post(m.api + "/_bulk").
		WithConfirmDialog(interactive.Confirm{
			Title:   i18nui.EntityBulkTitle.Text(ctx, map[string]any{"entity": m.noun(ctx, true)}),
			Message: i18nui.EntityBulkConfirm.Text(ctx),
			Accept:  i18nui.EntityBulkApply.Text(ctx),
		}).
		OnSuccess(interactive.Navigate(listHref(s.path, s.q)))
	attrs := rpc.Attrs()
	attrs["aria-label"] = i18nui.EntityBulkBar.Text(ctx)
	return ui.Form(ui.FormConfig{
		Action:     m.api + "/_bulk",
		Method:     "POST",
		ID:         lb.form,
		Ctx:        ctx,
		HideSubmit: true,
		ExtraAttrs: attrs,
	}, fields...)
}

func selectCell(ctx context.Context, s *listState, lb *listBulk, row map[string]any, i int) render.HTML {
	return ui.Checkbox(ui.ToggleConfig{Ctx: ctx,
		Name:        "ids",
		ID:          lb.form + "-sel-" + strconv.Itoa(i),
		Value:       s.m.rowID(row),
		Label:       i18nui.EntityBulkSelect.Text(ctx, map[string]any{"title": s.rowTitle(ctx, row)}),
		LabelHidden: true,
		ExtraAttrs:  html.Attrs{"form": lb.form},
	})
}

func exportLink(ctx context.Context, s *listState) render.HTML {
	q := s.carry()
	for name := range q {
		if !s.exportParam(name) {
			q.Del(name)
		}
	}
	if s.key != "" {
		q.Set(exportKeyParam, s.key)
	}
	return ui.LinkButton(ui.LinkButtonConfig{
		Label:      i18nui.EntityBulkExport.Text(ctx),
		Href:       listHref(s.m.api+"/_export.csv", q),
		Variant:    ui.ButtonSecondary,
		Icon:       "download",
		ExtraAttrs: html.Attrs{"download": ""},
	})
}

func hiddenInput(name, value string) render.HTML {
	return html.Input(html.InputConfig{Type: "hidden", Name: name, Value: value})
}
