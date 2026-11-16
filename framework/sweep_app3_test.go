package framework

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/core/a2a"
	"github.com/DonaldMurillo/gofastr/core/handler"
	"github.com/DonaldMurillo/gofastr/core/mcp"
	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/cron"
	fembed "github.com/DonaldMurillo/gofastr/framework/embed"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/framework/event"
)

func sweepApp3PostsApp(t *testing.T, opts ...AppOption) *App {
	t.Helper()
	app := NewApp(append([]AppOption{WithDB(openMemDB(t)), WithoutDefaultMiddleware()}, opts...)...)
	app.Entity("posts", entity.EntityConfig{
		Exposure: &entity.ExposureConfig{Public: true},
		Fields:   []schema.Field{{Name: "title", Type: schema.String}},
	})
	if err := AutoMigrate(app.DB, app.Registry); err != nil {
		t.Fatal(err)
	}
	return app
}

// An app built without WithOutbox must leave the handler's Outbox nil, so
// a create publishes on the bus instead of staging to a nil outbox.
func TestSweepApp3EntityWithoutOutboxEmitsOnBus(t *testing.T) {
	app := sweepApp3PostsApp(t)
	got := make(chan event.Event, 1)
	app.Events().On(event.EntityCreated, func(_ context.Context, e event.Event) error {
		select {
		case got <- e:
		default:
		}
		return nil
	})
	ch := app.MustCrudHandler("posts")
	if ch.Outbox != nil {
		t.Fatalf("handler Outbox = %#v without WithOutbox, want nil", ch.Outbox)
	}
	if _, err := ch.CreateOne(context.Background(), map[string]any{"title": "a"}); err != nil {
		t.Fatalf("CreateOne: %v", err)
	}
	select {
	case <-got:
	case <-time.After(5 * time.Second):
		t.Fatal("no EntityCreated event reached the bus")
	}
}

// CrudHandlerForEntity refuses an entity that shares the registered one's
// name and version but is not the instance the registry holds.
func TestSweepApp3CrudHandlerForEntityRefusesLookalike(t *testing.T) {
	app := sweepApp3PostsApp(t)
	lookalike := entity.Define("posts", entity.EntityConfig{
		Fields: []schema.Field{{Name: "title", Type: schema.String}},
	})
	if _, err := app.CrudHandlerForEntity(lookalike); err == nil {
		t.Fatal("CrudHandlerForEntity built a handler for an entity the registry does not hold")
	}
	ent, err := app.Registry.Get("posts")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := app.CrudHandlerForEntity(ent); err != nil {
		t.Fatalf("registered instance refused: %v", err)
	}
}

// An unversioned entity's handler addresses the app's API prefix, where
// its routes are mounted.
func TestSweepApp3CrudHandlerForEntityTakesAPIPrefix(t *testing.T) {
	app := sweepApp3PostsApp(t, WithAPIPrefix("/api"))
	ent, err := app.Registry.Get("posts")
	if err != nil {
		t.Fatal(err)
	}
	ch, err := app.CrudHandlerForEntity(ent)
	if err != nil {
		t.Fatal(err)
	}
	if ch.BasePath != "/api" {
		t.Fatalf("BasePath = %q, want %q", ch.BasePath, "/api")
	}
}

// A route registered without a method claims every method, so an entity
// whose CRUD routes land on its path is refused before anything mounts.
func TestSweepApp3MethodlessRouteBlocksEntity(t *testing.T) {
	for _, pattern := range []string{"/posts", "/posts/{slug}"} {
		app := atomicTestApp(t)
		app.Router().HandleFunc("", pattern, func(http.ResponseWriter, *http.Request) {})
		err := app.TryEntity("posts", EntityConfig{Fields: []schema.Field{{Name: "title", Type: schema.String}}})
		if err == nil || !strings.Contains(err.Error(), "already registered") {
			t.Fatalf("TryEntity over a method-less %s: err = %v, want the collision diagnostic", pattern, err)
		}
		if _, gerr := app.Registry.Get("posts"); gerr == nil {
			t.Errorf("entity refused over %s stayed in the registry", pattern)
		}
	}
}

