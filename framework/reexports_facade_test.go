package framework

import (
	"context"
	"database/sql"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/core/handler"
	"github.com/DonaldMurillo/gofastr/core/mcp"
	"github.com/DonaldMurillo/gofastr/core/router"
	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/access"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/framework/migrate"
	"github.com/DonaldMurillo/gofastr/framework/tenant"
	_ "github.com/DonaldMurillo/gofastr/sqlite/stdlib"
)

// These tests exercise every facade wrapper function in reexports_*.go
// through a real call, so a wrapper that stops delegating (or a signature
// that drifts from its subpackage) fails here instead of in a consumer.
// The facade is the public API; each wrapper body is contract, not glue.

func facadeEntity(t *testing.T, name string) *entity.Entity {
	t.Helper()
	return Define(name, EntityConfig{
		Fields: []schema.Field{{Name: "title", Type: schema.String}},
	}.WithTimestamps(false))
}

func facadeDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func TestFacadeAccessWrappersDelegate(t *testing.T) {
	policy := NewRolePolicy()
	if policy == nil {
		t.Fatal("NewRolePolicy returned nil")
	}
	if err := policy.Grant("editor", Permission("posts:write")); err != nil {
		t.Fatal(err)
	}
	ctx := WithPolicy(context.Background(), policy)
	ctx = WithRoles(ctx, []string{"editor"})
	if !Can(ctx, Permission("posts:write")) {
		t.Error("Can: editor should hold posts:write")
	}
	if got := GetRoles(ctx); len(got) != 1 || got[0] != "editor" {
		t.Errorf("GetRoles = %v", got)
	}
	if got := GetPermissions(ctx); len(got) == 0 {
		t.Errorf("GetPermissions = %v, want the granted permission", got)
	}

	db := facadeDB(t)
	if store := NewGrantStore(db, policy); store == nil {
		t.Error("NewGrantStore returned nil")
	}

	resolver := NewCachedResolver(func(context.Context) []string { return []string{"editor"} }, WithResolverTTL(time.Minute))
	if resolver == nil {
		t.Fatal("NewCachedResolver returned nil")
	}

	// Middleware chain: AccessMiddleware installs policy+roles, then
	// RequirePermission gates on them.
	var reached bool
	h := AccessMiddleware(policy, resolver.Resolve)(
		RequirePermission(Permission("posts:write"))(
			http.HandlerFunc(func(http.ResponseWriter, *http.Request) { reached = true })))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if !reached || rec.Code != http.StatusOK {
		t.Errorf("middleware chain: reached=%v code=%d", reached, rec.Code)
	}

	// Decider seam: an allow decision wins over the (permission-less) policy.
	allowAll := access.Decider(func(context.Context, []string, access.Permission, access.Ref) access.Decision {
		return DecisionAllow
	})
	dctx := WithDecider(context.Background(), allowAll)
	if GetDecider(dctx) == nil {
		t.Error("GetDecider returned nil after WithDecider")
	}
	if !CanResource(dctx, Permission("projects:edit"), access.Ref{Type: "project", ID: "42"}) {
		t.Error("CanResource: allow-all decider should allow")
	}
	var sawDecider bool
	dm := DeciderMiddleware(allowAll)(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		sawDecider = GetDecider(r.Context()) != nil
	}))
	dm.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))
	if !sawDecider {
		t.Error("DeciderMiddleware did not install the decider")
	}
}

func TestFacadeEntityWrappersDelegate(t *testing.T) {
	ent := facadeEntity(t, "facade_posts")
	if ent.GetName() != "facade_posts" {
		t.Errorf("Define name = %q", ent.GetName())
	}

	for _, rel := range []Relation{
		HasOne("profile", "profiles", "user_id"),
		HasMany("posts", "posts", "author_id"),
		BelongsTo("author", "users", "author_id"),
		ManyToMany("tags", "tags", "post_tags", "post_id", "tag_id"),
	} {
		if rel.Name == "" || rel.Entity == "" {
			t.Errorf("relation constructor returned zero value: %+v", rel)
		}
	}

	if cond := NewStringColumn("title").Eq("x"); cond.SQL() == "" {
		t.Error("NewStringColumn condition rendered empty SQL")
	}
	NewIntColumn("n")
	NewFloatColumn("f")
	NewBoolColumn("b")
	NewTimestampColumn("ts")
	NewUUIDColumn("id")

	reg := NewValidationRegistry()
	if reg == nil {
		t.Fatal("NewValidationRegistry returned nil")
	}
	if errs := Required("title")(context.Background(), map[string]any{}); len(errs) == 0 {
		t.Error("Required: missing field should error")
	}
	unique := Unique("title", func(context.Context, any) bool { return false })
	if errs := unique(context.Background(), map[string]any{"title": "dup"}); len(errs) == 0 {
		t.Error("Unique: false check should error")
	}
	custom := Custom("always", func(context.Context, map[string]any) map[string]string {
		return map[string]string{"title": "nope"}
	})
	if errs := custom(context.Background(), map[string]any{}); len(errs) == 0 {
		t.Error("Custom: returned errors should propagate")
	}
	if lines := FormatValidationErrors(map[string]string{"title": "bad"}); len(lines) != 1 {
		t.Errorf("FormatValidationErrors = %v", lines)
	}

	cond := And(Or(NewStringColumn("title").Eq("a"), NewStringColumn("title").Eq("b")),
		Not(NewStringColumn("title").Eq("c")))
	if cond.SQL() == "" {
		t.Error("And/Or/Not condition rendered empty SQL")
	}
}

