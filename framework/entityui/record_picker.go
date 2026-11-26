package entityui

import (
	"context"
	"net/http"
	"net/url"
	"strings"

	"github.com/DonaldMurillo/gofastr/core/handler"
	"github.com/DonaldMurillo/gofastr/core/i18n"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/crud"
	"github.com/DonaldMurillo/gofastr/framework/filter"
	"github.com/DonaldMurillo/gofastr/framework/i18nui"
	"github.com/DonaldMurillo/gofastr/framework/ui"
	"github.com/DonaldMurillo/gofastr/internal/fieldtext"
)

const (
	pickerRows            = 20
	pickerScan            = 500
	pickerBodyLimit int64 = 4 << 10
	maxPickerQuery        = 200
)

func pickerID(field string) string { return "eui-f-" + field }

func (fb *formBuilder) relationPicker(ctx context.Context, f schema.Field, label, help, id, cur, form string) render.HTML {
	if !fb.m.hasAPI {
		return fb.readOnly(ctx, f, label)
	}
	opts, more, refused, err := fb.b.ui.pickerOptions(ctx, f, "")
	if err != nil {
		return slotFailed(ctx)
	}
	valueLabel := ""
	if cur != "" {
		valueLabel = "—"
		if !refused && fb.relationReadable(ctx, f, cur) {
			valueLabel = cur
			if other, err := fb.b.ui.entityFor(f.To); err == nil {
				if t, ok := fb.b.ui.RecordTitle(ctx, other.GetName(), cur); ok && t != "" {
					valueLabel = t
				}
			}
		}
	}
	plural := f.To
	if other, err := fb.b.ui.entityFor(f.To); err == nil {
		if om, err := fb.b.ui.meta(other.GetName()); err == nil {
			plural = strings.ToLower(om.plural(ctx))
		}
	}
	return ui.Picker(ui.PickerConfig{
		Name: fb.m.inputName(f.Name), ID: id, Label: label, Help: help,
		Required:    f.Required && !fb.masked[f.Name],
		Value:       cur,
		ValueLabel:  valueLabel,
		Form:        form,
		Placeholder: i18nui.EntityPickerSearch.Text(ctx, map[string]any{"entities": plural}),
		Options:     opts,
		More:        more,
		Endpoint:    fb.m.api + "/_pick?field=" + url.QueryEscape(f.Name),
		Action:      render.Join(fb.relationOpen(ctx, f, cur), fb.relationNew(ctx, f)),
		Ctx:         ctx,
	})
}

func (fb *formBuilder) relationNew(ctx context.Context, f schema.Field) render.HTML {
	base, ok := fb.b.ui.relatedBase(ctx, fb.m, f.Name)
	if !ok {
		return ""
	}
	other, err := fb.b.ui.entityFor(f.To)
	if err != nil {
		return ""
	}
	om, err := fb.b.ui.meta(other.GetName())
	if err != nil || !canCreate(ctx, om) {
		return ""
	}
	createPath := fb.b.ui.createScreenPath(other, base)
	if createPath == "" {
		return ""
	}
	return ui.LinkButton(ui.LinkButtonConfig{
		Label:    i18nui.EntityNew.Text(ctx, map[string]any{"entity": om.singular(ctx)}),
		Href:     createPath,
		Variant:  ui.ButtonSecondary,
		Icon:     "plus",
		IconOnly: true,
	})
}

func (u *UI) pickerOptions(ctx context.Context, f schema.Field, q string) (opts []ui.PickerOption, more string, refused bool, err error) {
	other, err := u.entityFor(f.To)
	if err != nil {
		return nil, "", true, nil
	}
	om, err := u.meta(other.GetName())
	if err != nil {
		return nil, "", false, err
	}
	if !canReadRelated(ctx, om.ch) {
		return nil, "", true, nil
	}
	q = strings.TrimSpace(q)
	if r := []rune(q); len(r) > maxPickerQuery {
		q = string(r[:maxPickerQuery])
	}
	list := crud.ListOptions{Fields: om.readTitleFields(), Limit: pickerRows + 1, Sorts: []filter.ParsedSort{{Field: om.pk}}}
	if tf := om.titleField(); tf != "" {
		if tff, ok := om.field(tf); ok && !tff.NoQuery {
			list.Sorts = []filter.ParsedSort{{Field: tf}, {Field: om.pk}}
		}
	}
	byTitle := q != "" && len(om.e.Config.SearchFields) == 0
	switch {
	case byTitle:
		list.Limit = pickerScan
	case q != "":
		list.Search = q
	}
	rows, err := om.listRows(ctx, list)
	if err != nil {
		return nil, "", false, err
	}
	needle := strings.ToLower(q)
	for _, r := range rows {
		oid := fieldtext.Cell(om.rowValue(r, om.pk))
		record := om.lookupRecord(ctx, oid, r)
		if record == nil {
			continue
		}
		label := u.rowTitles(ctx, om, []map[string]any{record}, 0)[0]
		if label == "" {
			label = oid
		}
		if byTitle && !strings.Contains(strings.ToLower(label), needle) {
			continue
		}
		if len(opts) == pickerRows {
			more = i18nui.EntityPickerMore.Text(ctx, map[string]any{"n": pickerRows})
			break
		}
		opts = append(opts, ui.PickerOption{Value: oid, Label: label})
	}
	return opts, more, false, nil
}

type pickerBody struct {
	Q string `json:"q"`
}

// PickerHandler serves POST <api>/_pick?field=<field>: a relation
// picker's search, answered with option rows for that field's input. It
// answers 405 to another method, 403 to a cross-site post, 404 for a
// field that is not an editable relation, the entity's read refusal
// (401 or 403), 415, 413 or 400 for a bad body, 403 when the caller may
// not read the related entity, and 500 when the read fails.
func (u *UI) PickerHandler(entityName string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		if r.Method != http.MethodPost {
			w.Header().Set("Allow", http.MethodPost)
			writeBulkError(w, r, http.StatusMethodNotAllowed, i18n.StatusPhrase(http.StatusMethodNotAllowed))
			return
		}
		if refuseCrossSitePost(w, r, reasonAccessDenied) {
			return
		}
		m, err := u.meta(entityName)
		if err != nil || !m.hasAPI {
			writeBulkError(w, r, http.StatusNotFound, i18n.StatusPhrase(http.StatusNotFound))
			return
		}
		f, ok := m.field(r.URL.Query().Get("field"))
		if !ok || f.Type != schema.Relation || m.system(f) || m.locked(f) {
			writeBulkError(w, r, http.StatusNotFound, i18n.StatusPhrase(http.StatusNotFound))
			return
		}
		ctx := r.Context()
		if m.tr != nil {
			ctx = i18n.WithTranslator(ctx, m.tr)
		}
		if !requireRead(w, r, ctx, m) {
			return
		}
		if !handler.IsJSONContentType(r.Header.Get("Content-Type")) {
			writeBulkError(w, r, http.StatusUnsupportedMediaType, i18n.ReasonJSONMediaType)
			return
		}
		var body pickerBody
		if !decodeBody(w, r, pickerBodyLimit, &body) {
			return
		}
		opts, more, refused, err := u.pickerOptions(ctx, f, body.Q)
		if err != nil {
			writeBulkError(w, r, http.StatusInternalServerError, i18n.StatusPhrase(http.StatusInternalServerError))
			return
		}
		if refused {
			writeBulkError(w, r, http.StatusForbidden, reasonAccessDenied)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(ui.PickerRows(pickerID(f.Name), opts, more)))
	})
}
