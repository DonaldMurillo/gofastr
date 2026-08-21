package framework

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"os"
	"path/filepath"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/access"
	"github.com/DonaldMurillo/gofastr/framework/event"
	"github.com/DonaldMurillo/gofastr/framework/hook"
	"github.com/DonaldMurillo/gofastr/framework/semcov"
)

// TestHarnessRecordsRoutesItReaches is the end-to-end claim the testing
// rules rest on: a request driven through the harness is recorded against
// the route's *pattern*, not the concrete path it was made with.
func TestHarnessRecordsRoutesItReaches(t *testing.T) {
	dir := t.TempDir()
	semcov.Reset()
	t.Cleanup(semcov.Reset)
	t.Setenv("GOFASTR_SEMANTIC_COVERAGE_DIR", dir)

	app := NewApp(WithConfig(AppConfig{Name: "semcov"}))
	app.Router().Get("/widgets/{id}", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	app.Router().Get("/never-called", http.NotFoundHandler())

	ta := TestHarness(t, app)
	ta.Get("/widgets/42").AssertStatus(t, http.StatusOK)

	if err := semcov.Flush(); err != nil {
		t.Fatal(err)
	}
	m, err := semcov.Read(dir)
	if err != nil {
		t.Fatalf("read manifest: %v", err)
	}
	// Recorded by pattern: one test hitting /widgets/42 credits the
	// /widgets/{id} route, which is what makes the coverage check
	// comparable against the statically discovered route table.
	if !m.CoveredRoute("GET", "/widgets/{id}") {
		t.Errorf("route not recorded by pattern: %v", m.Routes)
	}
	if m.CoveredRoute("GET", "/widgets/42") {
		t.Error("recorded the request path instead of the pattern")
	}
	// The whole point: a route nothing exercised stays absent, so the
	// testing analyzer has something to report.
	if m.CoveredRoute("GET", "/never-called") {
		t.Error("a route no test reached was recorded as covered")
	}
}

func TestRecordingIsOffWhenOptedOut(t *testing.T) {
	dir := t.TempDir()
	semcov.Reset()
	t.Cleanup(semcov.Reset)
	t.Setenv("GOFASTR_SEMANTIC_COVERAGE_DIR", dir)
	t.Setenv(semanticCoverageOptOut, "1")

	app := NewApp(WithConfig(AppConfig{Name: "semcov-off"}))
	app.Router().Get("/x", http.NotFoundHandler())
	ta := TestHarness(t, app)
	ta.Get("/x")

	if semcov.Enabled() {
		t.Fatal("opt-out did not take effect")
	}
	if _, err := semcov.Read(dir); err == nil {
		t.Error("a manifest was written despite the opt-out")
	}
}

// TestPermissionEvaluationsAreRecorded closes the hole that made
// GOFASTR1102 unusable: the rule reads permission coverage from the
// manifest, so if nothing writes it, every declared permission reports as
// unexercised forever, a rule that is always wrong is worse than no rule.
func TestPermissionEvaluationsAreRecorded(t *testing.T) {
	dir := t.TempDir()
	semcov.Reset()
	access.SetObserver(nil)
	t.Cleanup(func() {
		semcov.Reset()
		access.SetObserver(nil)
	})
	t.Setenv("GOFASTR_SEMANTIC_COVERAGE_DIR", dir)

	policy := access.NewRolePolicy()
	policy.Grant("editor", "posts:write")

	app := NewApp(WithConfig(AppConfig{Name: "perm-cov"}))
	app.Use(access.Middleware(policy, func(ctx context.Context) []string {
		return []string{"editor"}
	}))
	app.Router().Get("/granted", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		access.Can(r.Context(), "posts:write")
		w.WriteHeader(http.StatusOK)
	}))
	app.Router().Get("/denied", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		access.Can(r.Context(), "posts:delete")
		w.WriteHeader(http.StatusOK)
	}))

	ta := TestHarness(t, app)
	ta.Get("/granted").AssertStatus(t, http.StatusOK)
	ta.Get("/denied").AssertStatus(t, http.StatusOK)

	if err := semcov.Flush(); err != nil {
		t.Fatal(err)
	}
	m, err := semcov.Read(dir)
	if err != nil {
		t.Fatal(err)
	}
	if !m.CoveredPermission("posts:write") {
		t.Errorf("a granted permission was not recorded: %v", m.Permissions)
	}
	// A denial proves the boundary as well as a grant does, recording
	// only the grants would report a correctly-tested rejection path as
	// untested.
	if !m.CoveredPermission("posts:delete") {
		t.Errorf("a denied permission was not recorded: %v", m.Permissions)
	}
	if m.CoveredPermission("posts:publish") {
		t.Error("a permission nothing checked was recorded")
	}
	// The role the caller held during those checks is recorded too, which
	// is what makes GOFASTR1109 answerable.
	if !m.CoveredRole("editor") {
		t.Errorf("the caller's role was not recorded: %v", m.Roles)
	}
	if m.CoveredRole("admin") {
		t.Error("a role nobody authenticated as was recorded")
	}
}

