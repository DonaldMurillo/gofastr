package crud

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/framework/hook"
)

// refusalState hid a record's state from a caller its declared ReadScope
// hides (TestStateRefusalHidesFromReadScope), but not from one a
// BeforeGet hook hides: a 409 on a move, or a 422 on a write of the state
// field, named the stored state of a row GET answers 404 for (2026-11-01
// bug bash). It now asks the same BeforeGet chain the write responses do.
// Since the owner decision of the same day the write itself answers 404
// for such a row before any refusal is built (refuseHiddenTarget), which
// names no state either.
func TestStateRefusalHidesFromBeforeGetScope(t *testing.T) {
	ch, db := statesWorld(t)
	scope := func(_ context.Context, data any) error {
		switch p := data.(type) {
		case *hook.GetPayload:
			p.AddWhere("status IN ($1, $2)", "draft", "open")
		case *hook.ListPayload:
			p.AddWhere("status IN ($1, $2)", "draft", "open")
		}
		return nil
	}
	hooks := hook.NewHookRegistry()
	hooks.RegisterHook(hook.BeforeGet, scope)
	hooks.RegisterHook(hook.BeforeList, scope)
	ch.Hooks = hooks
	seedStateInvoice(t, db, "hidden", "paid", "2026-01-02")
	seedStateInvoice(t, db, "shown", "open", nil)
	r := statesRouter(ch)
	send := func(method, path, body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		return rec
	}

	if get := send(http.MethodGet, "/invoices/hidden", ""); get.Code != http.StatusNotFound {
		t.Fatalf("precondition: GET of the hook-hidden row = %d (%s), want 404", get.Code, get.Body.String())
	}

	mv := send(http.MethodPost, "/invoices/hidden/transitions/issue", "")
	if mv.Code != http.StatusNotFound || strings.Contains(mv.Body.String(), "paid") {
		t.Errorf("SECURITY: the move refusal for a row GET answers 404 for = %d %s, want GET's 404 naming no state", mv.Code, mv.Body.String())
	}
	up := send(http.MethodPatch, "/invoices/hidden", `{"status":"draft"}`)
	if up.Code != http.StatusNotFound || strings.Contains(up.Body.String(), "paid") {
		t.Errorf("SECURITY: the state refusal for a row GET answers 404 for = %d %s, want GET's 404 naming no state", up.Code, up.Body.String())
	}

	// A row the hook admits still names where it stands.
	if mv := send(http.MethodPost, "/invoices/shown/transitions/issue", ""); mv.Code != http.StatusConflict || !strings.Contains(mv.Body.String(), `"current":"open"`) {
		t.Errorf("the move refusal for a visible row = %d %s, want a 409 naming its state", mv.Code, mv.Body.String())
	}

	// In process the hook binds only under WithReadHooks, as GetOne.
	if _, err := ch.RunTransition(WithReadHooks(context.Background()), "hidden", "issue"); !errors.Is(err, errNotFound) || strings.Contains(err.Error(), "paid") {
		t.Errorf("SECURITY: in-process move under WithReadHooks = %v, want a refusal naming no state", err)
	}
	_, err := ch.RunTransition(context.Background(), "hidden", "issue")
	if tce, ok := errors.AsType[*TransitionConflictError](err); !ok || tce.Current != "paid" {
		t.Errorf("in-process move without WithReadHooks = %v, want the stored state, as GetOne reads it", err)
	}
}
