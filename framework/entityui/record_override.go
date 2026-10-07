package entityui

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/url"
	"path"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/DonaldMurillo/gofastr/core-ui/interactive"
	"github.com/DonaldMurillo/gofastr/core/handler"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework/access"
	"github.com/DonaldMurillo/gofastr/framework/crud"
	"github.com/DonaldMurillo/gofastr/framework/i18nui"
	"github.com/DonaldMurillo/gofastr/framework/ui"
)

// The status override: a form on the record screen that writes the state
// field directly, outside the declared moves, for a caller an operator
// trusted with the capability <entity>:override_state. The write itself
// is crud's: it runs under crud.WithStateOverride through UpdateOne in
// the caller's own context, so the owner and tenant scopes, the state
// gates and the audit row (operation "state_override", the reason in the
// row's reason column) all come from the CRUD layer, never from here.
//
// The form posts to <write base>/{id}/_override (the same write base the
// transition buttons use), a route the host mounts UI.OverrideHandler
// for. It is OFF by default: RecordBuilder.Override turns it on, and it
// draws only for an entity with enforced states on an app that keeps an
// audit log.

// overrideReasonMax is the longest override reason, in characters (the
// form's maxlength and help text count the same way).
const overrideReasonMax = 500

// overrideBodyLimit caps one override post: the reason is the only long
// field, so this leaves room for nothing else.
const overrideBodyLimit = 8 << 10

// The form's field names, shared by the form and both arms of the
// handler (the runtime serializes the form as JSON with these keys, a
// scriptless browser posts them urlencoded).
const (
	overrideStateField  = "state"
	overrideReasonField = "reason"
)

// overrideCapability is the capability name the override asks for.
func overrideCapability(m *meta) access.Permission {
	return access.Permission(m.name + ":override_state")
}

// mayOverride is every gate the form needs: the builder turned it on,
// the entity declares enforced states, the app keeps an audit log, the
// caller may update the record, and the caller holds the capability
// exactly — a Wildcard grant does not satisfy it, and neither does a
// back office's elevation (CanResourceExact consults neither).
func (b *RecordBuilder) mayOverride(ctx context.Context, m *meta, id string) bool {
	if !b.override || !hasEnforcedStates(m) || b.ui.host.Audit() == nil {
		return false
	}
	return access.CanResourceExact(ctx, overrideCapability(m), access.Ref{Type: m.name, ID: id}) &&
		m.ch.CanUpdateRecordScoped(ctx, id)
}

// hasEnforcedStates reports whether m declares states the CRUD handler
// enforces: the override exists to leave the moves' rule; an Advisory
// field is already writable by a plain update.
func hasEnforcedStates(m *meta) bool {
	return m.states != nil && !m.states.Advisory
}

// stateValues lists the state field's declared values, in declared
// order. A Hidden state field still has values (the header's badge reads
// it the same way).
func stateValues(m *meta) []string {
	for _, f := range m.e.GetFields() {
		if f.Name == m.states.Field {
			return f.Values
		}
	}
	return nil
}

// overridePanel draws the override form inside a disclosure: a select of
// every declared state (the stored one marked), a required reason, and a
// submit that asks first and lands back on the record. An empty return
// draws nothing.
func (b *RecordBuilder) overridePanel(ctx context.Context, m *meta, row map[string]any, base string) render.HTML {
	if !b.mayOverride(ctx, m, b.id) {
		return ""
	}
	current := cell(rowValue(row, m.states.Field))
	opts := make([]ui.SelectOption, 0, len(stateValues(m)))
	for _, v := range stateValues(m) {
		opts = append(opts, ui.SelectOption{
			Value:    v,
			Text:     m.valueLabel(ctx, m.states.Field, v),
			Selected: v == current,
		})
	}
	action := m.api + "/" + url.PathEscape(b.id) + "/_override"
	back := base + "/" + url.PathEscape(b.id)
	form := ui.Form(ui.FormConfig{
		Action: action,
		Method: http.MethodPost,
		ID:     "eui-" + m.name + "-override",
		ExtraAttrs: interactive.Post(action).
			WithConfirm(i18nui.TVars(ctx, i18nui.KeyEntityOverrideConfirm, map[string]string{"entity": m.singular(ctx)})).
			OnSuccessToast(i18nui.T(ctx, i18nui.KeyEntityOverrideDone)).
			OnSuccess(interactive.Navigate(back)).
			Attrs(),
	},
		hiddenInput("back", back),
		ui.Select(ui.SelectConfig{
			Name:     overrideStateField,
			Label:    i18nui.T(ctx, i18nui.KeyEntityOverrideState),
			Options:  opts,
			Required: true,
		}),
		ui.TextArea(ui.TextAreaConfig{
			Name:      overrideReasonField,
			Label:     i18nui.T(ctx, i18nui.KeyEntityOverrideReason),
			Required:  true,
			MaxLength: overrideReasonMax,
			Rows:      3,
			Help:      i18nui.T(ctx, i18nui.KeyEntityOverrideHelp),
		}),
	)
	return ui.Collapsible(ui.CollapsibleConfig{
		Summary: i18nui.T(ctx, i18nui.KeyEntityOverride),
		ID:      "eui-" + m.name + "-override-disclosure",
	}, form)
}

