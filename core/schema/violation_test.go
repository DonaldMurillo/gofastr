package schema

import (
	"context"
	"errors"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/i18n"
)

func TestViolationCodeAndEnglish(t *testing.T) {
	max := 3.0
	err := Validate(Field{Name: "t", Type: String, Max: &max}, "abcd")
	v, ok := errors.AsType[Violation](err)
	if !ok {
		t.Fatalf("Validate returned %T, want Violation", err)
	}
	if v.Code() != "max_length" || err.Error() != "must be at most 3 characters" {
		t.Fatalf("code %q, message %q", v.Code(), err.Error())
	}
}

func TestViolationTranslates(t *testing.T) {
	cat := i18n.NewMapCatalog()
	cat.Set("fr", "validation.required", i18n.Message{Text: "est obligatoire"})
	ctx := i18n.WithTranslator(i18n.WithContext(context.Background(), i18n.Locale{Tag: "fr"}), i18n.NewTranslator(cat, "en"))
	res := ValidateAll(Schema{Fields: []Field{{Name: "title", Type: String, Required: true}}}, map[string]any{})
	vs := res.Errors["title"]
	if len(vs) != 1 || vs[0].Text(ctx) != "est obligatoire" || vs[0].Code() != "required" {
		t.Fatalf("violations = %#v", vs)
	}
}

func TestPatternMessageQuotesPattern(t *testing.T) {
	err := Validate(Field{Name: "s", Type: String, Pattern: "^a$"}, "b")
	if err == nil || err.Error() != `must match pattern "^a$"` {
		t.Fatalf("err = %v", err)
	}
}

func TestAsViolationWrapsPlainError(t *testing.T) {
	v := AsViolation(errors.New("boom"))
	if v.Code() != "invalid" || v.Error() != "is invalid: boom" {
		t.Fatalf("got %q %q", v.Code(), v.Error())
	}
}
