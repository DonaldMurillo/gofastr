package entity

import (
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
)

// HidesRelation is the one rule crud's include and nested-filter lookup,
// llm.md and /openapi.json share for a relation behind a Hidden field.
// Each arm holds on its own: a relation named after a Hidden field whose
// key it does not hold (a has_many over the field's name would publish the
// hidden name as its key), and a belongs_to whose foreign key is Hidden
// under another name. A has_many's foreign key lives on the target, so a
// Hidden local column that shares its name hides nothing.
func TestHidesRelation(t *testing.T) {
	ent := Define("notes", EntityConfig{
		Fields: []schema.Field{
			{Name: "body", Type: schema.String},
			{Name: "audit_trail", Type: schema.String, Hidden: true},
			{Name: "editor_id", Type: schema.String, Hidden: true},
			{Name: "note_id", Type: schema.String, Hidden: true},
			{Name: "author_id", Type: schema.Relation, To: "users"},
			{Name: "owner_ref", Type: schema.Relation, To: "users", Hidden: true},
		},
	})
	for _, tc := range []struct {
		rel  Relation
		want bool
	}{
		{HasMany("audit_trail", "audits", "note_id"), true},
		{BelongsTo("editor", "users", "editor_id"), true},
		{BelongsTo("owner_ref", "users", "owner_ref"), true},
		{HasMany("replies", "replies", "note_id"), false},
		{HasOne("summary", "summaries", "editor_id"), false},
		{BelongsTo("author_id", "users", "author_id"), false},
	} {
		if got := ent.HidesRelation(tc.rel); got != tc.want {
			t.Errorf("HidesRelation(%s %q fk %v) = %v, want %v", relKind(tc.rel.Type), tc.rel.Name, tc.rel.ForeignKey, got, tc.want)
		}
	}
}

func relKind(t RelationType) string {
	return map[RelationType]string{RelHasOne: "has_one", RelHasMany: "has_many", RelManyToOne: "belongs_to", RelManyToMany: "many_to_many"}[t]
}