// OverrideHandler serves POST <write base>/{id}/_override, the route the
// override form posts to; the host mounts it beside the entity's other
// write routes (the way it mounts BulkHandler). In order it:
//
//   - takes POST only, refuses a cross-site post (the one shared guard,
//     handler.IsCrossSiteRequest) and caps the body;
//   - answers 404 for an unknown entity, an entity with no write routes
//     or one without enforced states;
//   - re-checks <entity>:override_state with access.CanResourceExact on
//     the record id — a Wildcard grant and crud.WithElevation never
//     satisfy it — BEFORE reading anything;
//   - reads the record under the caller's context: a record the caller's
//     read scope does not reach answers 404, the same as a missing id,
//     and nothing changes;
//   - validates the target state against the field's declared values and
//     the reason (non-blank, at most OverrideReasonMax characters), and writes
//     with crud.UpdateOne under crud.WithStateOverride in the caller's
//     context, so the CRUD gates and the audit row ("state_override"
//     with the reason) come from crud itself. An entity no audit log
//     records is refused by crud (409 here); the write never falls back
//     to an unaudited one.
//
// A JSON caller (the form's RPC arm) gets a status, a toast header and a
// JSON body. A plain form post (a scriptless browser) gets 303 back to
// the record screen along the form-carried return path, which must be a
// same-origin relative path (scrubBackPath; anything else is a 400 and
// nothing is written), and an error answer that says what to fix.
func (u *UI) OverrideHandler(entityName string) http.Handler {
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
		if err != nil || !m.hasAPI || !hasEnforcedStates(m) {
			writeBulkError(w, http.StatusNotFound, "not found")
			return
		}
		id := overrideID(r)
		if id == "" {
			writeBulkError(w, http.StatusNotFound, "not found")
			return
		}
		ctx := r.Context()
		if m.tr != nil {
			ctx = i18nui.WithTranslator(ctx, m.tr)
		}
		// The capability gate runs before any read: a caller without it
		// learns nothing, not even that the record exists.
		if !access.CanResourceExact(ctx, overrideCapability(m), access.Ref{Type: m.name, ID: id}) {
			writeOverrideError(w, r, ctx, http.StatusForbidden, i18nui.TVars(ctx, i18nui.KeyEntityOverrideDenied, map[string]string{"entity": m.singular(ctx)}))
			return
		}
		plain := !handler.IsJSONContentType(r.Header.Get("Content-Type"))
		if plain && !isFormContentType(r.Header.Get("Content-Type")) {
			writeBulkError(w, http.StatusUnsupportedMediaType, "unsupported media type")
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, overrideBodyLimit)
		state, reason, back, err := readOverrideBody(r, plain)
		if err != nil {
			if _, tooBig := errors.AsType[*http.MaxBytesError](err); tooBig {
				writeBulkError(w, http.StatusRequestEntityTooLarge, "request body too large")
				return
			}
			writeBulkError(w, http.StatusBadRequest, "invalid request body")
			return
		}
		if plain {
			var ok bool
			if back, ok = scrubBackPath(back); !ok {
				writeBulkError(w, http.StatusBadRequest, "invalid return path")
				return
			}
		}
		// The read runs under the caller's context: another owner's id,
		// a record the entity's read permission refuses and a missing id
		// answer the same 404, and nothing changes. The in-process read
		// and write apply scope only, so the entity's own permissions are
		// asked here: the capability adds to update, it does not replace
		// it.
		if !m.ch.CanReadRecordScoped(ctx, id) {
			writeBulkError(w, http.StatusNotFound, "not found")
			return
		}
		if _, err := m.ch.GetOne(crud.WithReadHooks(ctx), id, nil); err != nil {
			writeBulkError(w, http.StatusNotFound, "not found")
			return
		}
		if !m.ch.CanUpdateRecordScoped(ctx, id) {
			writeOverrideError(w, r, ctx, http.StatusForbidden, i18nui.TVars(ctx, i18nui.KeyEntityOverrideDenied, map[string]string{"entity": m.singular(ctx)}))
			return
		}
		if !slices.Contains(stateValues(m), state) {
			writeOverrideError(w, r, ctx, http.StatusBadRequest, i18nui.T(ctx, i18nui.KeyEntityOverrideStateBad))
			return
		}
		reason = strings.TrimSpace(reason)
		if reason == "" {
			writeOverrideError(w, r, ctx, http.StatusBadRequest, i18nui.T(ctx, i18nui.KeyEntityOverrideBlank))
			return
		}
		if utf8.RuneCountInString(reason) > overrideReasonMax {
			writeOverrideError(w, r, ctx, http.StatusBadRequest, i18nui.T(ctx, i18nui.KeyEntityOverrideLong))
			return
		}
		if _, err := m.ch.UpdateOne(crud.WithStateOverride(ctx, reason), id, map[string]any{m.states.Field: state}); err != nil {
			writeOverrideRefusal(w, r, ctx, m, err)
			return
		}
		if plain {
			http.Redirect(w, r, back, http.StatusSeeOther)
			return
		}
		ui.AddToast(w, ui.ToastTrigger{Variant: ui.StatusSuccess, Title: i18nui.T(ctx, i18nui.KeyEntityOverrideDone)})
		writeBulkJSON(w, http.StatusOK, map[string]any{"status": "ok", "state": state})
	})
}

