package entityui

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/url"
	"strings"

	"github.com/DonaldMurillo/gofastr/core/handler"
	"github.com/DonaldMurillo/gofastr/framework/dsl"
	"github.com/DonaldMurillo/gofastr/framework/i18nui"
	"github.com/DonaldMurillo/gofastr/framework/ui"
)

// SavedViewsHandler serves the saved-view forms' two posts, mounted by
// the host beside the entity's write routes:
//
//	POST <write base>/_views                  save the current view
//	POST <write base>/_views/_delete/{id}     delete one of the caller's
//
// The save form carries a name, the active filter text, the shown
// columns, the list's key and the list's URL to return to. The filter
// is re-parsed and re-checked against the entity's fields BEFORE
// anything is stored, and the columns with the same rule the cols param
// applies: a refused value answers 400 naming the field. The owner and
// tenant come only from the caller's context — the store enforces that,
// and nothing about a caller travels from the request into it. A
// JSON/RPC caller gets a status and a toast; a plain form post gets a
// 303 back to the list, with the new view open on save and the saved
// param dropped on delete. The body is capped, cross-site posts are
// refused, and nothing is stored cacheable.
func (u *UI) SavedViewsHandler(entityName string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		if r.Method != http.MethodPost {
			writeBulkError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		if handler.IsForgeableRequest(r) && handler.IsCrossSiteRequest(r) {
			writeBulkError(w, http.StatusForbidden, "forbidden")
			return
		}
		ctx := r.Context()
		m, err := u.meta(entityName)
		if err != nil || !m.hasAPI || u.views == nil {
			writeBulkError(w, http.StatusNotFound, "not found")
			return
		}
		if m.tr != nil {
			ctx = i18nui.WithTranslator(ctx, m.tr)
		}
		if userID(ctx) == "" {
			// The store refuses a caller with no user; say it here, in
			// the handler's own status, rather than racing the store.
			writeBulkError(w, http.StatusForbidden, "sign in to save views")
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, savedViewBodyLimit)
		body, jsonCall, err := decodeSavedViewBody(r)
		if err != nil {
			if _, tooBig := errors.AsType[*http.MaxBytesError](err); tooBig {
				writeBulkError(w, http.StatusRequestEntityTooLarge, "request body too large")
				return
			}
			writeBulkError(w, http.StatusBadRequest, "invalid request body")
			return
		}
		back, ok := scrubBackPath(body.Back)
		if !jsonCall && !ok {
			// A plain post has no script to navigate it home.
			writeBulkError(w, http.StatusBadRequest, "missing return path")
			return
		}
		// The delete route is the one that carries an id.
		if r.PathValue("id") != "" {
			u.deleteSavedView(ctx, w, r, m, body, back, ok, jsonCall)
			return
		}
		u.createSavedView(ctx, w, r, m, body, back, ok, jsonCall)
	})
}

// savedViewBodyLimit caps a saved-view post: a name, a filter text, a
// column list, a key and a return path.
const savedViewBodyLimit = 16 << 10

// savedViewBody is what the two forms post.
type savedViewBody struct {
	Name   string `json:"name"`
	Filter string `json:"filter"`
	Cols   string `json:"cols"`
	Back   string `json:"back"`
	Key    string `json:"key"`
}

// decodeSavedViewBody reads the body either way the runtime sends it:
// strict JSON for the form RPC, a plain form post without script.
func decodeSavedViewBody(r *http.Request) (savedViewBody, bool, error) {
	if handler.IsJSONContentType(r.Header.Get("Content-Type")) {
		var body savedViewBody
		if err := handler.DecodeStrict(r.Body, &body); err != nil {
			return savedViewBody{}, true, err
		}
		return body, true, nil
	}
	if err := r.ParseForm(); err != nil {
		return savedViewBody{}, false, err
	}
	f := r.PostForm
	return savedViewBody{
		Name:   f.Get("name"),
		Filter: f.Get("filter"),
		Cols:   f.Get("cols"),
		Back:   f.Get("back"),
		Key:    f.Get("key"),
	}, false, nil
}

