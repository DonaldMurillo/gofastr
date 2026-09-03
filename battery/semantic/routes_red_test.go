//go:build red

// RED TEST — open finding, 2026-09-02 adversarial pass (tests-only; no fix applied).
// Property: a bearer-authenticated indexing API must reject duplicate JSON
// keys; stdlib encoding/json silently keeps the LAST value, so the document
// set an audit log (or intercepting proxy) records can differ from the set
// actually indexed.
// Surfaces: battery/semantic/routes.go indexHandler POST /index (:177,
// indexRequest via limitBody + plain Decode) and queryHandler POST /query
// (:200, Query).
// Finding: both handlers 400 malformed JSON and 413 oversized bodies (pinned
// by routes_security_test.go) but accept {"documents":[A],"documents":[B]}
// and {"text":"a","text":"b"} with last-wins, then 202/200. The repo's pinned
// convention for this shape is core/handler/bind.go validateBodyKeys
// (duplicate + unknown key rejection; bind_strict_keys_security_test.go).
// Severity: production API surface (bearer token, potentially network-exposed
// via Plugin mount), though exploitation needs a body-mangling middlebox or a
// hostile authorized client.
// Fix direction: pre-validate top-level keys before Decode (duplicate-key
// walk as in validateBodyKeys / uinodev1 rejectDuplicateKeys).
// Round-6 mechanism split: each surface has one exact-duplicate test
// (wire-level last-wins) and one case-folded test (stdlib json's
// tag-insensitive struct match) — independently fixable mechanisms; a
// dedup-only fix leaves the folded tests red.
package semantic

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestSemanticRedIndexRejectsDuplicateKeys(t *testing.T) {
	h := Handler(errIndex{}, WithAuthToken("red-token"))
	// First occurrence indexes doc "a"; the smuggled last value swaps the
	// batch for doc "b". Plain Decode keeps the last and 202s.
	body := `{"documents":[{"id":"a","text":"alpha"}],"documents":[{"id":"b","text":"beta"}]}`
	req := httptest.NewRequest(http.MethodPost, "/index", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer red-token")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("SECURITY: [semantic-index-strict-keys] POST /index with duplicate \"documents\" key returned %d %q — last-wins swapped the indexed batch; want 400", rec.Code, rec.Body.String())
	}
}

func TestSemanticRedIndexRejectsFoldedKeys(t *testing.T) {
	h := Handler(errIndex{}, WithAuthToken("red-token"))
	// "Documents" is not the wire tag, but stdlib json still binds it via
	// the case-insensitive tag fallback, so the folded spelling smuggles a
	// second batch exactly like the exact duplicate does.
	body := `{"Documents":[{"id":"a","text":"alpha"}],"documents":[{"id":"b","text":"beta"}]}`
	req := httptest.NewRequest(http.MethodPost, "/index", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer red-token")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("SECURITY: [semantic-index-strict-keys] POST /index with case-folded \"Documents\"/\"documents\" keys returned %d %q — the folded spelling still binds the tagged field last-wins; want 400", rec.Code, rec.Body.String())
	}
}

func TestSemanticRedQueryRejectsDuplicateKeys(t *testing.T) {
	h := Handler(errIndex{}, WithAuthToken("red-token"))
	body := `{"text":"alpha","text":"omega"}`
	req := httptest.NewRequest(http.MethodPost, "/query", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer red-token")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("SECURITY: [semantic-query-strict-keys] POST /query with duplicate \"text\" key returned %d %q — last-wins swapped the query text; want 400", rec.Code, rec.Body.String())
	}
}

func TestSemanticRedQueryRejectsFoldedKeys(t *testing.T) {
	h := Handler(errIndex{}, WithAuthToken("red-token"))
	// "Text" binds the json:"text" tag via the case-insensitive fallback,
	// so the folded spelling smuggles a second query value.
	body := `{"Text":"alpha","text":"omega"}`
	req := httptest.NewRequest(http.MethodPost, "/query", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer red-token")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("SECURITY: [semantic-query-strict-keys] POST /query with case-folded \"Text\"/\"text\" keys returned %d %q — the folded spelling still binds the tagged field last-wins; want 400", rec.Code, rec.Body.String())
	}
}
