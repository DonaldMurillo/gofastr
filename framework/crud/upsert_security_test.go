package crud

import (
	"context"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/handler"
	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/framework/tenant"
)

func upsertSecurityContext(userID, tenantID string) context.Context {
	ctx := context.Background()
	if userID != "" {
		ctx = handler.SetUser(ctx, &testUser{id: userID})
	}
	if tenantID != "" {
		ctx = tenant.SetTenantID(ctx, tenantID)
	}
	return ctx
}

// TestUpsert_RefusesSoftDeletedResurrection pins that an UpsertOne
// targeting a row already marked soft-deleted refuses rather than
// silently clearing deleted_at via ON CONFLICT DO UPDATE. Soft delete
// is a compliance / forensic contract; bypassing it through upsert
// would smuggle the row past the audit story.
func TestUpsert_RefusesSoftDeletedResurrection(t *testing.T) {
	installSecurityOwnerExtractor(t)
	cfg := makeEntityConfig("posts", "posts", "", []schema.Field{
		{Name: "id", Type: schema.String},
		{Name: "title", Type: schema.String, Required: true},
		{Name: "body", Type: schema.Text},
	}, func(c *entity.EntityConfig) { c.SoftDelete = true })
	ch, db := setupSecurityTestHandler(t, cfg,
		`CREATE TABLE posts (id TEXT PRIMARY KEY, title TEXT, body TEXT, deleted_at TEXT)`)
	seedRows(t, db, "posts", []map[string]any{
		{"id": "post-1", "title": "deleted", "body": "legacy", "deleted_at": "2024-01-01T00:00:00Z"},
	})

	_, err := ch.UpsertOne(context.Background(), map[string]any{
		"id":    "post-1",
		"title": "mutated",
		"body":  "tampered",
	})
	if err == nil {
		t.Fatalf("UpsertOne resurrected a soft-deleted row via ON CONFLICT DO UPDATE")
	}

	// Sanity: the row itself was not mutated.
	var body string
	if err := db.QueryRow("SELECT body FROM posts WHERE id = $1", "post-1").Scan(&body); err != nil {
		t.Fatalf("read body: %v", err)
	}
	if body != "legacy" {
		t.Fatalf("rejected upsert still mutated row body (got %q)", body)
	}
}

func TestUpsert_OwnerFieldStampedFromContext(t *testing.T) {
	installSecurityOwnerExtractor(t)
	ch, _ := setupSecurityTestHandler(t, makeEntityConfig("posts", "posts", "owner_id", []schema.Field{
		{Name: "id", Type: schema.String},
		{Name: "owner_id", Type: schema.String},
		{Name: "title", Type: schema.String},
	}), `CREATE TABLE posts (id TEXT PRIMARY KEY, owner_id TEXT, title TEXT)`)

	row, err := ch.UpsertOne(upsertSecurityContext("alice", ""), map[string]any{
		"id":    "post-1",
		"title": "hello",
	})
	if err != nil {
		t.Fatalf("upsert failed unexpectedly: %v", err)
	}
	if row["owner_id"] != "alice" {
		t.Fatalf("SECURITY: [upsert-owner] upsert row owner_id = %v, want alice. Attack: owner field not stamped from authenticated context.", row["owner_id"])
	}
}

func TestUpsert_BodyOwnerFieldCannotOverrideContext(t *testing.T) {
	installSecurityOwnerExtractor(t)
	ch, _ := setupSecurityTestHandler(t, makeEntityConfig("posts", "posts", "owner_id", []schema.Field{
		{Name: "id", Type: schema.String},
		{Name: "owner_id", Type: schema.String},
		{Name: "title", Type: schema.String},
	}), `CREATE TABLE posts (id TEXT PRIMARY KEY, owner_id TEXT, title TEXT)`)

	row, err := ch.UpsertOne(upsertSecurityContext("bob", ""), map[string]any{
		"id":       "post-1",
		"owner_id": "alice",
		"title":    "tampered",
	})
	if err != nil {
		t.Fatalf("upsert failed unexpectedly: %v", err)
	}
	if row["owner_id"] != "bob" {
		t.Fatalf("SECURITY: [upsert-owner] body-supplied owner_id %v overrode authenticated user bob. Attack: mass-assignment of owner field on upsert.", row["owner_id"])
	}
}

