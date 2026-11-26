package entityui

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/url"
	"strings"

	"github.com/DonaldMurillo/gofastr/core/handler"
	"github.com/DonaldMurillo/gofastr/core/i18n"
	"github.com/DonaldMurillo/gofastr/core/textsafe"
	"github.com/DonaldMurillo/gofastr/framework/access"
	"github.com/DonaldMurillo/gofastr/framework/dsl"
	"github.com/DonaldMurillo/gofastr/framework/i18nui"
	"github.com/DonaldMurillo/gofastr/framework/ui"
)

// SavedViewsHandler serves the saved-view forms' posts:
//
//	POST <write base>/_views                  save the current view
//	POST <write base>/_views/_delete?id=<id>  delete one of the caller's
//
// The filter and columns are re-checked before anything is stored (400
// naming the field). A JSON caller gets a status and a toast, a form post
// a 303 back to the list.
func (u *UI) SavedViewsHandler(entityName string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		if r.Method != http.MethodPost {
			w.Header().Set("Allow", http.MethodPost)
			writeBulkError(w, r, http.StatusMethodNotAllowed, i18n.StatusPhrase(http.StatusMethodNotAllowed))
			return
		}
		if refuseCrossSitePost(w, r, i18n.StatusPhrase(http.StatusForbidden)) {
			return
		}
		ctx := r.Context()
		m, err := u.meta(entityName)
		if err != nil || !m.hasAPI || u.views == nil {
			writeBulkError(w, r, http.StatusNotFound, i18n.StatusPhrase(http.StatusNotFound))
			return
		}
		if m.tr != nil {
			ctx = i18n.WithTranslator(ctx, m.tr)
		}
		if access.UserID(ctx) == "" {
			writeBulkError(w, r, http.StatusForbidden, reasonViewSignIn)
			return
		}
		if !requireRead(w, r, ctx, m) {
			return
		}
		body, jsonCall, ok := decodeSavedViewBody(w, r)
		if !ok {
			return
		}
		back, ok := scrubBackPath(body.Back)
		if !jsonCall && !ok {
			writeBulkError(w, r, http.StatusBadRequest, reasonMissingReturnPath)
			return
		}
		if strings.HasSuffix(r.URL.Path, "/_views/_delete") {
			u.deleteSavedView(ctx, w, r, m, body, back, ok, jsonCall)
			return
		}
		u.createSavedView(ctx, w, r, m, body, back, ok, jsonCall)
	})
}

const savedViewBodyLimit = 16 << 10

type savedViewBody struct {
	Name   string `json:"name"`
	Filter string `json:"filter"`
	Cols   string `json:"cols"`
	Back   string `json:"back"`
	Key    string `json:"key"`
}

func decodeSavedViewBody(w http.ResponseWriter, r *http.Request) (body savedViewBody, jsonCall, ok bool) {
	if handler.IsJSONContentType(r.Header.Get("Content-Type")) {
		if !decodeBody(w, r, savedViewBodyLimit, &body) {
			return savedViewBody{}, true, false
		}
		return body, true, true
	}
	if !parseBodyForm(w, r, savedViewBodyLimit) {
		return savedViewBody{}, false, false
	}
	f := r.PostForm
	body = savedViewBody{
		Name:   f.Get("name"),
		Filter: f.Get("filter"),
		Cols:   f.Get("cols"),
		Back:   f.Get("back"),
		Key:    f.Get("key"),
	}
	return body, false, true
}

