package entityui

import (
	"context"
	"errors"
	"log/slog"
	"net/http"

	"github.com/DonaldMurillo/gofastr/core-ui/interactive"
	"github.com/DonaldMurillo/gofastr/core/handler"
	"github.com/DonaldMurillo/gofastr/core/i18n"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework/crud"
	"github.com/DonaldMurillo/gofastr/framework/i18nui"
	"github.com/DonaldMurillo/gofastr/framework/ui"
)

const deletedViewKey = "deleted"

func (b *ListBuilder) deletedActions(ctx context.Context, s *listState, row map[string]any) render.HTML {
	m := s.m
	id := m.rowID(row)
	back := listHref(s.path, s.q)
	var forms []render.HTML
	if canUpdate(ctx, m, id) {
		forms = append(forms, deletedWriteForm(ctx,
			m.api+"/"+m.idPath(id)+"/_restore",
			i18nui.EntityRestore.Text(ctx), ui.ButtonSecondary, interactive.Confirm{},
			back,
		))
	}
	if canDelete(ctx, m, id) {
		forms = append(forms, deletedWriteForm(ctx,
			m.api+"/"+m.idPath(id)+"/_purge",
			i18nui.EntityPurge.Text(ctx), ui.ButtonDanger,
			interactive.Confirm{
				Title:   i18nui.EntityPurgeTitle.Text(ctx, map[string]any{"entity": m.noun(ctx, false)}),
				Message: i18nui.EntityPurgeConfirm.Text(ctx),
				Accept:  i18nui.EntityPurge.Text(ctx),
				Danger:  true,
			},
			back,
		))
	}
	if len(forms) == 0 {
		return ""
	}
	return ui.Cluster(ui.ClusterConfig{Gap: ui.GapXS, Align: ui.AlignCenter, NoWrap: true}, forms...)
}

func deletedWriteForm(ctx context.Context, action, label string, variant ui.ButtonVariant, confirm interactive.Confirm, back string) render.HTML {
	rpc := interactive.Post(action).
		OnSuccess(interactive.Navigate(back))
	if confirm.Message != "" {
		rpc = rpc.WithConfirmDialog(confirm)
	}
	return ui.Form(ui.FormConfig{
		Action:     action,
		Method:     http.MethodPost,
		Ctx:        ctx,
		HideSubmit: true,
		ExtraAttrs: rpc.Attrs(),
	},
		hiddenInput("back", back),
		ui.Button(ui.ButtonConfig{Ctx: ctx, Label: label, Type: "submit", Variant: variant}),
	)
}

const deletedBodyLimit = 8 << 10

// RestoreHandler serves POST <write base>/<id>/_restore under the caller's
// context. A JSON caller gets a toast and a status, a form post a 303 to
// its same-origin back path. Forbidden answers 403, a row out of scope or
// an entity without soft delete 404, a live row 409.
func (u *UI) RestoreHandler(entityName string) http.Handler {
	return u.serveDeletedWrite(entityName, "restore")
}

// PurgeHandler serves POST <write base>/<id>/_purge: it hard-deletes an
// already soft-deleted record (a live row answers 409); answers as RestoreHandler.
func (u *UI) PurgeHandler(entityName string) http.Handler {
	return u.serveDeletedWrite(entityName, "purge")
}