// createSavedView stores the posted view and answers: 303 to the list
// with the new view open (plain), or a toast and the new id (RPC).
func (u *UI) createSavedView(ctx context.Context, w http.ResponseWriter, r *http.Request, m *meta, body savedViewBody, back string, backOK, jsonCall bool) {
	name := strings.TrimSpace(body.Name)
	filter := strings.TrimSpace(body.Filter)
	var cols []string
	if strings.TrimSpace(body.Cols) != "" {
		cols = strings.Split(body.Cols, ",")
		for i, c := range cols {
			cols[i] = strings.TrimSpace(c)
		}
	}
	// Re-checked before anything is stored, with the same rules the
	// open path applies, so a saved view never holds text that cannot
	// run against the entity's fields.
	s := &listState{m: m, key: body.Key, p: listParamsFor(body.Key)}
	if filter != "" {
		if _, err := dsl.ParsePredicate(filter, m.e.GetFields()); err != nil {
			writeBulkError(w, http.StatusBadRequest, i18nui.T(ctx, i18nui.KeyEntitySavedBadFilter))
			return
		}
	}
	if len(cols) > 0 && !s.validColsNames(cols) {
		writeBulkError(w, http.StatusBadRequest, i18nui.T(ctx, i18nui.KeyEntitySavedBadCols))
		return
	}
	v, err := u.views.Create(ctx, SavedView{Entity: m.name, Name: name, Filter: filter, Columns: cols})
	if err != nil {
		writeSavedViewError(w, err)
		return
	}
	if jsonCall {
		ui.AddToast(w, ui.ToastTrigger{Variant: ui.StatusSuccess, TTL: 6000, Title: i18nui.T(ctx, i18nui.KeyEntitySavedSaved)})
		writeBulkJSON(w, http.StatusOK, map[string]any{"id": v.ID})
		return
	}
	if !backOK {
		writeBulkError(w, http.StatusBadRequest, "missing return path")
		return
	}
	// The rebuilt URL is re-scrubbed at the sink: the store's id rides
	// it, and the sink is what must be clean.
	loc, locOK := scrubBackPath(withSavedParam(back, body.Key, v.ID))
	if !locOK {
		writeBulkError(w, http.StatusBadRequest, "missing return path")
		return
	}
	http.Redirect(w, r, loc, http.StatusSeeOther)
}

// deleteSavedView removes one of the caller's views and answers: 303 to
// the list with no saved view open (plain), or a toast (RPC).
func (u *UI) deleteSavedView(ctx context.Context, w http.ResponseWriter, r *http.Request, m *meta, body savedViewBody, back string, backOK, jsonCall bool) {
	id := r.PathValue("id")
	if id == "" {
		writeBulkError(w, http.StatusNotFound, "not found")
		return
	}
	if err := u.views.Delete(ctx, m.name, id); err != nil {
		if errors.Is(err, ErrSavedViewNotFound) {
			writeBulkError(w, http.StatusNotFound, "not found")
			return
		}
		slog.ErrorContext(ctx, "entityui: saved view delete", "entity", m.name, "error", err)
		writeBulkError(w, http.StatusInternalServerError, "delete failed")
		return
	}
	if jsonCall {
		ui.AddToast(w, ui.ToastTrigger{Variant: ui.StatusSuccess, TTL: 6000, Title: i18nui.T(ctx, i18nui.KeyEntitySavedDeleted)})
		writeBulkJSON(w, http.StatusOK, map[string]any{"ok": true})
		return
	}
	if !backOK {
		writeBulkError(w, http.StatusBadRequest, "missing return path")
		return
	}
	// The rebuilt URL is re-scrubbed at the sink, like the save's.
	loc, locOK := scrubBackPath(withoutSavedParam(back, body.Key))
	if !locOK {
		writeBulkError(w, http.StatusBadRequest, "missing return path")
		return
	}
	http.Redirect(w, r, loc, http.StatusSeeOther)
}

// writeSavedViewError maps the store's refusals onto statuses.
func writeSavedViewError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ErrSavedViewBlank):
		writeBulkError(w, http.StatusBadRequest, "a saved view needs a name")
	case errors.Is(err, ErrSavedViewExists):
		writeBulkError(w, http.StatusConflict, "a saved view with that name exists")
	case errors.Is(err, ErrSavedViewCap):
		writeBulkError(w, http.StatusConflict, "too many saved views")
	case errors.Is(err, ErrSavedViewTooLong):
		writeBulkError(w, http.StatusBadRequest, "saved view name, filter or columns too long")
	case errors.Is(err, ErrSavedViewNotFound):
		writeBulkError(w, http.StatusNotFound, "not found")
	default:
		writeBulkError(w, http.StatusInternalServerError, "save failed")
	}
}

// withSavedParam is back with the list's saved param set to id.
func withSavedParam(back, key, id string) string {
	path, query := splitBack(back)
	q := parseBackQuery(query)
	q.Set(param(key, "saved"), id)
	return listHref(path, q)
}

// withoutSavedParam is back with the list's saved param dropped.
func withoutSavedParam(back, key string) string {
	path, query := splitBack(back)
	q := parseBackQuery(query)
	q.Del(param(key, "saved"))
	return listHref(path, q)
}

// splitBack cuts a return path at its question mark.
func splitBack(back string) (path, query string) {
	if i := strings.IndexByte(back, '?'); i >= 0 {
		return back[:i], back[i+1:]
	}
	return back, ""
}

// parseBackQuery reads a return path's query; a bad one reads as empty,
// so the answer keeps the path and drops nothing else.
func parseBackQuery(query string) url.Values {
	q, err := url.ParseQuery(query)
	if err != nil {
		return url.Values{}
	}
	return q
}