// A trailing {x...} wildcard is a different shape from a one-segment
// wildcard, so a catch-all route beside the entity's {id} routes is no
// collision.
func TestSweepApp3CatchAllRouteBesideEntityRegisters(t *testing.T) {
	app := atomicTestApp(t)
	app.Router().HandleFunc("GET", "/posts/{rest...}", func(http.ResponseWriter, *http.Request) {})
	if err := app.TryEntity("posts", EntityConfig{Fields: []schema.Field{{Name: "title", Type: schema.String}}}); err != nil {
		t.Fatalf("TryEntity beside GET /posts/{rest...}: %v", err)
	}
}

// lookupHookRegistry runs on the request path and never creates a
// registry, so it cannot grow the map. HookRegistry labels what it makes
// with the entity's name.
func TestSweepApp3LookupHookRegistryNeverCreates(t *testing.T) {
	app := NewApp(WithoutDefaultMiddleware())
	if reg := app.lookupHookRegistry("ghosts"); reg != nil {
		t.Fatalf("lookupHookRegistry(ghosts) = %v, want nil", reg)
	}
	if _, ok := app.hooks["ghosts"]; ok {
		t.Fatal("lookupHookRegistry created a registry")
	}
	made := app.HookRegistry("posts")
	if made.Label() != "posts" {
		t.Errorf("HookRegistry(posts) label = %q, want posts", made.Label())
	}
	if got := app.lookupHookRegistry("posts"); got != made {
		t.Fatal("lookupHookRegistry did not return the registry HookRegistry made")
	}
}

func sweepApp3Tool(context.Context, map[string]any) (any, error) { return "ok", nil }

// An entity whose MCP tool name another registration already holds is
// refused before its registry entry, routes or tools are published.
func TestSweepApp3EntityToolNameTakenIsAtomic(t *testing.T) {
	app := atomicTestApp(t)
	if err := app.MCP.RegisterTool("posts_list", "taken", nil, sweepApp3Tool); err != nil {
		t.Fatal(err)
	}
	err := app.TryEntity("posts", EntityConfig{
		Fields:   []schema.Field{{Name: "title", Type: schema.String}},
		Exposure: &entity.ExposureConfig{MCP: true},
	})
	if err == nil || !strings.Contains(err.Error(), "already registered on the server") {
		t.Fatalf("TryEntity with a taken tool name: err = %v, want the tool-name refusal", err)
	}
	if _, gerr := app.Registry.Get("posts"); gerr == nil {
		t.Error("refused entity stayed in the registry")
	}
	if app.MCP.HasTool("posts_get") {
		t.Error("refused entity left its posts_get tool registered")
	}
}

// Two endpoints of one entity claiming one MCP tool name are refused
// before anything is published.
func TestSweepApp3DuplicateEndpointToolIsAtomic(t *testing.T) {
	app := atomicTestApp(t)
	h := func(http.ResponseWriter, *http.Request) {}
	err := app.TryEntity("posts", EntityConfig{
		Fields: []schema.Field{{Name: "title", Type: schema.String}},
		Endpoints: []entity.Endpoint{
			{Method: "POST", Path: "{id}/a", Name: "posts_dup", MCP: true, Handler: http.HandlerFunc(h), MCPHandler: sweepApp3Tool},
			{Method: "POST", Path: "{id}/b", Name: "posts_dup", MCP: true, Handler: http.HandlerFunc(h), MCPHandler: sweepApp3Tool},
		},
	})
	if err == nil || !strings.Contains(err.Error(), "already claimed by this entity") {
		t.Fatalf("TryEntity with a duplicate endpoint tool: err = %v, want the duplicate refusal", err)
	}
	if _, gerr := app.Registry.Get("posts"); gerr == nil {
		t.Error("refused entity stayed in the registry")
	}
	if app.MCP.HasTool("posts_dup") {
		t.Error("refused entity left its first endpoint tool registered")
	}
}

