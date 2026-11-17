package entityui

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"path"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/DonaldMurillo/gofastr/core-ui/interactive"
	"github.com/DonaldMurillo/gofastr/core/handler"
	"github.com/DonaldMurillo/gofastr/core/i18n"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework/access"
	"github.com/DonaldMurillo/gofastr/framework/crud"
	"github.com/DonaldMurillo/gofastr/framework/i18nui"
	"github.com/DonaldMurillo/gofastr/framework/ui"
)

const overrideReasonMax = 500

const overrideBodyLimit = 8 << 10

const (
	overrideStateField  = "state"
	overrideReasonField = "reason"
)

func overrideCapability(m *meta) access.Permission {
	return access.Permission(m.name + ":override_state")
}

func (b *RecordBuilder) mayOverride(ctx context.Context, m *meta, id string) bool {
	if !b.override || !hasEnforcedStates(m) || b.ui.host.Audit() == nil {
		return false
	}
	return access.CanResourceExact(ctx, overrideCapability(m), access.Ref{Type: m.name, ID: id}) &&
		m.ch.CanUpdateRecordScoped(ctx, id)
}

func hasEnforcedStates(m *meta) bool {
	return m.states != nil && !m.states.Advisory
}

func stateValues(m *meta) []string {
	for _, f := range m.e.GetFields() {
		if f.Name == m.states.Field {
			return f.Values
		}
	}
	return nil
}

func (b *RecordBuilder) overridePanel(ctx context.Context, m *meta, row map[string]any, base string) render.HTML {
	if !b.mayOverride(ctx, m, b.id) {
		return ""
	}
	current := cell(m.rowValue(row, m.states.Field))
	opts := make([]ui.SelectOption, 0, len(stateValues(m)))
	for _, v := range stateValues(m) {
		opts = append(opts, ui.SelectOption{
			Value:    v,
			Text:     m.valueLabel(ctx, m.states.Field, v),
			Selected: v == current,
		})
	}
	action := m.api + "/" + m.idPath(b.id) + "/_override"
	back := under(base, m.idPath(b.id))
	form := ui.Form(ui.FormConfig{
		Ctx:    ctx,
		Action: action,
		Method: http.MethodPost,
		ID:     "eui-" + m.name + "-override",
		ExtraAttrs: interactive.Post(action).
			WithConfirmDialog(interactive.Confirm{
				Title:   i18nui.EntityOverrideTitle.Text(ctx, map[string]any{"entity": m.noun(ctx, false)}),
				Message: i18nui.EntityOverrideConfirm.Text(ctx),
				Accept:  i18nui.EntityOverride.Text(ctx),
			}).
			OnSuccessToast(i18nui.EntityOverrideDone.Text(ctx)).
			OnSuccess(interactive.Navigate(back)).
			Attrs(),
	},
		hiddenInput("back", back),
		ui.Select(ui.SelectConfig{Ctx: ctx,
			Name:     overrideStateField,
			Label:    i18nui.EntityOverrideState.Text(ctx),
			Options:  opts,
			Required: true,
		}),
		ui.TextArea(ui.TextAreaConfig{
			Ctx:       ctx,
			Name:      overrideReasonField,
			Label:     i18nui.EntityOverrideReason.Text(ctx),
			Required:  true,
			MaxLength: overrideReasonMax,
			Rows:      3,
			Help:      i18nui.EntityOverrideHelp.Text(ctx),
		}),
	)
	return ui.Collapsible(ui.CollapsibleConfig{
		Summary: i18nui.EntityOverride.Text(ctx),
		ID:      "eui-" + m.name + "-override-disclosure",
	}, form)
}

