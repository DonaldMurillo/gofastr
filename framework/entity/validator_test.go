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
