package crud

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
)

func phraseItemsHandler(t *testing.T, jsonCase JSONCase) *CrudHandler {
	t.Helper()
	db := setupDB(t, `CREATE TABLE items (id TEXT PRIMARY KEY, title TEXT, body_text TEXT)`)
	ent := entity.Define("items", entity.EntityConfig{
		Table: "items",
		Fields: []schema.Field{
			{Name: "title", Type: schema.String},
			{Name: "body_text", Type: schema.String},
		},
		Exposure: &entity.ExposureConfig{Public: true},
	}.WithTimestamps(false))
	ent.SetDB(db)
	seedRows(t, db, "items", []map[string]any{{"id": "i1", "title": "a"}})
	return NewCrudHandler(ent, db).WithJSONCase(jsonCase)
}

// batch-endpoints.md: a per-item failure carries the message and code the
// single-row write answers, with `reason` whenever the refusal has a code.
// A batch update item with no key answered a hard-coded English
// `missing "id"` with no reason, so a client could not branch on it and a
// locale never translated it (2026-11-01 bug bash).
func TestBatchUpdateMissingKeyHasReason(t *testing.T) {
	ch := phraseItemsHandler(t, CaseSnake)
	r := httptest.NewRequest(http.MethodPatch, "/items/_batch", strings.NewReader(`{"items":[{"title":"x"}]}`))
	r.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	ch.BatchUpdate()(rec, r)
	var resp BatchResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v (%s)", err, rec.Body.String())
	}
	if rec.Code != http.StatusBadRequest || len(resp.Results) != 1 {
		t.Fatalf("batch update with no key = %d %s", rec.Code, rec.Body.String())
	}
	if got := resp.Results[0]; got.Reason != reasonItemMissingKey.Code() || got.Error != "missing id" {
		t.Fatalf("item = %+v, want reason %q and the phrase naming the key", got, reasonItemMissingKey.Code())
	}
}

// itemKeyFoldsUnambiguous ranged the item map and named whichever spelling
// the iteration reached second, so one request body answered different
// bytes from one call to the next. It now walks sorted keys and names
// both spellings through the keys_fold phrase.
func TestBatchFoldRefusalIsDeterministic(t *testing.T) {
	ch := phraseItemsHandler(t, CaseCamel) // bodyText and body_text fold together
	seen := map[string]bool{}
	for range 64 {
		r := httptest.NewRequest(http.MethodPost, "/items/_batch",
			strings.NewReader(`{"items":[{"bodyText":"a","body_text":"b"}]}`))
		r.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		ch.BatchCreate()(rec, r)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("fold collision = %d %s, want 400", rec.Code, rec.Body.String())
		}
		seen[rec.Body.String()] = true
	}
	if len(seen) != 1 {
		t.Fatalf("one request body answered %d different responses: %v", len(seen), seen)
	}
	for body := range seen {
		if !strings.Contains(body, "bodyText") || !strings.Contains(body, "body_text") || strings.Contains(body, "another spelling") {
			t.Fatalf("refusal = %s, want the keys_fold phrase naming both spellings", body)
		}
	}
}

// parseMultipartBody ranged MultipartForm.Value, so a form repeating two
// single-valued keys named either one, and its refusals were English
// fmt.Errorf text with no code of their own.
func TestMultipartRefusalIsDeterministic(t *testing.T) {
	ch, _ := dupFormHandler(t)
	body := "--B\r\n" +
		"Content-Disposition: form-data; name=\"title\"\r\n\r\none\r\n" +
		"--B\r\n" +
		"Content-Disposition: form-data; name=\"title\"\r\n\r\ntwo\r\n" +
		"--B\r\n" +
		"Content-Disposition: form-data; name=\"zeta\"\r\n\r\none\r\n" +
		"--B\r\n" +
		"Content-Disposition: form-data; name=\"zeta\"\r\n\r\ntwo\r\n" +
		"--B--\r\n"
	seen := map[string]bool{}
	for range 64 {
		req := withTestUser(httptest.NewRequest(http.MethodPost, "/dupmp_notes", strings.NewReader(body)), "alice")
		req.Header.Set("Content-Type", "multipart/form-data; boundary=B")
		rec := httptest.NewRecorder()
		ch.Create()(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("duplicate keys = %d %s, want 400", rec.Code, rec.Body.String())
		}
		seen[rec.Body.String()] = true
	}
	if len(seen) != 1 {
		t.Fatalf("one multipart body answered %d different responses: %v", len(seen), seen)
	}
	for b := range seen {
		if !strings.Contains(b, `"reason":"`+reasonDupFormKey.Code()+`"`) || !strings.Contains(b, `\"title\"`) {
			t.Fatalf("refusal = %s, want the duplicate_form_key phrase naming the first key", b)
		}
	}
}
