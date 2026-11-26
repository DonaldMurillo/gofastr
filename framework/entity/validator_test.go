package entity

import (
	"context"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/i18n"
)

func TestBuiltinValidatorsTranslate(t *testing.T) {
	cat := i18n.NewMapCatalog()
	cat.Set("es", "validation.required", i18n.Message{Text: "es obligatorio"})
	cat.Set("es", "validation.unique_taken", i18n.Message{Text: "ya está en uso"})
	ctx := i18n.WithContext(context.Background(), i18n.Locale{Tag: "es"})
	ctx = i18n.WithTranslator(ctx, i18n.NewTranslator(cat, "en"))

	vr := NewValidationRegistry()
	vr.RegisterValidator(Required("name"))
	vr.RegisterValidator(Unique("email", func(context.Context, any) bool { return false }))
	errs := vr.Validate(ctx, map[string]any{"email": "a@b.c"})
	if errs["name"] != "es obligatorio" || errs["email"] != "ya está en uso" {
		t.Fatalf("errs = %v, want both in Spanish", errs)
	}
	if got := vr.Validate(context.Background(), map[string]any{"email": "a@b.c"}); got["name"] != "is required" {
		t.Fatalf("no translator: name = %q, want the English default", got["name"])
	}
}

func TestRequiredRejectsTypedNil(t *testing.T) {
	var title *string
	errs := Required("title")(context.Background(), map[string]any{"title": title})
	if _, ok := errs["title"]; !ok {
		t.Fatal("required field accepted a typed nil")
	}
}

func TestRequiredRejectsZeroNamedScalar(t *testing.T) {
	type title string
	type score float32
	errs := Required("title", "score")(context.Background(), map[string]any{"title": title(""), "score": score(0)})
	if len(errs) != 2 {
		t.Fatalf("required accepted zero scalars: %v", errs)
	}
}

func TestFormatValidationErrorsUsesFieldOrder(t *testing.T) {
	want := []string{"a missing", "b invalid", "c too long"}
	for range 100 {
		got := FormatValidationErrors(map[string]string{"c": "too long", "b": "invalid", "a": "missing"})
		for i := range want {
			if got[i] != want[i] {
				t.Fatalf("errors = %v, want %v", got, want)
			}
		}
	}
}

func TestRequiredOwnsFieldNames(t *testing.T) {
	fields := []string{"title"}
	validate := Required(fields...)
	fields[0] = "author"
	errs := validate(t.Context(), nil)
	if _, ok := errs["title"]; !ok {
		t.Fatalf("caller changed required field names: %v", errs)
	}
}
