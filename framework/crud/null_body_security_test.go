package crud

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/framework/hook"
)

func TestHTTPWritesRejectNullObjects(t *testing.T) {
	installOwnerExtractor(t)
	for _, tc := range []struct {
		name, method, path, body, detail string
		handler                          func(*CrudHandler) http.HandlerFunc
	}{
		{"create", http.MethodPost, "/reddocs", "null", "the body must be a JSON object", (*CrudHandler).Create},
		{"update", http.MethodPatch, "/reddocs/d1", "null", "the body must be a JSON object", (*CrudHandler).Update},
		{"batch create", http.MethodPost, "/reddocs/_batch", `{"items":[{},null]}`, "items[1]:", (*CrudHandler).BatchCreate},
		{"batch update", http.MethodPatch, "/reddocs/_batch", `{"items":[{"id":"d1"},null]}`, "items[1]:", (*CrudHandler).BatchUpdate},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ch, db := setupCamelDocsHandler(t)
			seedRows(t, db, "reddocs", []map[string]any{{"id": "d1", "body_text": "original"}})
			ch.Hooks = hook.NewHookRegistry()
			calls := 0
			prepare := func(_ context.Context, payload any) error {
				calls++
				payload.(map[string]any)["body_text"] = "hook value"
				return nil
			}
			ch.Hooks.RegisterHook(hook.BeforeCreate, prepare)
			ch.Hooks.RegisterHook(hook.BeforeUpdate, prepare)
			req := withTestUser(httptest.NewRequest(tc.method, tc.path, strings.NewReader(tc.body)), "u1")
			req.SetPathValue("id", "d1")
			req.Header.Set("Content-Type", "application/json")
			rec := httptest.NewRecorder()
			tc.handler(ch)(rec, req)
			if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), tc.detail) {
				t.Errorf("null write returned %d %s, want 400 with %q", rec.Code, rec.Body, tc.detail)
			}
			if calls != 0 {
				t.Errorf("null write ran %d hooks, want zero", calls)
			}
			var count int
			if err := db.QueryRow("SELECT COUNT(*) FROM reddocs").Scan(&count); err != nil || count != 1 {
				t.Errorf("row count = %d, %v, want one existing row", count, err)
			}
		})
	}
}

func TestMCPCreateAllowsOmittedArguments(t *testing.T) {
	installOwnerExtractor(t)
	ch, _ := setupCamelDocsHandler(t)
	mux := http.NewServeMux()
	mux.HandleFunc("POST "+ch.mcpBase(), ch.Create())
	got, err := ch.createTool(mux)(withTestUserCtx("u1"), nil)
	row, _ := got.(map[string]any)
	if id, _ := row["id"].(string); err != nil || id == "" {
		t.Fatalf("MCP create with omitted arguments = (%v, %v), want a row with an ID", got, err)
	}
}
