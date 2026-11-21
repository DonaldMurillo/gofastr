package entityui

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/iotest"
)

// A 405 from a POST-only handler names POST in Allow, as RFC 9110
// requires and the picker and override handlers already do.
func TestPostOnlyHandlersAnswer405WithAllowPost(t *testing.T) {
	x := newTestUI(t, invoiceEntities(), invoiceRows(), withAPI(map[string]string{"invoices": "/api/invoices"}))
	for name, h := range map[string]http.Handler{
		"save":        x.ui.SaveHandler("invoices"),
		"saved-views": x.ui.SavedViewsHandler("invoices"),
		"restore":     x.ui.RestoreHandler("invoices"),
		"purge":       x.ui.PurgeHandler("invoices"),
	} {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/invoices/inv-1/_save", nil))
		if rec.Code != http.StatusMethodNotAllowed {
			t.Fatalf("%s: status %d, want 405; body=%s", name, rec.Code, rec.Body.String())
		}
		if got := rec.Header().Get("Allow"); got != http.MethodPost {
			t.Errorf("%s: Allow = %q, want %q", name, got, http.MethodPost)
		}
	}
}

// A 415 names the media type to send, the answer auth's JSON endpoints
// give, instead of the bare status phrase.
func TestJSONSurfaces415NameTheJSONMediaType(t *testing.T) {
	x := newTestUI(t, invoiceEntities(), invoiceRows(), withAPI(map[string]string{"invoices": "/api/invoices"}))
	bulk := httptest.NewRequest(http.MethodPost, "/api/invoices/_bulk", strings.NewReader(`{}`))
	bulk.Header.Set("Content-Type", "text/plain")
	bulk = bulk.WithContext(x.userCtx("/api/invoices/_bulk", "", "u1"))
	rec := httptest.NewRecorder()
	x.ui.BulkHandler("invoices").ServeHTTP(rec, bulk)
	assertJSONMediaType415(t, rec, "bulk")

	pick := pickPost(t, x, "customer_id", `{"q":""}`, func(r *http.Request) {
		r.Header.Set("Content-Type", "text/plain")
	})
	assertJSONMediaType415(t, pick, "picker")

	w := newOverrideWorld(t, nil)
	policy := overridePolicy(t, "invoices:read", "invoices:update", "invoices:override_state")
	ovr := postOverride(t, w, overrideCtx(policy, "boss"), "text/plain", "")
	assertJSONMediaType415(t, ovr, "override")
}

func assertJSONMediaType415(t *testing.T, rec *httptest.ResponseRecorder, surface string) {
	t.Helper()
	if rec.Code != http.StatusUnsupportedMediaType {
		t.Fatalf("%s: status %d, want 415; body=%s", surface, rec.Code, rec.Body.String())
	}
	var body struct {
		Error  string `json:"error"`
		Reason string `json:"reason"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("%s: decode: %v (%s)", surface, err, rec.Body.String())
	}
	if body.Reason != "json_media_type" || body.Error != "unsupported media type: expected application/json" {
		t.Errorf("%s: error %q reason %q, want the shared json_media_type phrase", surface, body.Error, body.Reason)
	}
}

// A body whose read fails (a dropped connection, not a syntax error)
// is not called invalid JSON: it names a body that could not be read.
func TestBulkBodyReadFailureNamesTheUnreadableBody(t *testing.T) {
	x := newTestUI(t, invoiceEntities(), invoiceRows(), withAPI(map[string]string{"invoices": "/api/invoices"}))
	req := httptest.NewRequest(http.MethodPost, "/api/invoices/_bulk", iotest.ErrReader(errors.New("connection reset")))
	req.Header.Set("Content-Type", "application/json")
	req = req.WithContext(x.userCtx("/api/invoices/_bulk", "", "u1"))
	rec := httptest.NewRecorder()
	x.ui.BulkHandler("invoices").ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status %d, want 400; body=%s", rec.Code, rec.Body.String())
	}
	var body struct {
		Error  string `json:"error"`
		Reason string `json:"reason"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Reason != "body_unreadable" || body.Error != "failed to read request body" {
		t.Errorf("error %q reason %q, want the shared body_unreadable phrase", body.Error, body.Reason)
	}
}