func TestUpsert_AnonymousCallerRejectedOnOwnerScopedEntity(t *testing.T) {
	installSecurityOwnerExtractor(t)
	ch, _ := setupSecurityTestHandler(t, makeEntityConfig("posts", "posts", "owner_id", []schema.Field{
		{Name: "id", Type: schema.String},
		{Name: "owner_id", Type: schema.String},
		{Name: "title", Type: schema.String},
	}), `CREATE TABLE posts (id TEXT PRIMARY KEY, owner_id TEXT, title TEXT)`)

	if _, err := ch.UpsertOne(context.Background(), map[string]any{
		"id":    "post-1",
		"title": "anonymous",
	}); err == nil {
		t.Fatal("SECURITY: [upsert-owner] anonymous upsert succeeded on owner-scoped entity. Attack: unauthenticated orphan row creation.")
	}
}

func TestUpsert_AnonymousBodyOwnerFieldRejected(t *testing.T) {
	installSecurityOwnerExtractor(t)
	ch, _ := setupSecurityTestHandler(t, makeEntityConfig("posts", "posts", "owner_id", []schema.Field{
		{Name: "id", Type: schema.String},
		{Name: "owner_id", Type: schema.String},
		{Name: "title", Type: schema.String},
	}), `CREATE TABLE posts (id TEXT PRIMARY KEY, owner_id TEXT, title TEXT)`)

	if _, err := ch.UpsertOne(context.Background(), map[string]any{
		"id":       "post-1",
		"owner_id": "alice",
		"title":    "forged",
	}); err == nil {
		t.Fatal("SECURITY: [upsert-owner] anonymous upsert accepted caller-supplied owner_id. Attack: forged ownership on unauthenticated upsert.")
	}
}

func TestUpsert_MissingTenantContextRejected(t *testing.T) {
	ch, _ := setupSecurityTestHandler(t, entity.EntityConfig{
		Table: "posts",
		Fields: []schema.Field{
			{Name: "id", Type: schema.String},
			{Name: "tenant_id", Type: schema.String},
			{Name: "title", Type: schema.String},
		},
		MultiTenant: true,
	}.WithTimestamps(false), `CREATE TABLE posts (id TEXT PRIMARY KEY, tenant_id TEXT, title TEXT)`)

	if _, err := ch.UpsertOne(context.Background(), map[string]any{
		"id":    "post-1",
		"title": "orphan",
	}); err == nil {
		t.Fatal("SECURITY: [upsert-tenant] multi-tenant upsert succeeded with no tenant in context. Attack: orphan tenant row creation.")
	}
}

func TestUpsert_BodyTenantFieldWithoutContextRejected(t *testing.T) {
	ch, _ := setupSecurityTestHandler(t, entity.EntityConfig{
		Table: "posts",
		Fields: []schema.Field{
			{Name: "id", Type: schema.String},
			{Name: "tenant_id", Type: schema.String},
			{Name: "title", Type: schema.String},
		},
		MultiTenant: true,
	}.WithTimestamps(false), `CREATE TABLE posts (id TEXT PRIMARY KEY, tenant_id TEXT, title TEXT)`)

	if _, err := ch.UpsertOne(context.Background(), map[string]any{
		"id":        "post-1",
		"tenant_id": "victim-tenant",
		"title":     "forged",
	}); err == nil {
		t.Fatal("SECURITY: [upsert-tenant] attacker-supplied tenant_id accepted without tenant context. Attack: forged tenant assignment on upsert.")
	}
}