func TestFacadeCrudWrappersDelegate(t *testing.T) {
	db := facadeDB(t)
	ent := facadeEntity(t, "facade_items")
	if err := MigrateEntity(db, ent); err != nil {
		t.Fatalf("MigrateEntity: %v", err)
	}
	ent.SetDB(db)

	ch := NewCrudHandler(ent, db)
	if ch == nil {
		t.Fatal("NewCrudHandler returned nil")
	}
	r := router.New()
	RegisterCrudRoutes(r, ch, "/api/items")
	ch2 := RegisterCrudRoutesFunc(r, facadeEntity(t, "facade_others"), db, "/api/others")
	if ch2 == nil {
		t.Fatal("RegisterCrudRoutesFunc returned nil handler")
	}

	loaded, err := EagerLoad(context.Background(), db, ent, nil, nil)
	if err != nil || loaded == nil {
		t.Errorf("EagerLoad = %v, %v", loaded, err)
	}

	ctx := WithServerWrites(context.Background())
	ctx = WithReadHooks(ctx)
	if ctx == nil {
		t.Fatal("context wrappers returned nil")
	}
	if ve := NewValidationError(map[string][]string{"title": {"required"}}); ve == nil {
		t.Fatal("NewValidationError returned nil")
	}

	if err := RegisterEntityMCPTools(mcp.NewServer(), ch, r); err != nil {
		t.Errorf("RegisterEntityMCPTools: %v", err)
	}
}

func TestFacadeMigrateWrappersDelegate(t *testing.T) {
	db := facadeDB(t)
	ent := facadeEntity(t, "facade_migr")
	if err := MigrateEntityDialect(db, ent, migrate.DialectSQLite); err != nil {
		t.Fatalf("MigrateEntityDialect: %v", err)
	}
	if d := DetectDialect(db); d != migrate.DialectSQLite {
		t.Errorf("DetectDialect = %v", d)
	}

	reg := NewRegistry()
	reg.SetDB(db)
	ent2 := facadeEntity(t, "facade_migr2")
	if err := reg.Register(ent2); err != nil {
		t.Fatal(err)
	}
	snap := SnapshotFromRegistry(reg, migrate.DialectSQLite)
	if len(snap.Tables) == 0 {
		t.Error("SnapshotFromRegistry produced no tables")
	}
	up, down, next, err := GenerateMigration(reg, migrate.SchemaSnapshot{}, migrate.DialectSQLite)
	if err != nil || up == "" || len(next.Tables) == 0 {
		t.Errorf("GenerateMigration: up=%q down=%q err=%v", up, down, err)
	}

	plan := migrate.Plan{Registry: reg}
	if err := AutoMigratePlanContext(context.Background(), db, plan); err != nil {
		t.Errorf("AutoMigratePlanContext: %v", err)
	}
	planSnap := SnapshotFromPlan(plan, migrate.DialectSQLite)
	if len(planSnap.Tables) == 0 {
		t.Error("SnapshotFromPlan produced no tables")
	}
	if _, _, _, err := GeneratePlan(plan, migrate.SchemaSnapshot{}, migrate.DialectSQLite); err != nil {
		t.Errorf("GeneratePlan: %v", err)
	}

	rendered := RenderMigrationFile(1, "facade", "CREATE TABLE x (id TEXT);", "DROP TABLE x;")
	if rendered == "" {
		t.Error("RenderMigrationFile returned empty")
	}
	path := filepath.Join(t.TempDir(), "snap.json")
	if err := SaveSnapshot(path, snap); err != nil {
		t.Fatalf("SaveSnapshot: %v", err)
	}
	if loaded, err := LoadSnapshot(path); err != nil || len(loaded.Tables) != len(snap.Tables) {
		t.Errorf("LoadSnapshot round-trip: %v tables, err %v", len(loaded.Tables), err)
	}
}

func TestFacadeObservabilityWrappersDelegate(t *testing.T) {
	m := NewMetrics()
	if m == nil {
		t.Fatal("NewMetrics returned nil")
	}
	h := MetricsMiddleware(m)(Tracing()(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/facade", nil))
	if rec.Code != http.StatusNoContent {
		t.Errorf("wrapped handler code = %d", rec.Code)
	}
	mrec := httptest.NewRecorder()
	MetricsHandler(m).ServeHTTP(mrec, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	if mrec.Code != http.StatusOK {
		t.Errorf("MetricsHandler code = %d", mrec.Code)
	}
}

func TestFacadeTenantCronEventHookWrappersDelegate(t *testing.T) {
	cfg := DefaultTenantConfig()
	ent := facadeEntity(t, "facade_tenant")
	if WithMultiTenant(ent, cfg) == nil {
		t.Error("WithMultiTenant returned nil")
	}
	// TenantMiddleware ignores client headers by design: the tenant comes
	// only from the server-resolved authenticated context.
	var captured string
	tm := TenantMiddleware("")(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		captured = GetTenantID(r.Context())
	}))
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req = req.WithContext(handler.SetTenant(req.Context(), "acme"))
	tm.ServeHTTP(httptest.NewRecorder(), req)
	if captured != "acme" {
		t.Errorf("TenantMiddleware/GetTenantID = %q", captured)
	}
	data := map[string]any{}
	InjectTenantID(data, tenant.SetTenantID(context.Background(), "acme"))
	if data["tenant_id"] != "acme" {
		t.Errorf("InjectTenantID data = %v", data)
	}

	if NewScheduler() == nil {
		t.Error("NewScheduler returned nil")
	}
	if NewEventBus() == nil {
		t.Error("NewEventBus returned nil")
	}
	if NewHookRegistry() == nil {
		t.Error("NewHookRegistry returned nil")
	}
}
