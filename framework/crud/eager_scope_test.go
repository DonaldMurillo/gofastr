package crud

import (
	"context"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/framework/tenant"
)

// TestEagerLoadScopesTenant pins that the exported EagerLoad scopes a
// MultiTenant target to the caller's tenant, mirroring the include path's
// applyRelatedTenantScope. Two tenants' related rows share the same FK; ctx
// carries tenant-A, so only tenant-A's row may come back. Before the fix both
// tenants' rows loaded.
func TestEagerLoadScopesTenant(t *testing.T) {
	ctx := tenant.SetTenantID(context.Background(), "tenant-A")

	db := setupDB(t,
		`CREATE TABLE stposts (id TEXT PRIMARY KEY)`,
		`CREATE TABLE stcomments (id TEXT PRIMARY KEY, post_id TEXT, tenant_id TEXT, body TEXT)`,
	)
	seedRows(t, db, "stcomments", []map[string]any{
		{"id": "c-a", "post_id": "p1", "tenant_id": "tenant-A", "body": "A sees this"},
		{"id": "c-b", "post_id": "p1", "tenant_id": "tenant-B", "body": "B secret"},
	})

	commentsEnt := entity.Define("stcomments", entity.EntityConfig{Name: "stcomments", Table: "stcomments", Scope: &entity.ScopeConfig{MultiTenant: true}, Fields: []schema.Field{
		{Name: "post_id", Type: schema.String},
		{Name: "tenant_id", Type: schema.String},
		{Name: "body", Type: schema.String},
	},
	}.WithTimestamps(false))
	postsEnt := entity.Define("stposts", entity.EntityConfig{
		Name: "stposts", Table: "stposts",
	}.WithTimestamps(false))
	reg := stubRegistry{byName: map[string]*entity.Entity{
		"stposts": postsEnt, "stcomments": commentsEnt,
	}}

	rels := []entity.Relation{
		entity.HasMany("stcomments", "stcomments", "post_id"),
	}

	got, err := EagerLoad(ctx, db, postsEnt, rels, []string{"p1"}, reg)
	if err != nil {
		t.Fatalf("EagerLoad: %v", err)
	}
	comments, _ := got["p1"]["stcomments"].([]map[string]any)
	if len(comments) != 1 {
		t.Fatalf("SECURITY: cross-tenant row leaked via EagerLoad: got %d comments, want 1 (%v)", len(comments), comments)
	}
	if comments[0]["id"] != "c-a" {
		t.Errorf("expected tenant-A comment c-a, got %v", comments[0]["id"])
	}
	if comments[0]["body"] == "B secret" {
		t.Errorf("SECURITY: tenant-B row leaked via EagerLoad")
	}
}

// TestEagerLoadScopesOwner pins that EagerLoad scopes an owner-scoped target
// to the caller's owner, mirroring applyRelatedOwnerScope. Two owners' rows
// share the same FK; ctx carries owner alice, so only alice's row may come
// back. Before the fix both owners' rows loaded.
func TestEagerLoadScopesOwner(t *testing.T) {
	installOwnerExtractor(t)
	ctx := signedIn("alice")

	db := setupDB(t,
		`CREATE TABLE soposts (id TEXT PRIMARY KEY)`,
		`CREATE TABLE sonotes (id TEXT PRIMARY KEY, post_id TEXT, user_id TEXT, body TEXT)`,
	)
	seedRows(t, db, "sonotes", []map[string]any{
		{"id": "n-a", "post_id": "p1", "user_id": "alice", "body": "alice sees this"},
		{"id": "n-b", "post_id": "p1", "user_id": "bob", "body": "bob secret"},
	})

	notesEnt := entity.Define("sonotes", entity.EntityConfig{Name: "sonotes", Table: "sonotes", Scope: &entity.ScopeConfig{OwnerField: "user_id"}, Fields: []schema.Field{
		{Name: "post_id", Type: schema.String},
		{Name: "user_id", Type: schema.String},
		{Name: "body", Type: schema.String},
	},
	}.WithTimestamps(false))
	postsEnt := entity.Define("soposts", entity.EntityConfig{
		Name: "soposts", Table: "soposts",
	}.WithTimestamps(false))
	reg := stubRegistry{byName: map[string]*entity.Entity{
		"soposts": postsEnt, "sonotes": notesEnt,
	}}

	rels := []entity.Relation{
		entity.HasMany("sonotes", "sonotes", "post_id"),
	}

	got, err := EagerLoad(ctx, db, postsEnt, rels, []string{"p1"}, reg)
	if err != nil {
		t.Fatalf("EagerLoad: %v", err)
	}
	notes, _ := got["p1"]["sonotes"].([]map[string]any)
	if len(notes) != 1 {
		t.Fatalf("SECURITY: cross-owner row leaked via EagerLoad: got %d notes, want 1 (%v)", len(notes), notes)
	}
	if notes[0]["id"] != "n-a" {
		t.Errorf("expected alice's note n-a, got %v", notes[0]["id"])
	}
	if notes[0]["body"] == "bob secret" {
		t.Errorf("SECURITY: bob's row leaked via EagerLoad")
	}
}