// OverrideHandler serves POST <write base>/{id}/_override, the route the
// override form posts to; the host mounts it beside the entity's other
// write routes. A JSON post gets a status, a toast header and a JSON
// body; a plain form post gets a 303 to its same-origin back path, or an
// error page. An entity without enforced states answers 404, a caller
// without <entity>:override_state 403, and an unaudited entity 409.
func (u *UI) OverrideHandler(entityName string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		if r.Method != http.MethodPost {
			w.Header().Set("Allow", http.MethodPost)
			writeBulkError(w, r, http.StatusMethodNotAllowed, i18n.StatusPhrase(http.StatusMethodNotAllowed))
			return
		}
		if handler.IsCrossSiteRequest(r) {
			writeBulkError(w, r, http.StatusForbidden, reasonAccessDenied)
			return
		}
		m, err := u.meta(entityName)
		if err != nil || !m.hasAPI || !hasEnforcedStates(m) {
			writeBulkError(w, r, http.StatusNotFound, i18n.StatusPhrase(http.StatusNotFound))
			return
		}
		id := crud.PathRecordID(r, m.keys)
		if len(m.keys) == 1 && id == "" {
			id = overrideID(r)
		}
		if id == "" {
			writeBulkError(w, r, http.StatusNotFound, i18n.StatusPhrase(http.StatusNotFound))
			return
		}
		ctx := r.Context()
		if m.tr != nil {
			ctx = i18n.WithTranslator(ctx, m.tr)
		}
		if !access.CanResourceExact(ctx, overrideCapability(m), access.Ref{Type: m.name, ID: id}) {
			writeOverrideError(w, r, ctx, http.StatusForbidden, reasonOverrideDenied, map[string]any{"entity": m.singular(ctx)})
			return
		}
		plain := !handler.IsJSONContentType(r.Header.Get("Content-Type"))
		if plain && !isFormContentType(r.Header.Get("Content-Type")) {
			writeBulkError(w, r, http.StatusUnsupportedMediaType, i18n.StatusPhrase(http.StatusUnsupportedMediaType))
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, overrideBodyLimit)
		state, reason, back, err := readOverrideBody(r, plain)
		if err != nil {
			if _, tooBig := errors.AsType[*http.MaxBytesError](err); tooBig {
				writeBulkError(w, r, http.StatusRequestEntityTooLarge, i18n.StatusPhrase(http.StatusRequestEntityTooLarge))
				return
			}
			writeBulkError(w, r, http.StatusBadRequest, i18n.ReasonInvalidJSONBody)
			return
		}
		if plain {
			var ok bool
			if back, ok = scrubBackPath(back); !ok {
				writeBulkError(w, r, http.StatusBadRequest, reasonInvalidReturnPath)
				return
			}
		}
		if !m.ch.CanReadRecordScoped(ctx, id) {
			writeBulkError(w, r, http.StatusNotFound, i18n.StatusPhrase(http.StatusNotFound))
			return
		}
		if _, err := m.getRow(ctx, id); err != nil {
			writeBulkError(w, r, http.StatusNotFound, i18n.StatusPhrase(http.StatusNotFound))
			return
		}
		if !m.ch.CanUpdateRecordScoped(ctx, id) {
			writeOverrideError(w, r, ctx, http.StatusForbidden, reasonOverrideDenied, map[string]any{"entity": m.singular(ctx)})
			return
		}
		if !slices.Contains(stateValues(m), state) {
			writeOverrideError(w, r, ctx, http.StatusBadRequest, reasonOverrideStateBad)
			return
		}
		reason = strings.TrimSpace(reason)
		if reason == "" {
			writeOverrideError(w, r, ctx, http.StatusBadRequest, reasonOverrideBlank)
			return
		}
		if utf8.RuneCountInString(reason) > overrideReasonMax {
			writeOverrideError(w, r, ctx, http.StatusBadRequest, reasonOverrideLong)
			return
		}
		if _, err := m.ch.UpdateOne(crud.WithReadHooks(crud.WithStateOverride(ctx, reason)), id, map[string]any{m.states.Field: state}); err != nil {
			writeOverrideRefusal(w, r, ctx, m, err)
			return
		}
		if plain {
			http.Redirect(w, r, back, http.StatusSeeOther)
			return
		}
		ui.AddToast(w, ui.ToastTrigger{Variant: ui.StatusSuccess, Title: i18nui.EntityOverrideDone.Text(ctx)})
		writeBulkJSON(w, http.StatusOK, map[string]any{"status": "ok", "state": state})
	})
}

func overrideID(r *http.Request) string {
	p := strings.TrimSuffix(r.URL.Path, "/")
	if path.Base(p) != "_override" {
		return ""
	}
	id := path.Base(path.Dir(p))
	if id == "" || id == "." || id == "/" || id == "_override" || strings.Contains(id, "/") {
		return ""
	}
	return id
}

type overrideBody struct {
	State  string `json:"state"`
	Reason string `json:"reason"`
	Back   string `json:"back"`
}

func readOverrideBody(r *http.Request, plain bool) (state, reason, back string, err error) {
	if !plain {
		var body overrideBody
		if err := handler.DecodeStrict(r.Body, &body); err != nil {
			return "", "", "", err
		}
		return body.State, body.Reason, body.Back, nil
	}
	if err := r.ParseForm(); err != nil {
		return "", "", "", err
	}
	return r.PostFormValue(overrideStateField), r.PostFormValue(overrideReasonField), r.PostFormValue("back"), nil
}

func isFormContentType(ct string) bool {
	return strings.HasPrefix(strings.ToLower(strings.TrimSpace(ct)), "application/x-www-form-urlencoded")
}

func writeOverrideError(w http.ResponseWriter, r *http.Request, ctx context.Context, status int, reason i18n.Phrase, params ...map[string]any) {
	if handler.IsJSONContentType(r.Header.Get("Content-Type")) {
		writeBulkError(w, r, status, reason, params...)
		return
	}
	msg := reason.Text(ctx, params...)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	body := ui.Callout(ui.CalloutConfig{Title: i18nui.EntityOverride.Text(ctx), Variant: ui.StatusDanger}, render.Text(msg))
	_, _ = w.Write([]byte(body))
}

func writeOverrideRefusal(w http.ResponseWriter, r *http.Request, ctx context.Context, m *meta, err error) {
	switch {
	case errors.Is(err, crud.ErrStateOverrideUnaudited):
		writeOverrideError(w, r, ctx, http.StatusConflict, reasonOverrideNoAudit)
	case errors.Is(err, crud.ErrStateOverrideNoReason):
		writeOverrideError(w, r, ctx, http.StatusBadRequest, reasonOverrideBlank)
	case errors.Is(err, crud.ErrForbidden):
		writeOverrideError(w, r, ctx, http.StatusForbidden, reasonOverrideDenied, map[string]any{"entity": m.singular(ctx)})
	case crud.IsNotFound(err):
		writeBulkError(w, r, http.StatusNotFound, i18n.StatusPhrase(http.StatusNotFound))
	case isWriteInvalid(err):
		writeOverrideError(w, r, ctx, http.StatusUnprocessableEntity, reasonOverrideFailed)
	case crud.IsHookRefusal(err):
		writeOverrideError(w, r, ctx, http.StatusBadRequest, reasonOverrideFailed)
	default:
		slog.ErrorContext(ctx, "entityui: state override", "entity", m.name, "error", err)
		writeOverrideError(w, r, ctx, http.StatusInternalServerError, reasonOverrideFailed)
	}
}

func isWriteInvalid(err error) bool {
	if _, ok := errors.AsType[*crud.ValidationError](err); ok {
		return true
	}
	_, ok := errors.AsType[*crud.StateError](err)
	return ok
}
