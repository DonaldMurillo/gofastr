package crud

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// The 415 and the cross-site-form 403 name their cause with the shared
// phrases (auth's 415, the CSRF middleware's 403), not the bare status
// phrase.
func TestContentTypeRefusalsNameTheSharedReasons(t *testing.T) {
	ch, _ := statesWorld(t)

	plain := httptest.NewRequest(http.MethodPost, "/invoices", strings.NewReader(`{"number":"a"}`))
	plain.Header.Set("Content-Type", "text/plain")
	rec := httptest.NewRecorder()
	ch.Create()(rec, plain)
	assertRefusalBody(t, rec, http.StatusUnsupportedMediaType, "json_media_type", "unsupported media type: expected application/json")

	rec = httptest.NewRecorder()
	ch.Create()(rec, multipartCreate(t, "forged", map[string]string{"Sec-Fetch-Site": "cross-site"}))
	assertRefusalBody(t, rec, http.StatusForbidden, "cross_site", "cross-site request refused")
}

func assertRefusalBody(t *testing.T, rec *httptest.ResponseRecorder, status int, reason, text string) {
	t.Helper()
	if rec.Code != status {
		t.Fatalf("status %d, want %d; body=%s", rec.Code, status, rec.Body.String())
	}
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
