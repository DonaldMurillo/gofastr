package entityui

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/url"

	"github.com/DonaldMurillo/gofastr/core-ui/interactive"
	"github.com/DonaldMurillo/gofastr/core/handler"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework/crud"
	"github.com/DonaldMurillo/gofastr/framework/i18nui"
	"github.com/DonaldMurillo/gofastr/framework/ui"
)

// The trash view. A soft-deleting entity's list offers ?view=deleted
// beside its views: the same list, narrowed to soft-deleted rows by
// crud.ListOptions.Deleted, under the same owner, tenant and read scope
// the live list reads under. There is no bulk bar, no New and no record
// link — the record screens read live rows only, so the title is text —
// and each row carries the two trash actions: Restore, and Delete
// permanently behind a confirm.
//
// The actions post to the host's write base for the entity,
// <write base>/<id>/_restore and <write base>/<id>/_purge, served by
// RestoreHandler and PurgeHandler. Each button is its own small POST
// form carrying the list's URL as its return path, so a scriptless
// browser still works: the form posts plainly and the answer is a 303
// back to the Deleted view; with script the runtime turns the submit
// into the form RPC, toasting the result and re-fetching the page.

// deletedViewKey is the reserved view key of the trash view (entity's
// reservedDisplayKeys keeps a Display view from taking it).
const deletedViewKey = "deleted"

// deletedActions draws one trashed row's actions: Restore where the
// caller may update the record, Delete permanently where they may
// delete it. A row neither gate allows draws nothing.
func (b *ListBuilder) deletedActions(ctx context.Context, s *listState, row map[string]any) render.HTML {
	m := s.m
	id := cell(rowValue(row, m.pk))
	back := listHref(s.path, s.q)
	var forms []render.HTML
	if canUpdate(ctx, m, id) {
		forms = append(forms, deletedWriteForm(ctx,
			m.api+"/"+url.PathEscape(id)+"/_restore",
			i18nui.T(ctx, i18nui.KeyEntityRestore), ui.ButtonSecondary, interactive.Confirm{},
			i18nui.TVars(ctx, i18nui.KeyEntityRestored, map[string]string{"entity": m.singular(ctx)}),
			back,
		))
	}
	if canDelete(ctx, m, id) {
		forms = append(forms, deletedWriteForm(ctx,
			m.api+"/"+url.PathEscape(id)+"/_purge",
			i18nui.T(ctx, i18nui.KeyEntityPurge), ui.ButtonDanger,
			interactive.Confirm{
				Title:   i18nui.TVars(ctx, i18nui.KeyEntityPurgeTitle, map[string]string{"entity": m.noun(ctx, false)}),
				Message: i18nui.T(ctx, i18nui.KeyEntityPurgeConfirm),
				Accept:  i18nui.T(ctx, i18nui.KeyEntityPurge),
				Danger:  true,
			},
			i18nui.TVars(ctx, i18nui.KeyEntityPurged, map[string]string{"entity": m.singular(ctx)}),
			back,
		))
	}
	if len(forms) == 0 {
		return ""
	}
	return ui.Cluster(ui.ClusterConfig{Gap: ui.GapXS, Align: ui.AlignCenter}, forms...)
}

// deletedWriteForm is one trash action: a POST form whose submit
// carries the RPC wiring (a confirm on the purge), the return path as
// its one field, and a button the scriptless path can press.
func deletedWriteForm(ctx context.Context, action, label string, variant ui.ButtonVariant, confirm interactive.Confirm, toast, back string) render.HTML {
	rpc := interactive.Post(action).
		OnSuccessToast(toast).
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
		ui.Button(ui.ButtonConfig{Label: label, Type: "submit", Variant: variant}),
	)
}

// deletedBodyLimit caps a restore or purge post: a return path and
// nothing else.
const deletedBodyLimit = 8 << 10

// RestoreHandler serves POST <write base>/<id>/_restore, the Deleted
// view's Restore action. It restores the record through the entity's
// CRUD handler under the CALLER's context, so permission, the Decider,
// owner and tenant scope are the write route's own gates. A JSON/RPC
// caller (the form RPC) gets a toast and a status; a plain form post
// gets a 303 back to the list's Deleted view — a return path the form
// carried, validated as a same-origin relative path, never an absolute
// URL. Forbidden answers 403, another owner's or tenant's id 404, a
// live row 409, and an entity without soft delete 404.
func (u *UI) RestoreHandler(entityName string) http.Handler {
	return u.serveDeletedWrite(entityName, "restore")
}

