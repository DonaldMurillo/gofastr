package entityui

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/DonaldMurillo/gofastr/core/handler"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/crud"
	"github.com/DonaldMurillo/gofastr/framework/filter"
	"github.com/DonaldMurillo/gofastr/framework/i18nui"
	"github.com/DonaldMurillo/gofastr/framework/ui"
)

// A relation field is a ui.Picker: a search over the related entity,
// answered by PickerHandler, with a hidden input that submits the id.

const (
	// pickerRows is how many records one answer lists; past it the
	// answer ends with a note saying the list is cut off.
	pickerRows = 20
	// pickerScan caps the rows a title search reads for an entity with
	// no SearchFields.
	pickerScan = 500
	// pickerBodyLimit caps a search post: it carries one query.
	pickerBodyLimit int64 = 4 << 10
	// maxPickerQuery caps the query a search reads, in runes.
	maxPickerQuery = 200
)

// pickerID is a relation field's input id; the endpoint answers rows
// for that input's listbox.
func pickerID(field string) string { return "eui-f-" + field }

// relationPicker draws a belongs-to field. cur is the stored id; a
// record the caller may not read is labelled with the em dash and keeps
// its id, so a save does not clear the column.
func (fb *formBuilder) relationPicker(ctx context.Context, f schema.Field, label, help, id, cur string) render.HTML {
	if !fb.m.hasAPI {
		return fb.readOnly(ctx, f, label)
	}
	opts, more, refused := fb.b.ui.pickerOptions(ctx, f, "")
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
		Name: f.Name, ID: id, Label: label, Help: help,
		Required:    f.Required && !fb.masked[f.Name],
		Value:       cur,
		ValueLabel:  valueLabel,
		Placeholder: i18nui.TVars(ctx, i18nui.KeyEntityPickerSearch, map[string]string{"entities": plural}),
		Options:     opts,
		More:        more,
		Endpoint:    fb.m.api + "/_pick?field=" + url.QueryEscape(f.Name),
		Action:      render.Join(fb.relationOpen(ctx, f, cur), fb.relationNew(ctx, f)),
		Ctx:         ctx,
	})
}

// relationNew links to the related entity's create screen, beside the
// picker: only for an entity the UI has screens for and the caller may
// create.
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
	return ui.LinkButton(ui.LinkButtonConfig{
		Label:    i18nui.TVars(ctx, i18nui.KeyEntityNew, map[string]string{"entity": om.singular(ctx)}),
		Href:     base + "/create",
		Variant:  ui.ButtonSecondary,
		Icon:     "plus",
		IconOnly: true,
	})
}

// pickerOptions lists the related records a picker offers for q: the
// first ones by title when q is empty, else the matches of the related
// entity's search (its SearchFields), or of its titles when it declares
// none. Each read runs through the related entity's own handler and read
// gate, and a row its Decider refuses is left out. more is the cut-off
// note when there are further rows; refused reports that the related
// entity is unknown or refuses the caller.
func (u *UI) pickerOptions(ctx context.Context, f schema.Field, q string) (opts []ui.PickerOption, more string, refused bool) {
	other, err := u.entityFor(f.To)
	if err != nil {
		return nil, "", true
	}
	om, err := u.meta(other.GetName())
	if err != nil || !canRead(ctx, om.ch) {
		return nil, "", true
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
	rows, err := om.ch.ListAll(crud.WithReadHooks(ctx), list)
	if err != nil {
		return nil, "", false
	}
	titles := u.rowTitles(ctx, om, rows, 0)
	needle := strings.ToLower(q)
	for i, r := range rows {
		oid := cell(rowValue(r, om.pk))
		if oid == "" {
			continue
		}
		label := titles[i]
		if label == "" {
			label = oid
		}
		if byTitle && !strings.Contains(strings.ToLower(label), needle) {
			continue
		}
		if !canReadRecord(ctx, om.ch, oid) {
			continue
		}
		if len(opts) == pickerRows {
			more = i18nui.TVars(ctx, i18nui.KeyEntityPickerMore, map[string]string{"n": strconv.Itoa(pickerRows)})
			break
		}
		opts = append(opts, ui.PickerOption{Value: oid, Label: label})
	}
	return opts, more, false
}

// pickerBody is a picker search: the query its input holds.
type pickerBody struct {
	Q string `json:"q"`
}

// PickerHandler serves POST <api>/_pick?field=<field>: a relation picker's
// search, answered with option rows for that field's input. It answers
// 405 to another method, 403 to a cross-site post or a caller who may
// not read the entity or the related one, 404 for a field that is not
// an editable relation, 415 to a body that is not JSON, 413 past
// pickerBodyLimit and 400 to a malformed body. The answer is never
// stored: it is per caller.
func (u *UI) PickerHandler(entityName string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		if r.Method != http.MethodPost {
			w.Header().Set("Allow", http.MethodPost)
			writeBulkError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		if handler.IsCrossSiteRequest(r) {
			writeBulkError(w, http.StatusForbidden, "access denied")
			return
		}
		m, err := u.meta(entityName)
		if err != nil || !m.hasAPI {
			writeBulkError(w, http.StatusNotFound, "not found")
			return
		}
		f, ok := m.field(r.URL.Query().Get("field"))
		if !ok || f.Type != schema.Relation || m.system(f) || m.locked(f) {
			writeBulkError(w, http.StatusNotFound, "not found")
			return
		}
		ctx := r.Context()
		if m.tr != nil {
			ctx = i18nui.WithTranslator(ctx, m.tr)
		}
		if !canRead(ctx, m.ch) {
			writeBulkError(w, http.StatusForbidden, "access denied")
			return
		}
		if !handler.IsJSONContentType(r.Header.Get("Content-Type")) {
			writeBulkError(w, http.StatusUnsupportedMediaType, "unsupported media type")
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, pickerBodyLimit)
		var body pickerBody
		if err := handler.DecodeStrict(r.Body, &body); err != nil {
			if _, tooBig := errors.AsType[*http.MaxBytesError](err); tooBig {
				writeBulkError(w, http.StatusRequestEntityTooLarge, "request body too large")
				return
			}
			writeBulkError(w, http.StatusBadRequest, "invalid request body")
			return
		}
		opts, more, refused := u.pickerOptions(ctx, f, body.Q)
		if refused {
			writeBulkError(w, http.StatusForbidden, "access denied")
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(ui.PickerRows(pickerID(f.Name), opts, more)))
	})
}
