package framework

import (
	"fmt"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/framework/routegroup"
)

// Each test below exercises ONE multi-version union defect (findings F5–F10
// from the review). They register conflicting versions via GroupEntity and
// assert the SECOND registration panics — the conflict must surface at
// registration (app.Entity / app.GroupEntity), not silently at boot. A test
// that names the right symbol in the panic message is the contract; the fix
// lives in registry.go's checkVersionCompat + checkColumnConflicts.

// TestUnion_RejectsSameNameDifferentTable (F5): two versions of one entity
// name MUST share one physical table. Registering /api/v1/posts on table
// "posts_v1" and /api/v2/posts on table "posts_v2" would make the name-union
// keep only the lex-first table and never create the other — every v2 request
// then hits a missing table.
func TestUnion_RejectsSameNameDifferentTable(t *testing.T) {
	defer expectPanic(t, "same entity name with different tables")()
	app := NewApp(WithoutDefaultMiddleware())
	v1 := app.Group("/api/v1")
	v2 := app.Group("/api/v2")
	app.GroupEntity(v1, "posts", entity.EntityConfig{
		Table:  "posts_v1",
		Fields: []schema.Field{{Name: "title", Type: schema.String}},
	}.WithTimestamps(false))
	app.GroupEntity(v2, "posts", entity.EntityConfig{
		Table:  "posts_v2",
		Fields: []schema.Field{{Name: "title", Type: schema.String}},
	}.WithTimestamps(false))
}

// TestUnion_RejectsVersionExclusiveRequired (F6): a Required-no-default column
// added in v2 but absent in v1 creates a NOT NULL column the older version can
// never supply — every complete POST /api/v1/posts is rejected by the DB.
func TestUnion_RejectsVersionExclusiveRequired(t *testing.T) {
	defer expectPanicContaining(t, "version-exclusive mandatory column", "summary")()
	app := NewApp(WithoutDefaultMiddleware())
	v1 := app.Group("/api/v1")
	v2 := app.Group("/api/v2")
	app.GroupEntity(v1, "posts", entity.EntityConfig{
		Table:  "posts",
		Fields: []schema.Field{{Name: "title", Type: schema.String, Required: true}},
	}.WithTimestamps(false))
	app.GroupEntity(v2, "posts", entity.EntityConfig{
		Table: "posts",
		Fields: []schema.Field{
			{Name: "title", Type: schema.String, Required: true},
			{Name: "summary", Type: schema.Text, Required: true}, // mandatory + exclusive → breaks v1
		},
	}.WithTimestamps(false))
}

// TestUnion_RejectsConflictingStringMax (F7): String.Max selects VARCHAR(n),
// which is DDL, not validation. v1 title Max 100 and v2 title Max 200 must
// conflict — otherwise the lex-first width wins and a valid v2 request longer
// than that width fails at Postgres.
func TestUnion_RejectsConflictingStringMax(t *testing.T) {
	max100 := float64(100)
	max200 := float64(200)
	defer expectPanicContaining(t, "conflicting String.Max", "title")()
	app := NewApp(WithoutDefaultMiddleware())
	v1 := app.Group("/api/v1")
	v2 := app.Group("/api/v2")
	app.GroupEntity(v1, "posts", entity.EntityConfig{
		Table:  "posts",
		Fields: []schema.Field{{Name: "title", Type: schema.String, Max: &max100}},
	}.WithTimestamps(false))
	app.GroupEntity(v2, "posts", entity.EntityConfig{
		Table:  "posts",
		Fields: []schema.Field{{Name: "title", Type: schema.String, Max: &max200}},
	}.WithTimestamps(false))
}