// An endpoint's generated MCP tool name comes from its prefix-relative
// path, so the API prefix moves the route but not the tool's name.
func TestSweepApp3EndpointToolNameIgnoresAPIPrefix(t *testing.T) {
	names := map[string][]mcp.Tool{}
	for _, prefix := range []string{"", "/api"} {
		opts := []AppOption{WithoutDefaultMiddleware()}
		if prefix != "" {
			opts = append(opts, WithAPIPrefix(prefix))
		}
		app := NewApp(opts...)
		app.Entity("posts", EntityConfig{
			Fields: []schema.Field{{Name: "title", Type: schema.String}},
			Endpoints: []entity.Endpoint{{Method: "POST", Path: "{id}/publish", MCP: true,
				Handler: http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}), MCPHandler: sweepApp3Tool}},
		})
		names[prefix] = app.MCP.ListTools()
	}
	if len(names[""]) != 1 || len(names["/api"]) != 1 {
		t.Fatalf("tools: %v / %v, want one endpoint tool each", names[""], names["/api"])
	}
	if a, b := names[""][0].Name, names["/api"][0].Name; a != b {
		t.Fatalf("endpoint tool name %q without a prefix, %q under /api", a, b)
	}
}

// An MCP App with nothing serving /mcp is unreachable, and boot says so;
// WithMCP silences the warning.
func TestSweepApp3MCPAppWithoutMountWarns(t *testing.T) {
	for _, mounted := range []bool{false, true} {
		var buf bytes.Buffer
		opts := []AppOption{
			WithoutDefaultMiddleware(),
			WithLogger(slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelWarn}))),
			WithMCPApp(mcp.AppConfig{Name: "studio", Description: "studio", InputSchema: map[string]any{"type": "object"},
				Handler: sweepApp3Tool, ResourceURI: "ui://demo/studio.html", HTML: "<h1>studio</h1>"}),
		}
		if mounted {
			opts = append(opts, WithMCP())
		}
		app := NewApp(opts...)
		if err := app.InitPlugins(); err != nil {
			t.Fatal(err)
		}
		if _, _, err := app.finalizeRoutes(); err != nil {
			t.Fatal(err)
		}
		warned := strings.Contains(buf.String(), "WithMCPApp registered but /mcp is not mounted")
		if warned == mounted {
			t.Errorf("WithMCP=%v: warned=%v; log=%q", mounted, warned, buf.String())
		}
	}
}

// A worker's banner names only the health surface it serves.
func TestSweepApp3WorkerBannerAdvertisesOnlyHealth(t *testing.T) {
	app := sweepApp3PostsApp(t, WithRole(RoleWorker))
	var buf bytes.Buffer
	app.startupOutput = &buf
	app.printStartupBanner("127.0.0.1:8080", "tcp", "demo", true, true, "")
	out := buf.String()
	if !strings.Contains(out, "worker ready") || !strings.Contains(out, "/healthz") {
		t.Fatalf("worker banner lacks the worker line or health URLs: %q", out)
	}
	for _, leak := range []string{"/posts", "OpenAPI", "/mcp", "Listening"} {
		if strings.Contains(out, leak) {
			t.Errorf("worker banner advertises %q: %q", leak, out)
		}
	}
}

// An embed grant never reaches /.debug/*, even when a WithDebugAuthorize
// predicate would admit the caller.
func TestSweepApp3DebugRefusesEmbedGrantUnderCustomAuthorize(t *testing.T) {
	app := NewApp(WithDebugAuthorize(func(context.Context) bool { return true }))
	app.registerDebugEndpoints()
	user := handler.SetUser(context.Background(), "u-1")
	if got := debugStatus(app, "/.debug/stats", user); got != http.StatusOK {
		t.Fatalf("admitted caller = %d, want 200", got)
	}
	if got := debugStatus(app, "/.debug/stats", fembed.WithGrant(user, fembed.Grant{})); got != http.StatusForbidden {
		t.Fatalf("caller through an embed grant = %d, want 403", got)
	}
}