// TestHookFiringsAreRecorded proves the other half of GOFASTR1107: a
// registered hook that actually runs is credited, and, the part that
// makes the rule meaningful, a lifecycle point with nothing registered
// is NOT, so an app with no hooks does not report full hook coverage.
func TestHookFiringsAreRecorded(t *testing.T) {
	dir := t.TempDir()
	semcov.Reset()
	hook.SetObserver(nil)
	t.Cleanup(func() {
		semcov.Reset()
		hook.SetObserver(nil)
	})
	t.Setenv("GOFASTR_SEMANTIC_COVERAGE_DIR", dir)
	semcov.Enable(dir)
	hook.SetObserver(func(f hook.Firing) {
		semcov.RecordHook(f.Entity, f.Type.String())
	})

	reg := hook.NewHookRegistry()
	reg.SetLabel("posts")
	reg.RegisterHook(hook.BeforeCreate, func(ctx context.Context, data any) error { return nil })

	if err := reg.ExecuteHooks(context.Background(), hook.BeforeCreate, nil); err != nil {
		t.Fatal(err)
	}
	// Nothing is registered for AfterDelete. Executing it must not count:
	// ExecuteHooks runs on every CRUD operation regardless, so crediting
	// the call would give every entity full hook coverage on first use.
	if err := reg.ExecuteHooks(context.Background(), hook.AfterDelete, nil); err != nil {
		t.Fatal(err)
	}

	if err := semcov.Flush(); err != nil {
		t.Fatal(err)
	}
	m, err := semcov.Read(dir)
	if err != nil {
		t.Fatal(err)
	}
	if !m.CoveredHook("posts", "beforecreate") {
		t.Errorf("a hook that ran was not recorded: %v", m.Hooks)
	}
	if m.CoveredHook("posts", "afterdelete") {
		t.Error("a lifecycle point with no registered hook was recorded as covered")
	}
}

func TestEntityOpForMapsCrudShape(t *testing.T) {
	cases := []struct{ method, pattern, want string }{
		{"GET", "/posts", "list"},
		{"GET", "/posts/{id}", "get"},
		{"POST", "/posts", "create"},
		{"PUT", "/posts/{id}", "update"},
		{"PATCH", "/posts/{id}", "update"},
		{"DELETE", "/posts/{id}", "delete"},
	}
	for _, c := range cases {
		if got := entityOpFor(c.method, c.pattern); got != c.want {
			t.Errorf("entityOpFor(%q, %q) = %q, want %q", c.method, c.pattern, got, c.want)
		}
	}
}

