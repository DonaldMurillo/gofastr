package entity

import (
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
)

// Relation.Hidden is the opt-out for a relation no Hidden field of the
// entity covers: a has_many whose foreign key is Hidden on the target
// stays includable by default ("my notes"), and Hidden: true takes it out
// of every read surface HidesRelation guards.
func TestRelationHiddenOptsOut(t *testing.T) {
	ent := Define("users", EntityConfig{
		Fields: []schema.Field{{Name: "email", Type: schema.String}},
	})
	owned := HasMany("owned_notes", "notes", "owner_ref")
	if ent.HidesRelation(owned) {
		t.Fatalf("a has_many with no Hidden local field is hidden by default; want includable")
	}
	owned.Hidden = true
	if !ent.HidesRelation(owned) {
		t.Fatalf("a relation marked Hidden is not hidden")
	}
}
