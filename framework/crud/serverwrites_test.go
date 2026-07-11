package crud

import (
	"context"
	"database/sql"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/framework/tenant"
)

// swHandler builds a CrudHandler for server-writes tests. The owner
// extractor is installed (harmless when OwnerField is unset) so the
// owner-scoped test variants can reuse the same helper.
func swHandler(t *testing.T, cfg entity.EntityConfig, ddl string) (*CrudHandler, *sql.DB) {
	return setupSecurityTestHandler(t, cfg, ddl)
}

// TestServerWritesPersistHiddenField asserts that a Hidden field supplied
// in an in-process CreateOne body IS persisted when the caller opts in via
// WithServerWrites.
func TestServerWritesPersistHiddenField(t *testing.T) {
	ch, db := swHandler(t, entity.EntityConfig{
		Table: "sw_widgets",
		Fields: []schema.Field{
			{Name: "name", Type: schema.String, Required: true},
			{Name: "secret_code", Type: schema.String, Hidden: true},
		},
	}.WithTimestamps(false),
		`CREATE TABLE sw_widgets (id TEXT PRIMARY KEY, name TEXT NOT NULL, secret_code TEXT)`)

	ctx := WithServerWrites(context.Background())
	if _, err := ch.CreateOne(ctx, map[string]any{"name": "x", "secret_code": "TOPSECRET"}); err != nil {
		t.Fatalf("CreateOne: %v", err)
	}
	var got sql.NullString
	if err := db.QueryRow("SELECT secret_code FROM sw_widgets LIMIT 1").Scan(&got); err != nil {
		t.Fatalf("read secret_code: %v", err)
	}
	if !got.Valid || got.String != "TOPSECRET" {
		t.Errorf("hidden field secret_code = %v, want TOPSECRET", got)
	}
}

// TestServerWritesPersistReadOnlyField asserts a ReadOnly field is
// persisted under WithServerWrites.
func TestServerWritesPersistReadOnlyField(t *testing.T) {
	ch, db := swHandler(t, entity.EntityConfig{
		Table: "sw_ro",
		Fields: []schema.Field{
			{Name: "name", Type: schema.String, Required: true},
			{Name: "score", Type: schema.Int, ReadOnly: true},
		},
	}.WithTimestamps(false),
		`CREATE TABLE sw_ro (id TEXT PRIMARY KEY, name TEXT NOT NULL, score INTEGER)`)

	ctx := WithServerWrites(context.Background())
	if _, err := ch.CreateOne(ctx, map[string]any{"name": "x", "score": 42}); err != nil {
		t.Fatalf("CreateOne: %v", err)
	}
	var got sql.NullInt64
	if err := db.QueryRow("SELECT score FROM sw_ro LIMIT 1").Scan(&got); err != nil {
		t.Fatalf("read score: %v", err)
	}
	if !got.Valid || got.Int64 != 42 {
		t.Errorf("read-only field score = %v, want 42", got)
	}
}

// TestServerWritesKeepOwnerProtected asserts that even with WithServerWrites
// a client-supplied owner id in the body cannot override the context owner.
func TestServerWritesKeepOwnerProtected(t *testing.T) {
	ch, db := swHandler(t, entity.EntityConfig{
		Table: "sw_owned",
		Fields: []schema.Field{
			{Name: "user_id", Type: schema.String, Required: true},
			{Name: "name", Type: schema.String},
		},
		OwnerField: "user_id",
	}.WithTimestamps(false),
		`CREATE TABLE sw_owned (id TEXT PRIMARY KEY, user_id TEXT NOT NULL, name TEXT)`)

	ctx := WithServerWrites(ctxWithUser("alice"))
	if _, err := ch.CreateOne(ctx, map[string]any{"name": "x", "user_id": "victim"}); err != nil {
		t.Fatalf("CreateOne: %v", err)
	}
	var got string
	if err := db.QueryRow("SELECT user_id FROM sw_owned LIMIT 1").Scan(&got); err != nil {
		t.Fatalf("read user_id: %v", err)
	}
	if got == "victim" {
		t.Errorf("SECURITY: body-supplied user_id='victim' persisted under WithServerWrites")
	}
	if got != "alice" {
		t.Errorf("user_id = %q, want alice (context owner)", got)
	}
}

// TestServerWritesKeepTenantProtected asserts that even with WithServerWrites
// a client-supplied tenant_id in the body cannot override the context tenant.
func TestServerWritesKeepTenantProtected(t *testing.T) {
	ch, db := swHandler(t, entity.EntityConfig{
		Table: "sw_tenant",
		Fields: []schema.Field{
			{Name: "name", Type: schema.String},
		},
		MultiTenant: true,
	}.WithTimestamps(false),
		`CREATE TABLE sw_tenant (id TEXT PRIMARY KEY, tenant_id TEXT, name TEXT)`)

	ctx := WithServerWrites(tenant.SetTenantID(context.Background(), "acme"))
	if _, err := ch.CreateOne(ctx, map[string]any{"name": "x", "tenant_id": "evil"}); err != nil {
		t.Fatalf("CreateOne: %v", err)
	}
	var got string
	if err := db.QueryRow("SELECT tenant_id FROM sw_tenant LIMIT 1").Scan(&got); err != nil {
		t.Fatalf("read tenant_id: %v", err)
	}
	if got == "evil" {
		t.Errorf("SECURITY: body-supplied tenant_id='evil' persisted under WithServerWrites")
	}
	if got != "acme" {
		t.Errorf("tenant_id = %q, want acme (context tenant)", got)
	}
}

// TestCreateOneStillSkipsHiddenByDefault asserts that WITHOUT WithServerWrites
// a Hidden field in the body is silently dropped.
func TestCreateOneStillSkipsHiddenByDefault(t *testing.T) {
	ch, db := swHandler(t, entity.EntityConfig{
		Table: "sw_def",
		Fields: []schema.Field{
			{Name: "name", Type: schema.String, Required: true},
			{Name: "secret_code", Type: schema.String, Hidden: true},
		},
	}.WithTimestamps(false),
		`CREATE TABLE sw_def (id TEXT PRIMARY KEY, name TEXT NOT NULL, secret_code TEXT)`)

	// No WithServerWrites — the hidden field must be skipped.
	if _, err := ch.CreateOne(context.Background(), map[string]any{"name": "x", "secret_code": "LEAK"}); err != nil {
		t.Fatalf("CreateOne: %v", err)
	}
	var got sql.NullString
	_ = db.QueryRow("SELECT secret_code FROM sw_def LIMIT 1").Scan(&got)
	if got.Valid && got.String == "LEAK" {
		t.Errorf("hidden field secret_code persisted without WithServerWrites opt-in")
	}
}
