package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/i18n"
	"github.com/DonaldMurillo/gofastr/core/schema"
)

// assertFlatEnvelope decodes body as the one error envelope every route
// answers (issue #652): "error" is the message text, "code" the status,
// "reason" the stable code when the error has one, "success" false.
// core/handler.WriteError nested the first three under "error" while
// the CRUD routes wrote them flat, so a client parsed two shapes.
func assertFlatEnvelope(t *testing.T, status int, raw []byte, wantReason string) map[string]any {
	t.Helper()
	var body map[string]any
	if err := json.Unmarshal(raw, &body); err != nil {
		t.Fatalf("body is not JSON: %v: %s", err, raw)
	}
	msg, ok := body["error"].(string)
	if !ok || msg == "" {
		t.Fatalf(`"error" = %#v, want the message text: %s`, body["error"], raw)
	}
	if code, ok := body["code"].(float64); !ok || int(code) != status {
		t.Errorf(`"code" = %#v, want %d: %s`, body["code"], status, raw)
	}
	if s, ok := body["success"].(bool); !ok || s {
		t.Errorf(`"success" = %#v, want false: %s`, body["success"], raw)
	}
	if wantReason != "" {
		if r, _ := body["reason"].(string); r != wantReason {
			t.Errorf(`"reason" = %#v, want %q: %s`, body["reason"], wantReason, raw)
		}
	} else if _, has := body["reason"]; has {
		t.Errorf(`"reason" sent for an error with none: %s`, raw)
	}
	for _, k := range []string{"message"} {
		if _, has := body[k]; has {
			t.Errorf("nested-envelope key %q at the top level: %s", k, raw)
		}
	}
	return body
}

func TestWriteError_FlatEnvelope(t *testing.T) {
	cases := []struct {
		name       string
		err        error
		status     int
		wantMsg    string
		wantReason string
	}{
		{"phrased", Fail(http.StatusUnauthorized, ReasonAuthenticationRequired), 401, ReasonAuthenticationRequired.English(), ReasonAuthenticationRequired.Code()},
		{"own wording", Errorf(http.StatusTeapot, "short and stout"), 418, "short and stout", ""},
		{"not an *Error", errors.New("pq: secret"), 500, "internal server error", "internal"},
		{"validation, no fields", ValidationError(nil), 400, "validation failed", "validation_failed"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			WriteError(w, nil, tc.err)
			if w.Code != tc.status {
				t.Fatalf("status %d, want %d", w.Code, tc.status)
			}
			body := assertFlatEnvelope(t, tc.status, w.Body.Bytes(), tc.wantReason)
			if body["error"] != tc.wantMsg {
				t.Errorf(`"error" = %q, want %q`, body["error"], tc.wantMsg)
			}
			if _, has := body["fields"]; has {
				t.Errorf(`"fields" sent with no violations: %s`, w.Body)
			}
		})
	}
}

func TestWriteError_FlatEnvelopeCarriesFields(t *testing.T) {
	w := httptest.NewRecorder()
	WriteError(w, nil, ValidationError(map[string][]schema.Violation{
		"email": {{Reason: schema.ReasonRequired}},
	}))
	body := assertFlatEnvelope(t, 400, w.Body.Bytes(), "validation_failed")
	fields, _ := body["fields"].(map[string]any)
	reasons, _ := body["field_reasons"].(map[string]any)
	if got, _ := fields["email"].([]any); len(got) != 1 || got[0] != "is required" {
		t.Errorf(`"fields" = %v: %s`, body["fields"], w.Body)
	}
	if got, _ := reasons["email"].([]any); len(got) != 1 || got[0] != "required" {
		t.Errorf(`"field_reasons" = %v: %s`, body["field_reasons"], w.Body)
	}
}

// The adapter's own refusals (a body it cannot bind, a body over the
// cap, a handler's error) go out in the same envelope.
func TestHandlerAdapter_FlatEnvelope(t *testing.T) {
	type in struct {
		Name string `json:"name"`
	}
	h := HandlerAdapter(func(_ context.Context, v in) (struct{}, error) {
		if v.Name == "taken" {
			return struct{}{}, Fail(http.StatusConflict, i18n.StatusPhrase(http.StatusConflict))
		}
		return struct{}{}, nil
	})
	post := func(body string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		h(w, r)
		return w
	}
	t.Run("bind 400", func(t *testing.T) {
		w := post(`{"name":`)
		if w.Code != http.StatusBadRequest {
			t.Fatalf("status %d: %s", w.Code, w.Body)
		}
		assertFlatEnvelope(t, 400, w.Body.Bytes(), "invalid_json")
	})
	t.Run("body cap 413", func(t *testing.T) {
		w := post(`{"name":"` + string(bytes.Repeat([]byte("a"), maxBodyBytes+1)) + `"}`)
		if w.Code != http.StatusRequestEntityTooLarge {
			t.Fatalf("status %d: %s", w.Code, w.Body)
		}
		assertFlatEnvelope(t, 413, w.Body.Bytes(), i18n.StatusPhrase(http.StatusRequestEntityTooLarge).Code())
	})
	t.Run("handler error", func(t *testing.T) {
		w := post(`{"name":"taken"}`)
		if w.Code != http.StatusConflict {
			t.Fatalf("status %d: %s", w.Code, w.Body)
		}
		assertFlatEnvelope(t, 409, w.Body.Bytes(), "conflict")
	})
}
