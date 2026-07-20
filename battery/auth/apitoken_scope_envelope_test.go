package auth

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// Scope rejections use the canonical flat error envelope
// {"error": "<message>", "success": false, "code": 403} with a JSON
// content type — the shape the generated SDKs and sdkdocs document.
func TestScopeErrorFlatEnvelope(t *testing.T) {
	reject := func(t *testing.T, h http.Handler, req *http.Request) {
		t.Helper()
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusForbidden {
			t.Fatalf("status = %d, want 403", rec.Code)
		}
		if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
			t.Fatalf("Content-Type = %q, want application/json", ct)
		}
		var envelope struct {
			Error   any   `json:"error"`
			Success *bool `json:"success"`
			Code    int   `json:"code"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
			t.Fatalf("body is not JSON: %v (%q)", err, rec.Body.String())
		}
		msg, ok := envelope.Error.(string)
		if !ok || msg == "" {
			t.Fatalf(`"error" = %#v, want non-empty string (flat envelope)`, envelope.Error)
		}
		if envelope.Success == nil || *envelope.Success {
			t.Fatalf(`"success" = %v, want false`, envelope.Success)
		}
		if envelope.Code != http.StatusForbidden {
			t.Fatalf(`"code" = %d, want 403`, envelope.Code)
		}
	}

	ok := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	t.Run("RequireAPIScopes", func(t *testing.T) {
		req := httptest.NewRequest("POST", "/api/customers", nil)
		req = req.WithContext(context.WithValue(req.Context(), tokenScopesKey{}, []string{"invoices:read"}))
		reject(t, RequireAPIScopes("/api")(ok), req)
	})
	t.Run("RequireScope", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/anything", nil)
		req = req.WithContext(context.WithValue(req.Context(), tokenScopesKey{}, []string{"other:read"}))
		reject(t, RequireScope("posts:read")(ok), req)
	})
}