// TestUnion_RejectsConflictingNamedIndex (F8): a named index on the shared
// table must be identical across versions. v1 declares idx_posts_slug
// non-unique, v2 declares it UNIQUE — the merge would keep the first and
// silently violate v2's declared invariant.
func TestUnion_RejectsConflictingNamedIndex(t *testing.T) {
	defer expectPanicContaining(t, "conflicting named index", "idx_posts_slug")()
	app := NewApp(WithoutDefaultMiddleware())
	v1 := app.Group("/api/v1")
	v2 := app.Group("/api/v2")
	app.GroupEntity(v1, "posts", entity.EntityConfig{
		Table:   "posts",
		Fields:  []schema.Field{{Name: "slug", Type: schema.String}},
		Indices: []entity.Index{{Name: "idx_posts_slug", Columns: []string{"slug"}}},
	}.WithTimestamps(false))
	app.GroupEntity(v2, "posts", entity.EntityConfig{
		Table:  "posts",
		Fields: []schema.Field{{Name: "slug", Type: schema.String}},
		Indices: []entity.Index{
			{Name: "idx_posts_slug", Columns: []string{"slug"}, Unique: true}, // uniqueness differs
		},
	}.WithTimestamps(false))
}

// TestUnion_RejectsConflictingForeignKey (F9): a foreign-key column must
// reference the same target across versions. v1 points owner_id at users, v2
// at teams — the DDL would reference whichever the merge saw first, silently
// mis-typing the other version's relation.
func TestUnion_RejectsConflictingForeignKey(t *testing.T) {
	// Register the FK targets so the entity declarations are well-formed.
	app := NewApp(WithoutDefaultMiddleware())
	app.Entity("users", entity.EntityConfig{
		Table:  "users",
		Fields: []schema.Field{{Name: "id", Type: schema.String}},
	}.WithTimestamps(false))
	app.Entity("teams", entity.EntityConfig{
		Table:  "teams",
		Fields: []schema.Field{{Name: "id", Type: schema.String}},
	}.WithTimestamps(false))
	v1 := app.Group("/api/v1")
	v2 := app.Group("/api/v2")
	app.GroupEntity(v1, "posts", entity.EntityConfig{
		Table:     "posts",
		Fields:    []schema.Field{{Name: "owner_id", Type: schema.String}},
		Relations: []entity.Relation{entity.BelongsTo("author", "users", "owner_id")},
	}.WithTimestamps(false))
	defer expectPanicContaining(t, "conflicting FK target", "owner_id")()
	app.GroupEntity(v2, "posts", entity.EntityConfig{
		Table:     "posts",
		Fields:    []schema.Field{{Name: "owner_id", Type: schema.String}},
		Relations: []entity.Relation{entity.BelongsTo("author", "teams", "owner_id")}, // target differs
	}.WithTimestamps(false))
}

// TestUnion_RejectsMixedManagedUnmanaged (F10): versions of one entity name
// must agree on managed/unmanaged. An unmanaged representative suppresses
// migration for the whole union, so a managed version's columns would never
// be created.
func TestUnion_RejectsMixedManagedUnmanaged(t *testing.T) {
	defer expectPanicContaining(t, "mixed managed/unmanaged", "managed")()
	app := NewApp(WithoutDefaultMiddleware())
	v1 := app.Group("/api/v1")
	v2 := app.Group("/api/v2")
	app.GroupEntity(v1, "posts", entity.EntityConfig{
		Table:     "posts",
		Unmanaged: true,
		Fields:    []schema.Field{{Name: "title", Type: schema.String}},
	}.WithTimestamps(false))
	app.GroupEntity(v2, "posts", entity.EntityConfig{
		Table:  "posts",
		Fields: []schema.Field{{Name: "title", Type: schema.String}},
	}.WithTimestamps(false))
}

