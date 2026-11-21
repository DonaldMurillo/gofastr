package migrate

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
)

// TestDeclaredForeignKeysErrorNamesColumn pins the merged column
// sanitizer's error text: an unsafe foreign-key column is refused with
// the shared "invalid column" context, not a bare quoted ident, so the
// migration error says what was wrong with which column.
func TestDeclaredForeignKeysErrorNamesColumn(t *testing.T) {
	src := rawEnt("child", "child", []schema.Field{{Name: "x", Type: schema.String}}, []entity.Relation{{
		Name:       "parent",
		Type:       entity.RelManyToOne,
		Entity:     "parent",
		ForeignKey: []string{"p;id"},
	}}, "id")
	target := rawEnt("parent", "parent", []schema.Field{{Name: "id", Type: schema.String}}, nil, "id")
	all := map[string]*entity.Entity{"child": src, "parent": target}
	_, err := declaredForeignKeys(src, all)
	if err == nil {
		t.Fatal("expected an error for an unsafe FK column, got nil")
	}
	if !strings.Contains(err.Error(), `invalid column "p;id"`) {
		t.Errorf("error must name the invalid column, got: %v", err)
	}
}
