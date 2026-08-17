package entity

import (
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
)

// An owner scope without a matching field declaration used to build an
// entity whose INSERT/SELECT column lists never mentioned the owner
// column: crud.InjectOwner stamped body["user_id"], but doCreate builds
// its column list from GetFields() so the stamp was silently dropped, and
// AutoMigrate never created the column — the first scoped read then
// failed with "no such column: user_id". Define injects the missing
// owner column, same contract as the tenant_id and deleted_at
// injections above, and matching what the blueprint generator emits for
// an `owner_field:` entity.
func TestDefine_InjectsUndeclaredOwnerField(t *testing.T) {
	e := Define("logs", EntityConfig{
		Fields: []schema.Field{{Name: "notes", Type: schema.String}},
		Scope:  &ScopeConfig{OwnerField: "user_id"},
	}.WithTimestamps(false))

	var owner *schema.Field
	for i := range e.Config.Fields {
		if e.Config.Fields[i].Name == "user_id" {
			owner = &e.Config.Fields[i]
			break
		}
	}
	if owner == nil {
		t.Fatalf("user_id not injected into fields: %+v", e.Config.Fields)
	}
	if owner.Type != schema.String {
		t.Errorf("injected owner column type = %v, want String", owner.Type)
	}
	if !owner.Hidden {
		t.Errorf("injected owner column must be Hidden (never shown in API responses/OpenAPI): %+v", *owner)
	}
	if !owner.ReadOnly {
		t.Errorf("injected owner column must be ReadOnly (framework-managed, client-unsettable): %+v", *owner)
	}
	if owner.AutoGenerate != schema.AutoNone {
		t.Errorf("injected owner column AutoGenerate = %v, want AutoNone (crud.InjectOwner stamps it)", owner.AutoGenerate)
	}
}

// A declared owner field wins and is left untouched — injection must not
// flip flags on a column the author chose to declare (e.g. one kept
// visible in responses).
func TestDefine_DeclaredOwnerFieldLeftUntouched(t *testing.T) {
	e := Define("logs", EntityConfig{
		Fields: []schema.Field{
			{Name: "user_id", Type: schema.String},
			{Name: "notes", Type: schema.String},
		},
		Scope: &ScopeConfig{OwnerField: "user_id"},
	}.WithTimestamps(false))

	count := 0
	for _, f := range e.Config.Fields {
		if f.Name != "user_id" {
			continue
		}
		count++
		if f.Hidden || f.ReadOnly {
			t.Errorf("declared owner field was mutated by injection: %+v", f)
		}
	}
	if count != 1 {
		t.Fatalf("user_id appears %d times, want exactly the caller's declaration", count)
	}
}

func TestDefine_RejectsUnsafeOwnerField(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("Define accepted an OwnerField that is not a valid SQL identifier; " +
				"the name is interpolated into owner-scope WHERE clauses")
		}
	}()
	Define("notes", EntityConfig{
		Fields: []schema.Field{{Name: "id", Type: schema.String}},
		Scope:  &ScopeConfig{OwnerField: `user_id"; DROP TABLE notes--`},
	})
}