// The banner names the /mcp endpoint and its tool count when it is mounted.
func TestSweepApp3BannerNamesMCPEndpoint(t *testing.T) {
	app := NewApp(WithoutDefaultMiddleware(), WithMCP())
	if err := app.MCP.RegisterTool("ping", "ping", nil, sweepApp3Tool); err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	app.startupOutput = &buf
	app.printStartupBanner("127.0.0.1:8080", "tcp", "demo", false, false, "")
	if out := buf.String(); !strings.Contains(out, "http://127.0.0.1:8080/mcp  (1 tools") {
		t.Fatalf("banner does not name /mcp with its tool count: %q", out)
	}
}

// Under RoleServe AddCron and AddQueue register neither a start hook nor a
// drainer; under RoleWorker each registers both.
func TestSweepApp3WorkersAreWorkerScoped(t *testing.T) {
	t.Setenv("GOFASTR_ROLE", "")
	for _, tc := range []struct {
		role Role
		want int
	}{{RoleServe, 0}, {RoleWorker, 2}} {
		app := NewApp(WithoutDefaultMiddleware(), WithRole(tc.role))
		hooks := len(app.startHooks)
		app.AddCron(cron.NewScheduler())
		app.AddQueue(&blockedCloseQueue{release: make(chan struct{})})
		if got := len(app.startHooks) - hooks; got != tc.want {
			t.Errorf("role %v: %d start hooks added, want %d", tc.role, got, tc.want)
		}
	}
}

// A cron job that ignores its cancelled context is abandoned at the
// Shutdown deadline instead of hanging the teardown.
func TestSweepApp3CronDrainHonorsShutdownDeadline(t *testing.T) {
	t.Setenv("GOFASTR_ROLE", "")
	app := NewApp(WithoutDefaultMiddleware())
	block := make(chan struct{})
	started := make(chan struct{})
	s := cron.NewScheduler()
	if err := s.Register(cron.CronJob{Name: "stuck", Spec: "* * * * *", Run: func(context.Context) error {
		close(started)
		<-block
		return nil
	}}); err != nil {
		t.Fatal(err)
	}
	app.AddCron(s)
	if err := app.runStartHooks(); err != nil {
		t.Fatal(err)
	}
	s.RunOnce(context.Background(), time.Now().Truncate(time.Minute))
	<-started
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- app.Shutdown(ctx) }()
	select {
	case err := <-done:
		if err == nil {
			t.Error("Shutdown abandoned a running job without reporting it")
		}
	case <-time.After(10 * time.Second):
		close(block)
		t.Fatal("Shutdown hung on a cron job that ignores its context")
	}
	close(block)
}

// A scheduler added during a module's Init skips its jobs while the
// module is disabled.
func TestSweepApp3ModuleCronSkipsWhileDisabled(t *testing.T) {
	t.Setenv("GOFASTR_ROLE", "")
	app := NewApp(WithoutDefaultMiddleware())
	var ran atomic.Int32
	s := cron.NewScheduler()
	if err := s.Register(cron.CronJob{Name: "tick", Spec: "* * * * *", Run: func(context.Context) error { ran.Add(1); return nil }}); err != nil {
		t.Fatal(err)
	}
	app.RegisterModule(&modStub{name: "m1", init: func(app *App) error { app.AddCron(s); return nil }})
	if err := app.InitPlugins(); err != nil {
		t.Fatal(err)
	}
	now := time.Now().Truncate(time.Minute)
	if err := app.Modules().Disable(context.Background(), "m1"); err != nil {
		t.Fatal(err)
	}
	s.RunOnce(context.Background(), now)
	s.Stop()
	if n := ran.Load(); n != 0 {
		t.Fatalf("job ran %d times while its module was disabled", n)
	}
	if err := app.Modules().Enable(context.Background(), "m1"); err != nil {
		t.Fatal(err)
	}
	s.RunOnce(context.Background(), now)
	s.Stop()
	if n := ran.Load(); n != 1 {
		t.Fatalf("job ran %d times once its module was enabled, want 1", n)
	}
}

