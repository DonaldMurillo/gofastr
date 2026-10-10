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

// listBulk is what a bulk list draws beyond a plain one: the bar's form
// id, which the row checkboxes join through their form attribute, and
// the actions the bar offers this caller. A nil listBulk draws nothing.
type listBulk struct {
	form    string
	actions []bulkAction
}

// bulkFor reports the bulk state of the list: on when the builder asked
// for it, the entity allows it, and the caller has at least one action.
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

// bulkBar draws the bar above the rows: the action and scope selects,
// Apply, and as hidden fields the page's ids, the list key and the
// list's query. It is a form RPC to POST <api>/_bulk; success re-fetches
// the list and the server's toast reports the counts, a refusal lands in
// the form's error summary. Every action asks first.
//
// "Selected rows" is offered only beside the table, the presentation
// with a select column. "Every match" needs a known total and a filter
// the list applied, and is left out on a list with .Where pins: the
// pins are not in the query, so the server's every-match read would be
// wider than the rows the screen drew.
func (b *ListBuilder) bulkBar(ctx context.Context, s *listState, lb *listBulk, rows []map[string]any, total int, known bool) render.HTML {
	if len(rows) == 0 {
		return ""
	}
	m := s.m
	// Beside the table the bar floats under the rows as a pill, whose
	// place names its controls; above cards it is a plain form.
	floating := s.as != "cards"
	actionOpts := make([]ui.SelectOption, 0, len(lb.actions))
	for _, a := range lb.actions {
		actionOpts = append(actionOpts, ui.SelectOption{Value: a.key, Text: a.label})
	}
	var scopes []ui.SelectOption
	if s.as != "cards" {
		scopes = append(scopes, ui.SelectOption{Value: bulkScopeSelected, Text: i18nui.T(ctx, i18nui.KeyEntityBulkSelected)})
	}
	scopes = append(scopes, ui.SelectOption{Value: bulkScopePage, Text: i18nui.TVars(ctx, i18nui.KeyEntityBulkPage, map[string]string{
		"count": strconv.Itoa(len(rows)),
	})})
	// The bar carries the digest of the ids every match covers, so the
	// run refuses any other set. A list past the cap is not offered it.
	var match string
	if known && !s.filterBad && len(b.where) == 0 && s.savedID == "" && total > len(rows) {
		if ids, err := b.ui.matchIDs(ctx, m, s.key, s.carry()); err == nil {
			match = matchDigest(ids)
		}
	}
	every := match != ""
	if every {
		scopes = append(scopes, ui.SelectOption{Value: bulkScopeEvery, Text: i18nui.TVars(ctx, i18nui.KeyEntityBulkEvery, map[string]string{
			"count": formatNumber(float64(total), 0),
		})})
	}

	fields := make([]render.HTML, 0, len(rows)+6)
	for _, row := range rows {
		fields = append(fields, hiddenInput("page", cell(rowValue(row, m.pk))))
	}
	if every {
		fields = append(fields, hiddenInput("match", match))
	}
	// A soft delete's toast offers Undo, which returns here.
	if b.undo && m.e.Config.Scope.SoftDelete {
		fields = append(fields, hiddenInput("undo", "1"), hiddenInput("back", listHref(s.path, s.q)))
	}
	fields = append(fields,
		hiddenInput("key", s.key),
		hiddenInput("query", s.carry().Encode()),
		ui.Cluster(ui.ClusterConfig{Gap: ui.GapSM, Align: ui.AlignEnd},
			ui.Select(ui.SelectConfig{
				Name:        "action",
				ID:          lb.form + "-action",
				Label:       i18nui.T(ctx, i18nui.KeyEntityBulkAction),
				LabelHidden: floating,
				Options:     actionOpts,
			}),
			ui.Select(ui.SelectConfig{
				Name:        "scope",
				ID:          lb.form + "-scope",
				Label:       i18nui.T(ctx, i18nui.KeyEntityBulkScope),
				LabelHidden: floating,
				Options:     scopes,
			}),
			ui.Button(ui.ButtonConfig{
				Label:   i18nui.T(ctx, i18nui.KeyEntityBulkApply),
				Type:    "submit",
				Variant: ui.ButtonSecondary,
			}),
		),
	)
	rpc := interactive.Post(m.api + "/_bulk").
		WithConfirmDialog(interactive.Confirm{
			Title:   i18nui.TVars(ctx, i18nui.KeyEntityBulkTitle, map[string]string{"entity": m.noun(ctx, true)}),
			Message: i18nui.T(ctx, i18nui.KeyEntityBulkConfirm),
			Accept:  i18nui.T(ctx, i18nui.KeyEntityBulkApply),
		}).
		OnSuccess(interactive.Navigate(listHref(s.path, s.q)))
	attrs := rpc.Attrs()
	attrs["aria-label"] = i18nui.T(ctx, i18nui.KeyEntityBulkBar)
	return ui.Form(ui.FormConfig{
		Action:     m.api + "/_bulk",
		Method:     "POST",
		ID:         lb.form,
		Ctx:        ctx,
		HideSubmit: true,
		ExtraAttrs: attrs,
	}, fields...)
}

// selectCell is one row's checkbox in the select column, a control of
// the bulk bar's form named by the row's title.
func selectCell(ctx context.Context, s *listState, lb *listBulk, row map[string]any, i int) render.HTML {
	return ui.Checkbox(ui.ToggleConfig{
		Name:        "ids",
		ID:          lb.form + "-sel-" + strconv.Itoa(i),
		Value:       cell(rowValue(row, s.m.pk)),
		Label:       i18nui.TVars(ctx, i18nui.KeyEntityBulkSelect, map[string]string{"title": s.rowTitle(ctx, row)}),
		LabelHidden: true,
		ExtraAttrs:  html.Attrs{"form": lb.form},
	})
}

// exportLink is the header's Export CSV link: <api>/_export.csv with the
// list's narrowing (its view, search, filter and facets, a builder's View
// included) and key, so the file holds the rows the list narrowed to.
// It is a download, which the client router leaves to the browser.
func exportLink(ctx context.Context, s *listState) render.HTML {
	q := s.carry()
	q.Del(exportKeyParam)
	if s.key != "" {
		q.Set(exportKeyParam, s.key)
	}
	return ui.LinkButton(ui.LinkButtonConfig{
		Label:      i18nui.T(ctx, i18nui.KeyEntityBulkExport),
		Href:       listHref(s.m.api+"/_export.csv", q),
		Variant:    ui.ButtonSecondary,
		Icon:       "download",
		ExtraAttrs: html.Attrs{"download": ""},
	})
}

// hiddenInput is one hidden form field.
func hiddenInput(name, value string) render.HTML {
	return html.Input(html.InputConfig{Type: "hidden", Name: name, Value: value})
}