func (u *UI) serveDeletedWrite(entityName, op string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		if r.Method != http.MethodPost {
			writeBulkError(w, r, http.StatusMethodNotAllowed, i18n.StatusPhrase(http.StatusMethodNotAllowed))
			return
		}
		if refuseCrossSitePost(w, r, i18n.StatusPhrase(http.StatusForbidden)) {
			return
		}
		ctx := crud.WithReadHooks(r.Context())
		m, err := u.meta(entityName)
		if err != nil || !m.hasAPI || !m.e.Config.Scope.SoftDelete {
			writeBulkError(w, r, http.StatusNotFound, i18n.StatusPhrase(http.StatusNotFound))
			return
		}
		if m.tr != nil {
			ctx = i18n.WithTranslator(ctx, m.tr)
		}
		if !requireRead(w, r, ctx, m) {
			return
		}
		id := crud.PathRecordID(r, m.keys)
		if id == "" {
			writeBulkError(w, r, http.StatusNotFound, i18n.StatusPhrase(http.StatusNotFound))
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, deletedBodyLimit)
		jsonCall := handler.IsJSONContentType(r.Header.Get("Content-Type"))
		var back string
		var haveBack bool
		if jsonCall {
			var body struct {
				Back string `json:"back"`
			}
			if err := handler.DecodeStrict(r.Body, &body); err != nil {
				if _, tooBig := errors.AsType[*http.MaxBytesError](err); tooBig {
					writeBulkError(w, r, http.StatusRequestEntityTooLarge, i18n.StatusPhrase(http.StatusRequestEntityTooLarge))
					return
				}
				writeBulkError(w, r, http.StatusBadRequest, i18n.ReasonInvalidJSONBody)
				return
			}
			back, haveBack = scrubBackPath(body.Back)
		} else {
			if err := r.ParseForm(); err != nil {
				if _, tooBig := errors.AsType[*http.MaxBytesError](err); tooBig {
					writeBulkError(w, r, http.StatusRequestEntityTooLarge, i18n.StatusPhrase(http.StatusRequestEntityTooLarge))
					return
				}
				writeBulkError(w, r, http.StatusBadRequest, i18n.ReasonInvalidJSONBody)
				return
			}
			back, haveBack = scrubBackPath(r.PostFormValue("back"))
			if !haveBack {
				writeBulkError(w, r, http.StatusBadRequest, reasonMissingReturnPath)
				return
			}
		}
		if visible, err := u.hookedRowVisible(ctx, m, id); err != nil {
			slog.ErrorContext(ctx, "entityui: trash write read", "entity", m.name, "op", op, "error", err)
			writeBulkError(w, r, http.StatusInternalServerError, reasonWriteFailed)
			return
		} else if !visible {
			writeBulkError(w, r, http.StatusNotFound, i18n.StatusPhrase(http.StatusNotFound))
			return
		}
		var writeErr error
		if op == "restore" {
			writeErr = m.ch.RestoreOne(crud.WithReadHooks(ctx), id)
		} else {
			writeErr = m.ch.PurgeOne(crud.WithReadHooks(ctx), id)
		}
		if writeErr != nil {
			switch {
			case errors.Is(writeErr, crud.ErrForbidden):
				writeBulkError(w, r, http.StatusForbidden, i18n.StatusPhrase(http.StatusForbidden))
			case crud.IsNotFound(writeErr), errors.Is(writeErr, crud.ErrNoSoftDelete):
				writeBulkError(w, r, http.StatusNotFound, i18n.StatusPhrase(http.StatusNotFound))
			case errors.Is(writeErr, crud.ErrNotSoftDeleted):
				writeBulkError(w, r, http.StatusConflict, reasonNotDeleted)
			default:
				slog.ErrorContext(ctx, "entityui: trash write", "entity", m.name, "op", op, "error", writeErr)
				writeBulkError(w, r, http.StatusInternalServerError, reasonWriteFailed)
			}
			return
		}
		if jsonCall {
			ui.AddToast(w, ui.ToastTrigger{Variant: ui.StatusSuccess, TTL: 6000, Title: trashToast(ctx, m, op)})
			writeBulkJSON(w, http.StatusOK, map[string]any{"ok": true})
			return
		}
		http.Redirect(w, r, back, http.StatusSeeOther)
	})
}

func (u *UI) hookedRowVisible(ctx context.Context, m *meta, id string) (bool, error) {
	for _, deleted := range []bool{true, false} {
		ids, err := u.readIDs(ctx, m, []string{id}, deleted)
		if err != nil {
			return false, err
		}
		if len(ids) > 0 {
			return true, nil
		}
	}
	return false, nil
}

func trashToast(ctx context.Context, m *meta, op string) string {
	if op == "restore" {
		return i18nui.EntityRestored.Text(ctx, map[string]any{"entity": m.singular(ctx)})
	}
	return i18nui.EntityPurged.Text(ctx, map[string]any{"entity": m.singular(ctx)})
}

func scrubBackPath(s string) (string, bool) {
	if len(s) > 2048 || !handler.IsSafeRelativePath(s) {
		return "", false
	}
	for i := range len(s) {
		if s[i] < 0x20 || s[i] == 0x7f {
			return "", false
		}
	}
	return s, true
}
