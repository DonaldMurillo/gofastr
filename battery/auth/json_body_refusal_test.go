package auth

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/iotest"
)

// A body whose read fails is not called invalid JSON: it names a body
// that could not be read, the phrase the webhook ingest handler and the
// idempotency middleware already use for this event.
func TestDecodeJSONLimitedReadFailureNamesTheUnreadableBody(t *testing.T) {
	var dst struct{}
	req := httptest.NewRequest(http.MethodPost, "/login", iotest.ErrReader(errors.New("connection reset")))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	if decodeJSONLimited(rec, req, &dst) {
		t.Fatal("a failed read reported success")
	}
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status %d, want 400; body=%s", rec.Code, rec.Body.String())
	}
	assertAuthRefusal(t, rec, "body_unreadable", "failed to read request body")
}

// A malformed JSON body keeps the invalid-JSON-body answer.
func TestDecodeJSONLimitedMalformedJSONStaysInvalidJSONBody(t *testing.T) {
	var dst struct {
		Email string `json:"email"`
	}
	req := httptest.NewRequest(http.MethodPost, "/login", strings.NewReader(`{"email":`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	if decodeJSONLimited(rec, req, &dst) {
		t.Fatal("a malformed body reported success")
	}
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status %d, want 400; body=%s", rec.Code, rec.Body.String())
	}
	assertAuthRefusal(t, rec, "invalid_json_body", "invalid JSON body")
}

func assertAuthRefusal(t *testing.T, rec *httptest.ResponseRecorder, reason, text string) {
	t.Helper()
	var body struct {
		Error  string `json:"error"`
		Reason string `json:"reason"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v (%s)", err, rec.Body.String())
	}
	if body.Reason != reason || body.Error != text {
		t.Errorf("error %q reason %q, want %q / %q", body.Error, body.Reason, text, reason)
	}
}
