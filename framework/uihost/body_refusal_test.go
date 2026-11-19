package uihost

import (
	"errors"
	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/iotest"
)

// A 405 from the server-action handler names POST in Allow, as the
// session endpoints' methodNotAllowed helper does.
func TestServerActionRefusesNonPostWithAllow(t *testing.T) {
	ds := New(app.NewApp("refusal-test"))
	rec := httptest.NewRecorder()
	ds.handleServerAction(rec, httptest.NewRequest(http.MethodGet, "/__gofastr/action", nil))
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status %d, want 405; body=%s", rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get("Allow"); got != http.MethodPost {
		t.Errorf("Allow = %q, want %q", got, http.MethodPost)
	}
}

// A body whose read fails is not called invalid JSON: it names a body
// that could not be read.
func TestDecodeBoundedReadFailureNamesTheUnreadableBody(t *testing.T) {
	var dst struct{}
	req := httptest.NewRequest(http.MethodPost, "/__gofastr/action", iotest.ErrReader(errors.New("connection reset")))
	rec := httptest.NewRecorder()
	if decodeBounded(rec, req, &dst) {
		t.Fatal("a failed read reported success")
	}
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status %d, want 400; body=%s", rec.Code, rec.Body.String())
	}
	if got := strings.TrimSpace(rec.Body.String()); got != "failed to read request body" {
		t.Errorf("body %q, want the body_unreadable phrase", got)
	}
}

// A malformed JSON body keeps the invalid-JSON-body answer.
func TestDecodeBoundedMalformedJSONStaysInvalidJSONBody(t *testing.T) {
	var dst struct {
		Action string `json:"action"`
	}
	req := httptest.NewRequest(http.MethodPost, "/__gofastr/action", strings.NewReader(`{"action":`))
	rec := httptest.NewRecorder()
	if decodeBounded(rec, req, &dst) {
		t.Fatal("a malformed body reported success")
	}
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status %d, want 400; body=%s", rec.Code, rec.Body.String())
	}
	if got := strings.TrimSpace(rec.Body.String()); got != "invalid JSON body" {
		t.Errorf("body %q, want the invalid_json_body phrase", got)
	}
}
