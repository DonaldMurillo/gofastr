package entity

import (
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
)

// ent-2: a {type: relation, to: X} field must derive a BelongsTo relation in
// Config.Relations so migrate emits the FK constraint and ?include= can resolve
// it. Without the derived relation the field is a plain TEXT column with no
// referential integrity and no eager-loadable join.
func TestRelationFieldDerivesBelongsTo(t *testing.T) {
	e := Define("posts", EntityConfig{
		Fields: []schema.Field{
			{Name: "title", Type: schema.String},
			{Name: "author", Type: schema.Relation, To: "users"},
		},
	})

	var rel *Relation
	for i := range e.Config.Relations {
		if e.Config.Relations[i].Name == "author" {
			rel = &e.Config.Relations[i]
			break
		}
	}
	if rel == nil {
		t.Fatalf("expected a derived relation named %q in Config.Relations, got %+v", "author", e.Config.Relations)
	}
	if rel.Type != RelManyToOne {
		t.Errorf("expected RelManyToOne (BelongsTo), got %d", rel.Type)
	}
	if rel.Entity != "users" {
		t.Errorf("expected target entity %q, got %q", "users", rel.Entity)
	}
	// For BelongsTo, ForeignKey is the column on the local table holding the
	// FK — that is the relation field's own column.
	if rel.ForeignKey != "author" {
		t.Errorf("expected ForeignKey %q (the field's own column), got %q", "author", rel.ForeignKey)
	}
}

// A relation field must not clobber an explicit BelongsTo the caller already
// declared for the same name.
func TestRelationFieldDoesNotOverrideExplicitRelation(t *testing.T) {
	e := Define("posts", EntityConfig{
		Fields: []schema.Field{
			{Name: "author_id", Type: schema.Relation, To: "users"},
		},
		Relations: []Relation{
			BelongsTo("author_id", "people", "author_id"),
		},
	})

	count := 0
	for _, r := range e.Config.Relations {
		if r.Name == "author_id" {
			count++
			if r.Entity != "people" {
				t.Errorf("explicit relation was overridden: expected entity %q, got %q", "people", r.Entity)
			}
		}
	}
	if count != 1 {
		t.Errorf("expected exactly one relation named %q, got %d", "author_id", count)
	}
}