// overrideID reads the record id off this route's own path: the segment
// before _override. "" when the path is not the route's shape.
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

// overrideBody is the strict shape both arms decode into.
type overrideBody struct {
	State  string `json:"state"`
	Reason string `json:"reason"`
	Back   string `json:"back"`
}

// readOverrideBody decodes the posted state, reason and return path:
// strict JSON for the RPC arm, the form's own fields for a plain post.
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

// isFormContentType reports whether the request is a native form post.
func isFormContentType(ct string) bool {
	return strings.HasPrefix(strings.ToLower(strings.TrimSpace(ct)), "application/x-www-form-urlencoded")
}

// writeOverrideError answers both arms: the RPC caller a JSON error, a
// plain post the same status as a page that says what to fix.
func writeOverrideError(w http.ResponseWriter, r *http.Request, ctx context.Context, status int, msg string) {
	if handler.IsJSONContentType(r.Header.Get("Content-Type")) {
		writeBulkError(w, status, msg)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	body := ui.Callout(ui.CalloutConfig{Title: i18nui.T(ctx, i18nui.KeyEntityOverride), Variant: ui.StatusDanger}, render.Text(msg))
	_, _ = w.Write([]byte(body))
}

// writeOverrideRefusal maps crud's refusals to answers. The unaudited
// refusal is a 409: the operator turned the audit log off, or the entity
// never had one, and the override's only trail is the audit row. A
// refused write answers what the JSON API would (403, 404, 422, 400 for
// a hook); only a failure is a 500.
func writeOverrideRefusal(w http.ResponseWriter, r *http.Request, ctx context.Context, m *meta, err error) {
	switch {
	case errors.Is(err, crud.ErrStateOverrideUnaudited):
		writeOverrideError(w, r, ctx, http.StatusConflict, i18nui.T(ctx, i18nui.KeyEntityOverrideNoAudit))
	case errors.Is(err, crud.ErrStateOverrideNoReason):
		writeOverrideError(w, r, ctx, http.StatusBadRequest, i18nui.T(ctx, i18nui.KeyEntityOverrideBlank))
	case errors.Is(err, crud.ErrForbidden):
		writeOverrideError(w, r, ctx, http.StatusForbidden, i18nui.TVars(ctx, i18nui.KeyEntityOverrideDenied, map[string]string{"entity": m.singular(ctx)}))
	case crud.IsNotFound(err):
		writeBulkError(w, http.StatusNotFound, "not found")
	case isWriteInvalid(err):
		writeOverrideError(w, r, ctx, http.StatusUnprocessableEntity, i18nui.T(ctx, i18nui.KeyEntityOverrideFailed))
	case crud.IsHookRefusal(err):
		writeOverrideError(w, r, ctx, http.StatusBadRequest, i18nui.T(ctx, i18nui.KeyEntityOverrideFailed))
	default:
		slog.ErrorContext(ctx, "entityui: state override", "entity", m.name, "error", err)
		writeOverrideError(w, r, ctx, http.StatusInternalServerError, i18nui.T(ctx, i18nui.KeyEntityOverrideFailed))
	}
}

// isWriteInvalid reports whether err is crud refusing the written values:
// a field's validation or a state rule.
func isWriteInvalid(err error) bool {
	if _, ok := errors.AsType[*crud.ValidationError](err); ok {
		return true
	}
	_, ok := errors.AsType[*crud.StateError](err)
	return ok
}
