package crud

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
)

// goInternals is what encoding/json's own text carried into a 400 body:
// Go type spellings, the request struct's name, the decoder's prefix.
var goInternals = regexp.MustCompile(`interface \{\}|map\[|struct|\b[A-Z][A-Za-z0-9_]*\.[a-z]|Go value|Go struct|of type|json: |cannot unmarshal|invalid character|batchDeleteRequest|batchCreateRequest`)

// A malformed body answered with encoding/json's text, which names the
// server's Go types: `invalid JSON: json: cannot unmarshal number into
// batchDeleteRequest.ids.0 of type string`, `...Go value of type
// map[string]interface {}`. Each decode failure now names the JSON path
// in the client's spelling and the JSON type, and keeps its reason code.
func TestJSONDecodeErrorsArePhrasedOverHTTP(t *testing.T) {
	ch := phraseItemsHandler(t, CaseSnake)
	cases := []struct {
		name, method, path, body string
		serve                    http.HandlerFunc
		want, reason             string
	}{
		{"batch delete wrong id type", http.MethodDelete, "/items/_batch", `{"ids":[1]}`, ch.BatchDelete(),
			"invalid JSON: ids[0]: expected a string, got a number", "invalid_json"},
		{"batch delete syntax error", http.MethodDelete, "/items/_batch", `{"ids": [1,}`, ch.BatchDelete(),
			"invalid request body: syntax error at byte 12", "invalid_body"},
		{"batch delete cut off", http.MethodDelete, "/items/_batch", `{"ids":[`, ch.BatchDelete(),
			"invalid request body: the body ends before the JSON value is complete", "invalid_body"},
		{"batch create item not an object", http.MethodPost, "/items/_batch", `{"items":[5]}`, ch.BatchCreate(),
			"invalid JSON: items[0]: expected an object, got a number", "invalid_json"},
		{"create with an array body", http.MethodPost, "/items", `[1]`, ch.Create(),
			"invalid JSON: expected an object, got an array", "invalid_json"},
		{"create syntax error", http.MethodPost, "/items", `{"title": tru}`, ch.Create(),
			"invalid JSON: syntax error at byte 14", "invalid_json"},
		{"create empty body", http.MethodPost, "/items", ``, ch.Create(),
			"invalid JSON: the body is empty", "invalid_json"},
		{"create trailing value", http.MethodPost, "/items", `{"title":"a"} {}`, ch.Create(),
			"invalid JSON: unexpected data after the JSON value", "invalid_json"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequest(tc.method, tc.path, strings.NewReader(tc.body))
			r.Header.Set("Content-Type", "application/json")
			rec := httptest.NewRecorder()
			tc.serve(rec, r)
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status %d, want 400: %s", rec.Code, rec.Body.String())
			}
			var body struct {
				Error  string `json:"error"`
				Reason string `json:"reason"`
			}
			if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
				t.Fatalf("decode: %v (%s)", err, rec.Body.String())
			}
			if body.Error != tc.want || body.Reason != tc.reason {
				t.Errorf("error %q reason %q\n want  %q reason %q", body.Error, body.Reason, tc.want, tc.reason)
			}
			if m := goInternals.FindString(rec.Body.String()); m != "" {
				t.Errorf("body carries Go internals %q: %s", m, rec.Body.String())
			}
		})
	}
}

// A field of the wrong type on a single create is not a decode error:
// the body decodes into a map, and the schema refuses the value by name.
// That refusal must not leak Go types either.
func TestCreateWrongTypedFieldNamesNoGoType(t *testing.T) {
	ch := phraseItemsHandler(t, CaseSnake)
	r := httptest.NewRequest(http.MethodPost, "/items", strings.NewReader(`{"title":{"x":[1]}}`))
	r.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	ch.Create()(rec, r)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status %d, want 400: %s", rec.Code, rec.Body.String())
	}
	if m := goInternals.FindString(rec.Body.String()); m != "" {
		t.Errorf("body carries Go internals %q: %s", m, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"title"`) {
		t.Errorf("refusal does not name the field: %s", rec.Body.String())
	}
}