// TestUnion_RejectsMismatchedRowScopes (F12): MultiTenant, OwnerField and
// SoftDelete decide WHICH ROWS of the shared table a request may see or
// destroy, and each is enforced per-version at query time. An unscoped version
// beside a scoped one is a read of every tenant's (or every user's) rows
// through the weaker prefix; a hard-delete version physically removes rows the
// soft-delete version expects to be able to restore.
//
// Asserted at the Registry rather than through GroupEntity: Register is where
// checkVersionCompat runs, and the error carries more than the panic text.
func TestUnion_RejectsMismatchedRowScopes(t *testing.T) {
	titleOnly := []schema.Field{{Name: "title", Type: schema.String}}
	withOwner := []schema.Field{
		{Name: "title", Type: schema.String},
		{Name: "owner_id", Type: schema.String, Hidden: true},
	}
	tests := []struct {
		name string
		v1   entity.EntityConfig
		v2   entity.EntityConfig
		want string
	}{
		{
			name: "tenant-scoped beside unscoped",
			v1:   entity.EntityConfig{Fields: titleOnly},
			v2:   entity.EntityConfig{Fields: titleOnly, Scope: &entity.ScopeConfig{MultiTenant: true}},
			want: "tenant scoping",
		},
		{
			name: "soft-delete beside hard-delete",
			v1:   entity.EntityConfig{Fields: titleOnly},
			v2:   entity.EntityConfig{Fields: titleOnly, Scope: &entity.ScopeConfig{SoftDelete: true}},
			want: "delete semantics",
		},
		{
			name: "owner-scoped beside unscoped",
			v1:   entity.EntityConfig{Fields: withOwner},
			v2:   entity.EntityConfig{Fields: withOwner, Scope: &entity.ScopeConfig{OwnerField: "owner_id"}},
			want: "owner scoping",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			reg := NewRegistry()
			v1 := entity.Define("records", tc.v1.WithTimestamps(false))
			v1.Version = "/api/v1"
			v2 := entity.Define("records", tc.v2.WithTimestamps(false))
			v2.Version = "/api/v2"
			if err := reg.Register(v1); err != nil {
				t.Fatalf("register v1: %v", err)
			}
			err := reg.Register(v2)
			if err == nil {
				t.Fatal("registry accepted versions with incompatible row-isolation policy")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error should name %q:\n%s", tc.want, err)
			}
		})
	}
}

// The mirror of F12: versions that AGREE on all three still register.
func TestUnion_AcceptsMatchingRowScopes(t *testing.T) {
	cfg := func() entity.EntityConfig {
		return entity.EntityConfig{Fields: []schema.Field{
			{Name: "title", Type: schema.String},
			{Name: "owner_id", Type: schema.String, Hidden: true},
		}, Scope: &entity.ScopeConfig{MultiTenant: true, OwnerField: "owner_id", SoftDelete: true},
		}
	}
	reg := NewRegistry()
	v1 := entity.Define("records", cfg().WithTimestamps(false))
	v1.Version = "/api/v1"
	v2 := entity.Define("records", cfg().WithTimestamps(false))
	v2.Version = "/api/v2"
	if err := reg.Register(v1); err != nil {
		t.Fatalf("register v1: %v", err)
	}
	if err := reg.Register(v2); err != nil {
		t.Fatalf("versions agreeing on row scoping must register: %v", err)
	}
}

// --- helpers ---

// expectPanic returns a deferred func that fails the test if no panic occurred.
func expectPanic(t *testing.T, what string) func() {
	t.Helper()
	return func() {
		if r := recover(); r == nil {
			t.Fatalf("expected panic: %s", what)
		}
	}
}

// expectPanicContaining returns a deferred func that fails the test if no panic
// occurred, or if the panic message does not contain the wanted substring.
func expectPanicContaining(t *testing.T, what, want string) func() {
	t.Helper()
	return func() {
		r := recover()
		if r == nil {
			t.Fatalf("expected panic: %s", what)
		}
		msg := fmt.Sprint(r)
		if !strings.Contains(msg, want) {
			t.Errorf("panic message should contain %q:\n%s", want, msg)
		}
	}
}

// Compile-time assertion that routegroup is used (the GroupEntity path).
var _ = routegroup.WithMCPNamespace
