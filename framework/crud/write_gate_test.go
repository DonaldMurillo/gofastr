package crud

import (
	"context"
	"database/sql"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/access"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/framework/owner"
	"github.com/DonaldMurillo/gofastr/framework/tenant"
)

func writeGateHandler(t *testing.T, cfg entity.EntityConfig) *CrudHandler {
	t.Helper()
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Skip("sqlite3 driver not available")
	}
	t.Cleanup(func() { db.Close() })
	ent := entity.Define("projects", cfg.WithTimestamps(false))
	ent.SetDB(db)
	return NewCrudHandler(ent, db)
}

type writeGateOwnerKey struct{}

// The update and delete gates ask the Decider about the record, the way
// PUT and DELETE /projects/{id} do, and each asks its own permission.
func TestWriteGatesAskDeciderPerRecord(t *testing.T) {
	ch := writeGateHandler(t, entity.EntityConfig{
		Fields: []schema.Field{{Name: "name", Type: schema.String}},
		Exposure: &entity.ExposureConfig{Access: entity.AccessControl{
			Read: "projects:read", Update: "projects:update", Delete: "projects:delete",
		}},
	})
	policy := access.NewRolePolicy()
	if err := policy.Grant("member", "projects:read", "projects:update", "projects:delete"); err != nil {
		t.Fatal(err)
	}
	if err := policy.Grant("editor", "projects:read", "projects:update"); err != nil {
		t.Fatal(err)
	}
	deny := func(_ context.Context, _ []string, _ access.Permission, ref access.Ref) access.Decision {
		if ref.ID == "denied" {
			return access.DecisionDeny
		}
		return access.DecisionAbstain
	}
	member := access.WithDecider(access.WithRoles(access.WithPolicy(context.Background(), policy), []string{"member"}), deny)
	if !ch.CanUpdateRecordScoped(member, "ok") || !ch.CanDeleteRecordScoped(member, "ok") {
		t.Fatal("member refused a record the decider abstains on")
	}
	if ch.CanUpdateRecordScoped(member, "denied") {
		t.Error("update allowed on a record the decider denies")
	}
	if ch.CanDeleteRecordScoped(member, "denied") {
		t.Error("delete allowed on a record the decider denies")
	}
	editor := access.WithRoles(access.WithPolicy(context.Background(), policy), []string{"editor"})
	if !ch.CanUpdateRecordScoped(editor, "ok") {
		t.Error("editor refused an update it holds")
	}
	if ch.CanDeleteRecordScoped(editor, "ok") {
		t.Error("editor allowed a delete it does not hold")
	}
}

// Owner, tenant and the default session posture refuse the same way the
// routes' requireScope does.
func TestWriteGatesRequireScope(t *testing.T) {
	owned := writeGateHandler(t, entity.EntityConfig{
		Fields: []schema.Field{{Name: "name", Type: schema.String}, {Name: "owner_id", Type: schema.String}},
		Scope:  &entity.ScopeConfig{OwnerField: "owner_id"},
	})
	if owned.CanUpdateRecordScoped(context.Background(), "x") || owned.CanDeleteRecordScoped(context.Background(), "x") {
		t.Error("an owner-scoped entity allowed a write with no owner on ctx")
	}
	prev := owner.GetExtractor()
	owner.SetExtractor(func(ctx context.Context) (any, bool) {
		v, ok := ctx.Value(writeGateOwnerKey{}).(string)
		return v, ok
	})
	t.Cleanup(func() { owner.SetExtractor(prev) })
	if !owned.CanDeleteRecordScoped(context.WithValue(context.Background(), writeGateOwnerKey{}, "u1"), "x") {
		t.Error("an owner-scoped entity refused a write with an owner on ctx")
	}

	tenanted := writeGateHandler(t, entity.EntityConfig{
		Fields:   []schema.Field{{Name: "name", Type: schema.String}},
		Scope:    &entity.ScopeConfig{MultiTenant: true},
		Exposure: &entity.ExposureConfig{Public: true},
	})
	if tenanted.CanUpdateRecordScoped(context.Background(), "x") {
		t.Error("a multi-tenant entity allowed a write with no tenant on ctx")
	}
	if !tenanted.CanUpdateRecordScoped(tenant.SetTenantID(context.Background(), "t1"), "x") {
		t.Error("a multi-tenant entity refused a write with a tenant on ctx")
	}

	plain := writeGateHandler(t, entity.EntityConfig{Fields: []schema.Field{{Name: "name", Type: schema.String}}})
	if plain.CanUpdateRecordScoped(context.Background(), "x") {
		t.Error("a default-posture entity allowed an anonymous write")
	}
}
