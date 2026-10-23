package handler

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/i18n"
	"github.com/DonaldMurillo/gofastr/core/schema"
)

func TestWriteErrorTranslatesForRequest(t *testing.T) {
	cat := i18n.NewMapCatalog()
	cat.Set("fr", "error.validation_failed", i18n.Message{Text: "validation échouée"})
	cat.Set("fr", "validation.required", i18n.Message{Text: "est obligatoire"})
	ctx := i18n.WithTranslator(i18n.WithContext(context.Background(), i18n.Locale{Tag: "fr"}), i18n.NewTranslator(cat, "en"))
	r := httptest.NewRequest("POST", "/", nil).WithContext(ctx)
	w := httptest.NewRecorder()
	WriteError(w, r, ValidationError(map[string][]schema.Violation{"title": {{Reason: schema.ReasonRequired}}}))
	var body errorResponse
	if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if body.Error.Message != "validation échouée" || body.Error.Reason != "validation_failed" {
		t.Fatalf("message %q reason %q", body.Error.Message, body.Error.Reason)
	}
	if body.Error.Fields["title"][0] != "est obligatoire" || body.Error.FieldReasons["title"][0] != "required" {
		t.Fatalf("fields %v reasons %v", body.Error.Fields, body.Error.FieldReasons)
	}
}

func TestWriteErrorAppMessageHasNoReason(t *testing.T) {
	w := httptest.NewRecorder()
	WriteError(w, nil, Errorf(404, "no such widget"))
	var body errorResponse
	json.NewDecoder(w.Body).Decode(&body)
	if body.Error.Message != "no such widget" || body.Error.Reason != "" {
		t.Fatalf("message %q reason %q", body.Error.Message, body.Error.Reason)
	}
}

func TestWriteErrorInternalHasReason(t *testing.T) {
	w := httptest.NewRecorder()
	WriteError(w, nil, context.Canceled)
	var body errorResponse
	json.NewDecoder(w.Body).Decode(&body)
	if body.Error.Reason != "internal" || body.Error.Message != "internal server error" {
		t.Fatalf("message %q reason %q", body.Error.Message, body.Error.Reason)
	}
}