// /.debug/* is mounted only with AppConfig.DebugEndpoints.
func TestSweepApp3DebugEndpointsOnlyWhenEnabled(t *testing.T) {
	admin := handler.SetUser(context.Background(), debugRoleUser{roles: []string{"admin"}})
	for _, enabled := range []bool{false, true} {
		app := NewApp(WithoutDefaultMiddleware())
		app.Config.DebugEndpoints = enabled
		if err := app.InitPlugins(); err != nil {
			t.Fatal(err)
		}
		if _, _, err := app.finalizeRoutes(); err != nil {
			t.Fatal(err)
		}
		rec := httptest.NewRecorder()
		app.router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/.debug/stats", nil).WithContext(admin))
		want := http.StatusNotFound
		if enabled {
			want = http.StatusOK
		}
		if rec.Code != want {
			t.Errorf("DebugEndpoints=%v: /.debug/stats = %d, want %d", enabled, rec.Code, want)
		}
	}
}

// Shutdown ends detached A2A runs before the stop hooks run, so a run's
// final write never meets a closed store.
func TestSweepApp3A2ARunsEndBeforeStopHooks(t *testing.T) {
	started, returned := make(chan struct{}), make(chan struct{})
	hold := a2a.Skill{
		ID: "hold", Name: "Hold", Description: "Runs until cancelled.",
		Handler: func(ctx context.Context, _ a2a.TaskContext) error {
			close(started)
			<-ctx.Done()
			time.Sleep(50 * time.Millisecond)
			close(returned)
			return ctx.Err()
		},
	}
	e := newA2ATestEnv(t, A2AConfig{Skills: []a2a.Skill{hold}, DisableEntitySkills: true})
	var endedFirst atomic.Bool
	e.app.OnStop(func() error {
		select {
		case <-returned:
			endedFirst.Store(true)
		default:
		}
		return nil
	})
	status, body := e.a2aRequest(t, "tok-alice", `{"jsonrpc":"2.0","id":1,"method":"SendMessage","params":{`+
		`"message":{"messageId":"m1","role":"ROLE_USER","parts":[{"text":"go"}],"metadata":{"skill":"hold"}},`+
		`"configuration":{"returnImmediately":true}}}`)
	if status != http.StatusOK {
		t.Fatalf("SendMessage: %d %s", status, body)
	}
	<-started
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = e.app.Shutdown(ctx)
	if !endedFirst.Load() {
		t.Fatal("an OnStop hook ran while an A2A run was still running")
	}
}

// Seeds run under WithoutAutoMigrate: they are data, not schema.
func TestSweepApp3SeedsRunWithoutAutoMigrate(t *testing.T) {
	db := openTestDB(t, "sqlite3")
	app := NewApp(WithDB(db), WithoutAutoMigrate(), WithoutDefaultMiddleware())
	var seeded atomic.Bool
	app.Entity("gadgets", entity.EntityConfig{
		Table:  "gadgets",
		Fields: []schema.Field{{Name: "name", Type: schema.String}},
		Seed:   func(context.Context, *sql.DB) error { seeded.Store(true); return nil },
	}.WithTimestamps(false))
	if err := AutoMigrate(db, app.Registry); err != nil {
		t.Fatal(err)
	}
	if err := startAndStop(t, app); err != nil {
		t.Fatalf("start: %v", err)
	}
	if !seeded.Load() {
		t.Fatal("WithoutAutoMigrate skipped the entity seed")
	}
}

// Serve closes the caller's listener when it refuses to start: after a
// Shutdown that came before any run, and on an App already started.
func TestSweepApp3ServeClosesListenerWhenRefused(t *testing.T) {
	stopped := quietServeApp()
	if err := stopped.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
	ln := newPipeListener()
	if err := stopped.Serve(ln); err != nil {
		t.Fatalf("Serve after Shutdown = %v, want nil", err)
	}
	if !ln.isClosed() {
		t.Error("Serve after Shutdown left the listener open")
	}

	app := quietServeApp()
	ready := make(chan struct{})
	app.OnReady(func(string) { close(ready) })
	done := make(chan error, 1)
	go func() { done <- app.Serve(newPipeListener()) }()
	<-ready
	second := newPipeListener()
	if err := app.Serve(second); !errors.Is(err, ErrAlreadyStarted) {
		t.Errorf("second Serve = %v, want ErrAlreadyStarted", err)
	}
	if !second.isClosed() {
		t.Error("a refused second Serve left its listener open")
	}
	if err := app.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
	<-done
}
