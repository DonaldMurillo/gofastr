package auth

import (
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
)

func TestUserEntityFieldsWithAppendsAndPreservesCanon(t *testing.T) {
	username := schema.Field{Name: "username", Type: schema.String, Unique: true}
	disabledAt := schema.Field{Name: "disabled_at", Type: schema.Timestamp}

	got := UserEntityFields().With(username, disabledAt)

	// Canonical fields still come first.
	canon := UserEntityFields()
	if len(got) != len(canon)+2 {
		t.Fatalf("len(got)=%d, want %d", len(got), len(canon)+2)
	}
	for i, f := range canon {
		if got[i].Name != f.Name {
			t.Fatalf("canon[%d] = %q, want %q", i, got[i].Name, f.Name)
		}
	}
	if got[len(canon)].Name != "username" {
		t.Fatalf("first appended = %q, want username", got[len(canon)].Name)
	}
	if got[len(canon)+1].Name != "disabled_at" {
		t.Fatalf("second appended = %q, want disabled_at", got[len(canon)+1].Name)
	}
}

func TestUserEntityFieldsWithDoesNotMutateCanon(t *testing.T) {
	a := UserEntityFields()
	_ = a.With(schema.Field{Name: "added"})
	b := UserEntityFields()
	if len(a) != len(b) {
		t.Fatalf("With() leaked into base slice: a=%d b=%d", len(a), len(b))
	}
}

// UserEntityFields is assignable to []schema.Field without explicit
// conversion. The compiler enforces this — if it fails, hosts that
// pass auth.UserEntityFields() to entity.EntityConfig.Fields break.
func TestUserEntityFieldsAssignableToSchemaSlice(t *testing.T) {
	var slice []schema.Field = UserEntityFields()
	if len(slice) == 0 {
		t.Fatal("UserEntityFields returned empty slice")
	}
}