func TestFlushSemanticCoverageIsCallableFromTestMain(t *testing.T) {
	// The TestMain shape: enable, drive, flush once at the end.
	dir := t.TempDir()
	semcov.Reset()
	t.Cleanup(semcov.Reset)
	t.Setenv("GOFASTR_SEMANTIC_COVERAGE_DIR", dir)

	app := NewApp(WithConfig(AppConfig{Name: "flush-api"}))
	app.Router().Get("/x", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	ta := TestHarness(t, app)
	ta.Get("/x")

	if err := FlushSemanticCoverage(); err != nil {
		t.Fatalf("FlushSemanticCoverage: %v", err)
	}
	m, err := semcov.Read(dir)
	if err != nil {
		t.Fatal(err)
	}
	if !m.CoveredRoute("GET", "/x") {
		t.Error("the exported flush did not write what was recorded")
	}
}

func TestRecordSemanticCoverageIgnoresANilApp(t *testing.T) {
	semcov.Reset()
	t.Cleanup(semcov.Reset)
	RecordSemanticCoverage(t, nil)
	if semcov.Enabled() {
		t.Error("a nil app switched recording on")
	}
}

func TestEntityForPatternWithoutARegistry(t *testing.T) {
	// A router-only app has no entities; the lookup must return empty
	// rather than dereference a nil registry.
	app := &App{}
	if got := app.entityForPattern("/posts/{id}"); got != "" {
		t.Errorf("entityForPattern = %q, want empty", got)
	}
}

func TestEntityForPatternIgnoresParameterSegments(t *testing.T) {
	app := NewApp(WithConfig(AppConfig{Name: "entity-lookup"}))
	// No entity registered, so every pattern resolves to nothing, the
	// walk must still terminate on a pattern that is all parameters.
	for _, pattern := range []string{"/", "", "/{id}", "/{a}/{b}"} {
		if got := app.entityForPattern(pattern); got != "" {
			t.Errorf("entityForPattern(%q) = %q, want empty", pattern, got)
		}
	}
}

func TestEntityForPatternResolvesThroughTheTableName(t *testing.T) {
	// The route mounts at the entity's TABLE, which need not equal the
	// entity name, /api/v1/blog_posts/{id} has to find "BlogPost".
	app := NewApp(WithConfig(AppConfig{Name: "entity-table"}))
	app.Entity("BlogPost", EntityConfig{
		Table:  "blog_posts",
		Fields: []schema.Field{{Name: "title", Type: schema.String}},
	})
	if got := app.entityForPattern("/api/v1/blog_posts/{id}"); got != "BlogPost" {
		t.Errorf("entityForPattern = %q, want BlogPost", got)
	}
	if got := app.entityForPattern("/api/v1/unrelated"); got != "" {
		t.Errorf("entityForPattern matched an unrelated path: %q", got)
	}
	// The name is also matched, for a surface mounted under the entity
	// name rather than its table, a renamed table must not make the
	// entity invisible to coverage.
	if got := app.entityForPattern("/admin/BlogPost"); got != "BlogPost" {
		t.Errorf("entityForPattern by name = %q, want BlogPost", got)
	}
}

func TestEntityOpForUnknownMethod(t *testing.T) {
	if got := entityOpFor("OPTIONS", "/posts"); got != "options" {
		t.Errorf("entityOpFor(OPTIONS) = %q", got)
	}
}

func TestFlushFailureIsLoggedNotFatal(t *testing.T) {
	// A coverage manifest that cannot be written must not fail the test
	// that was merely being recorded. The recorder is observability; it
	// has no business turning a passing suite red.
	blocked := filepath.Join(t.TempDir(), "not-a-directory")
	if err := os.WriteFile(blocked, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	semcov.Reset()
	t.Cleanup(semcov.Reset)
	t.Setenv("GOFASTR_SEMANTIC_COVERAGE_DIR", blocked)

	app := NewApp(WithConfig(AppConfig{Name: "flush-fail"}))
	app.Router().Get("/x", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	ta := TestHarness(t, app)
	ta.Get("/x")

	// The flush runs in RecordSemanticCoverage's t.Cleanup and logs. The
	// assertion is that this test still passes.
	if err := semcov.Flush(); err == nil {
		t.Fatal("expected a write failure against a non-directory")
	}
}

// TestServingProcessRecordsWhenEnvIsSet covers the shape that had no
// coverage at all: an integration test that builds the binary and drives
// it over real HTTP never touches TestHarness, so every route it
// exercised was invisible. Setting GOFASTR_SEMANTIC_COVERAGE=1 on the
// server process is what makes those requests count.
func TestServingProcessRecordsWhenEnvIsSet(t *testing.T) {
	dir := t.TempDir()
	semcov.Reset()
	access.SetObserver(nil)
	hook.SetObserver(nil)
	t.Cleanup(func() {
		semcov.Reset()
		access.SetObserver(nil)
		hook.SetObserver(nil)
	})
	t.Setenv("GOFASTR_SEMANTIC_COVERAGE_DIR", dir)
	t.Setenv(semanticCoverageEnv, "1")

	app := NewApp(WithConfig(AppConfig{Name: "serving-cov"}))
	// A declared entity means the served route also credits an entity
	// operation, the way a real CRUD mount does.
	app.Entity("orders", EntityConfig{
		Fields: []schema.Field{{Name: "total", Type: schema.String}},
	})
	app.Router().Get("/orders/{id}", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	app.enableSemanticCoverageFromEnv()

	// Drive the router the way a real request would, without TestHarness.
	rec := httptest.NewRecorder()
	app.Router().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/orders/42", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("request failed: %d", rec.Code)
	}

	// Every dimension the harness records must also record here, or a
	// server-driven suite would get route coverage and silently nothing
	// else.
	policy := access.NewRolePolicy()
	policy.Grant("editor", "orders:read")
	access.Can(access.WithRoles(access.WithPolicy(context.Background(), policy),
		[]string{"editor"}), "orders:read")

	reg := hook.NewHookRegistry()
	reg.SetLabel("orders")
	reg.RegisterHook(hook.BeforeCreate, func(context.Context, any) error { return nil })
	if err := reg.ExecuteHooks(context.Background(), hook.BeforeCreate, nil); err != nil {
		t.Fatal(err)
	}

	bus := event.NewEventBus()
	if err := bus.Emit(context.Background(), event.Event{Type: "order.placed"}); err != nil {
		t.Fatal(err)
	}
	if err := semcov.Flush(); err != nil {
		t.Fatal(err)
	}

	// Flushed from the serve hook, not at shutdown, a harness usually
	// SIGKILLs the server, so nothing would ever run a shutdown path.
	m, err := semcov.Read(dir)
	if err != nil {
		t.Fatalf("manifest was not written during serving: %v", err)
	}
	if !m.CoveredRoute("GET", "/orders/{id}") {
		t.Errorf("serving process did not record the route: %v", m.Routes)
	}
	if !m.CoveredPermission("orders:read") || !m.CoveredRole("editor") {
		t.Errorf("permissions/roles not recorded: %v %v", m.Permissions, m.Roles)
	}
	if !m.CoveredHook("orders", "beforecreate") {
		t.Errorf("hook firing not recorded: %v", m.Hooks)
	}
	if !m.CoveredEvent("order.placed") {
		t.Errorf("event not recorded: %v", m.Events)
	}
	if !m.CoveredEntity("orders") {
		t.Errorf("entity operation not recorded: %v", m.Entities)
	}
}

func TestServingProcessSurvivesAnUnwritableManifest(t *testing.T) {
	// Coverage recording is observability bolted onto a serving process.
	// If the manifest cannot be written, the server must keep serving.
	// The request that triggered the flush is a user's request.
	blocked := filepath.Join(t.TempDir(), "not-a-directory")
	if err := os.WriteFile(blocked, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	semcov.Reset()
	t.Cleanup(semcov.Reset)
	t.Setenv("GOFASTR_SEMANTIC_COVERAGE_DIR", blocked)
	t.Setenv(semanticCoverageEnv, "1")

	app := NewApp(WithConfig(AppConfig{Name: "serving-unwritable"}))
	app.Router().Get("/x", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTeapot)
	}))
	app.enableSemanticCoverageFromEnv()

	rec := httptest.NewRecorder()
	app.Router().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/x", nil))
	if rec.Code != http.StatusTeapot {
		t.Fatalf("a failed coverage flush changed the response: %d", rec.Code)
	}
}

func TestServingProcessIsSilentWithoutTheEnvVar(t *testing.T) {
	dir := t.TempDir()
	semcov.Reset()
	t.Cleanup(semcov.Reset)
	t.Setenv("GOFASTR_SEMANTIC_COVERAGE_DIR", dir)
	t.Setenv(semanticCoverageEnv, "")

	app := NewApp(WithConfig(AppConfig{Name: "serving-nocov"}))
	app.Router().Get("/x", http.NotFoundHandler())
	app.enableSemanticCoverageFromEnv()

	rec := httptest.NewRecorder()
	app.Router().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/x", nil))

	if semcov.Enabled() {
		t.Fatal("a production process switched recording on")
	}
	if _, err := semcov.Read(dir); err == nil {
		t.Error("a manifest was written without the env var")
	}
}