// PurgeHandler serves POST <write base>/<id>/_purge, the Deleted view's
// Delete permanently action. It hard-deletes a record that is ALREADY
// soft-deleted (a live row answers 409 and is not touched) under the
// caller's own context; the answer shapes are RestoreHandler's.
func (u *UI) PurgeHandler(entityName string) http.Handler {
	return u.serveDeletedWrite(entityName, "purge")
}

// serveDeletedWrite is the one body behind RestoreHandler and
// PurgeHandler.
func (u *UI) serveDeletedWrite(entityName, op string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		if r.Method != http.MethodPost {
			writeBulkError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		// A cross-site form must not reach the write: the forgeable
		// content types are the ones a form can send without a
		// preflight, and the one cross-site predicate decides.
		if handler.IsForgeableRequest(r) && handler.IsCrossSiteRequest(r) {
			writeBulkError(w, http.StatusForbidden, "forbidden")
			return
		}
		ctx := r.Context()
		m, err := u.meta(entityName)
		if err != nil || !m.hasAPI || !m.e.Config.Scope.SoftDelete {
			writeBulkError(w, http.StatusNotFound, "not found")
			return
		}
		if m.tr != nil {
			ctx = i18nui.WithTranslator(ctx, m.tr)
		}
		if !canRead(ctx, m.ch) {
			writeBulkError(w, http.StatusForbidden, "access denied")
			return
		}
		id := r.PathValue("id")
		if id == "" {
			writeBulkError(w, http.StatusNotFound, "not found")
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
					writeBulkError(w, http.StatusRequestEntityTooLarge, "request body too large")
					return
				}
				writeBulkError(w, http.StatusBadRequest, "invalid request body")
				return
			}
			back, haveBack = scrubBackPath(body.Back)
		} else {
			if err := r.ParseForm(); err != nil {
				if _, tooBig := errors.AsType[*http.MaxBytesError](err); tooBig {
					writeBulkError(w, http.StatusRequestEntityTooLarge, "request body too large")
					return
				}
				writeBulkError(w, http.StatusBadRequest, "invalid request body")
				return
			}
			back, haveBack = scrubBackPath(r.PostFormValue("back"))
			// A plain post has no script to navigate it home: no valid
			// return path, no write.
			if !haveBack {
				writeBulkError(w, http.StatusBadRequest, "missing return path")
				return
			}
		}
		var writeErr error
		if op == "restore" {
			writeErr = m.ch.RestoreOne(ctx, id)
		} else {
			writeErr = m.ch.PurgeOne(ctx, id)
		}
		if writeErr != nil {
			switch {
			case errors.Is(writeErr, crud.ErrForbidden):
				writeBulkError(w, http.StatusForbidden, "forbidden")
			case crud.IsNotFound(writeErr), errors.Is(writeErr, crud.ErrNoSoftDelete):
				writeBulkError(w, http.StatusNotFound, "not found")
			case errors.Is(writeErr, crud.ErrNotSoftDeleted):
				writeBulkError(w, http.StatusConflict, "not deleted")
			default:
				slog.ErrorContext(ctx, "entityui: trash write", "entity", m.name, "op", op, "error", writeErr)
				writeBulkError(w, http.StatusInternalServerError, "write failed")
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

// trashToast is the answer's toast title.
func trashToast(ctx context.Context, m *meta, op string) string {
	if op == "restore" {
		return i18nui.TVars(ctx, i18nui.KeyEntityRestored, map[string]string{"entity": m.singular(ctx)})
	}
	return i18nui.TVars(ctx, i18nui.KeyEntityPurged, map[string]string{"entity": m.singular(ctx)})
}

// scrubBackPath validates a form-carried return path: it must name a
// path on this origin (handler.IsSafeRelativePath: no scheme or host, no
// backslash, no "//" even after percent-decoding), carry no control
// bytes and fit a URL, so a cross-site post cannot aim the answer's
// redirect at another origin. The scrubbed value is the one that
// reaches the Location header.
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