func (u *UI) createSavedView(ctx context.Context, w http.ResponseWriter, r *http.Request, m *meta, body savedViewBody, back string, backOK, jsonCall bool) {
	name := strings.TrimSpace(body.Name)
	filter := strings.TrimSpace(body.Filter)
	if !textsafe.Storable(name) || !textsafe.Storable(filter) {
		writeBulkError(w, r, http.StatusBadRequest, reasonSavedBadText)
		return
	}
	var cols []string
	if strings.TrimSpace(body.Cols) != "" {
		cols = strings.Split(body.Cols, ",")
		for i, c := range cols {
			cols[i] = strings.TrimSpace(c)
		}
	}
	s := &listState{m: m, key: body.Key, p: listParamsFor(body.Key)}
	if filter != "" {
		if _, err := dsl.ParsePredicate(filter, m.e.GetFields()); err != nil {
			writeBulkError(w, r, http.StatusBadRequest, reasonSavedBadFilter)
			return
		}
	}
	if len(cols) > 0 && !s.validColsNames(cols) {
		writeBulkError(w, r, http.StatusBadRequest, reasonSavedBadCols)
		return
	}
	v, err := u.views.Create(ctx, SavedView{Entity: m.name, Name: name, Filter: filter, Columns: cols})
	if err != nil {
		writeSavedViewError(w, r, err)
		return
	}
	if jsonCall {
		ui.AddToast(w, ui.ToastTrigger{Variant: ui.StatusSuccess, TTL: 6000, Title: i18nui.EntitySavedSaved.Text(ctx)})
		writeBulkJSON(w, http.StatusOK, map[string]any{"id": v.ID})
		return
	}
	if !backOK {
		writeBulkError(w, r, http.StatusBadRequest, reasonMissingReturnPath)
		return
	}
	loc, locOK := scrubBackPath(withSavedParam(back, body.Key, v.ID))
	if !locOK {
		loc = back
	}
	http.Redirect(w, r, loc, http.StatusSeeOther)
}

func (u *UI) deleteSavedView(ctx context.Context, w http.ResponseWriter, r *http.Request, m *meta, body savedViewBody, back string, backOK, jsonCall bool) {
	id := r.URL.Query().Get("id")
	if id == "" || !textsafe.Storable(id) {
		writeBulkError(w, r, http.StatusNotFound, i18n.StatusPhrase(http.StatusNotFound))
		return
	}
	if err := u.views.Delete(ctx, m.name, id); err != nil {
		if errors.Is(err, ErrSavedViewNotFound) {
			writeBulkError(w, r, http.StatusNotFound, i18n.StatusPhrase(http.StatusNotFound))
			return
		}
		slog.ErrorContext(ctx, "entityui: saved view delete", "entity", m.name, "error", err)
		writeBulkError(w, r, http.StatusInternalServerError, reasonDeleteFailed)
		return
	}
	if jsonCall {
		ui.AddToast(w, ui.ToastTrigger{Variant: ui.StatusSuccess, TTL: 6000, Title: i18nui.EntitySavedDeleted.Text(ctx)})
		writeBulkJSON(w, http.StatusOK, map[string]any{"ok": true})
		return
	}
	if !backOK {
		writeBulkError(w, r, http.StatusBadRequest, reasonMissingReturnPath)
		return
	}
	loc, locOK := scrubBackPath(withoutSavedParam(back, body.Key))
	if !locOK {
		loc = back
	}
	http.Redirect(w, r, loc, http.StatusSeeOther)
}

func writeSavedViewError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, ErrSavedViewBlank):
		writeBulkError(w, r, http.StatusBadRequest, reasonViewNameRequired)
	case errors.Is(err, ErrSavedViewExists):
		writeBulkError(w, r, http.StatusConflict, reasonViewNameTaken)
	case errors.Is(err, ErrSavedViewCap):
		writeBulkError(w, r, http.StatusConflict, reasonTooManySavedViews)
	case errors.Is(err, ErrSavedViewTooLong):
		writeBulkError(w, r, http.StatusBadRequest, reasonViewTooLong)
	case errors.Is(err, ErrSavedViewNotFound):
		writeBulkError(w, r, http.StatusNotFound, i18n.StatusPhrase(http.StatusNotFound))
	default:
		writeBulkError(w, r, http.StatusInternalServerError, reasonSaveFailed)
	}
}

func withSavedParam(back, key, id string) string {
	return handler.WithQuery(back, func(q url.Values) {
		q.Set(param(key, "saved"), id)
	})
}

func withoutSavedParam(back, key string) string {
	return handler.WithQuery(back, func(q url.Values) {
		q.Del(param(key, "saved"))
	})
}
