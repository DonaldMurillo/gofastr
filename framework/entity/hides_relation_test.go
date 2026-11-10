package entity

import (
	"context"
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

// A belongs_to whose foreign key holds a NoQuery column is hidden too: the
// related record's key is the column's stored value, which NoQuery keeps
// off the query surface and a Mask keeps out of responses. Define marks a
// field with a Mask NoQuery, so both spellings hide the relation. A
// has_many's key lives on the target, and a relation merely named after a
// NoQuery field holds no key of it: neither is hidden.
func TestHidesRelationBehindNoQueryKey(t *testing.T) {
	ent := Define("payments", EntityConfig{
		Fields: []schema.Field{
			{Name: "memo", Type: schema.String},
			{Name: "account_id", Type: schema.String, Mask: func(_ context.Context, v any) any { return "****" }},
			{Name: "payer_ref", Type: schema.Relation, To: "accounts", NoQuery: true},
			{Name: "region", Type: schema.String, NoQuery: true},
			{Name: "ledger_id", Type: schema.Relation, To: "ledgers"},
		},
	})
	for _, tc := range []struct {
		rel  Relation
		want bool
	}{
		{BelongsTo("account", "accounts", "account_id"), true},
		{BelongsTo("payer_ref", "accounts", "payer_ref"), true},
		{BelongsTo("route", "routes", "region", "ledger_id"), true},
		{BelongsTo("ledger_id", "ledgers", "ledger_id"), false},
		{HasMany("refunds", "refunds", "account_id"), false},
		{HasOne("receipt", "receipts", "payer_ref"), false},
		{HasMany("region", "regions", "payment_id"), false},
	} {
		if got := ent.HidesRelation(tc.rel); got != tc.want {
			t.Errorf("HidesRelation(%s %q fk %v) = %v, want %v", relKind(tc.rel.Type), tc.rel.Name, tc.rel.ForeignKey, got, tc.want)
		}
	}
}
