// full is a realistic shape with every supported framework surface
// wired on at once: three related entities with relations, audit log,
// cron, MCP, the UI host with one screen, file storage, search backend,
// access control, multi-tenancy, custom endpoints, plugins, and the
// OpenAPI + Swagger UI surface. Represents the upper bound on what a
// single GoFastr binary carries when nothing is opted out.
//
// Listens on $PORT (default :18082).
package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"log"
	"net/http"
	"os"
	"path/filepath"

	"github.com/gofastr/gofastr/battery/search"
	"github.com/gofastr/gofastr/battery/storage"
	"github.com/gofastr/gofastr/core-ui/app"
	"github.com/gofastr/gofastr/core-ui/component"
	"github.com/gofastr/gofastr/core/render"
	"github.com/gofastr/gofastr/core/router"
	"github.com/gofastr/gofastr/core/schema"
	"github.com/gofastr/gofastr/framework"
	"github.com/gofastr/gofastr/framework/uihost"
	_ "github.com/mattn/go-sqlite3"
)

func main() {
	// ----- DB ------------------------------------------------------------
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		log.Fatal(err)
	}
	db.SetMaxOpenConns(1)

	// ----- File storage -------------------------------------------------
	uploadDir, _ := os.MkdirTemp("", "gofastr-full-uploads-*")
	fileStore := storage.NewLocalStorage(uploadDir)

	// ----- App ----------------------------------------------------------
	fwApp := framework.NewApp(
		framework.WithDB(db),
		framework.WithFileStorage(fileStore),
		framework.WithConfig(framework.AppConfig{Name: "full-bench"}),
	)

	// ----- Entities -----------------------------------------------------
	authors := framework.Define("authors", framework.EntityConfig{
		Table: "authors",
		Fields: []schema.Field{
			{Name: "name", Type: schema.String, Required: true},
			{Name: "email", Type: schema.String, Unique: true},
			{Name: "avatar", Type: schema.Image}, // exercises the upload pipeline
		},
		MCP: true,
	})
	posts := framework.Define("posts", framework.EntityConfig{
		Table: "posts",
		Fields: []schema.Field{
			{Name: "title", Type: schema.String, Required: true},
			{Name: "body", Type: schema.Text},
			{Name: "status", Type: schema.String, Default: "draft"},
			{Name: "author_id", Type: schema.String},
			{Name: "views", Type: schema.Int, Default: 0},
		},
		Relations: []framework.Relation{
			framework.BelongsTo("author", "authors", "author_id"),
			framework.HasMany("comments", "comments", "post_id"),
		},
		MCP: true,
	})
	comments := framework.Define("comments", framework.EntityConfig{
		Table: "comments",
		Fields: []schema.Field{
			{Name: "body", Type: schema.Text, Required: true},
			{Name: "post_id", Type: schema.String},
			{Name: "tenant_id", Type: schema.String},
		},
		MultiTenant: true, // exercises the tenant-scoping path
		MCP:         true,
	})
	fwApp.Registry.Register(authors)
	fwApp.Registry.Register(posts)
	fwApp.Registry.Register(comments)

	if err := framework.AutoMigrate(db, fwApp.Registry); err != nil {
		log.Fatal(err)
	}

	// ----- CRUD routes --------------------------------------------------
	framework.RegisterCrudRoutes(fwApp.Router, framework.NewCrudHandler(authors, db), "/authors")
	framework.RegisterCrudRoutes(fwApp.Router, framework.NewCrudHandler(posts, db), "/posts")
	framework.RegisterCrudRoutes(fwApp.Router, framework.NewCrudHandler(comments, db), "/comments")

	// ----- Audit log ----------------------------------------------------
	fwApp.WithAuditLog(framework.AuditConfig{
		Table: "audit_log",
		Actor: func(_ context.Context) string { return "system" },
	})

	// ----- Cron ---------------------------------------------------------
	sched := framework.NewScheduler()
	if err := sched.Register(framework.CronJob{
		Name: "noop_minute",
		Spec: "* * * * *",
		Run:  func(_ context.Context) error { return nil },
	}); err != nil {
		log.Fatal(err)
	}
	fwApp.AddCron(sched)

	// ----- Access control ----------------------------------------------
	policy := framework.NewRolePolicy()
	policy.Grant("admin", "posts:read", "posts:write", "posts:delete")
	policy.Grant("editor", "posts:read", "posts:write")
	policy.Grant("reader", "posts:read")
	fwApp.Use(router.Middleware(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := framework.WithPolicy(r.Context(), policy)
			ctx = framework.WithRoles(ctx, []string{"reader"})
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}))
	fwApp.Use(router.Middleware(framework.TenantMiddleware("X-Tenant-ID")))

	// ----- Search backend wired to /search ----------------------------
	idx := search.NewMemory()
	fwApp.Router.GetFunc("/search", func(w http.ResponseWriter, r *http.Request) {
		results, _ := idx.Search(r.Context(), search.Query{
			Text:  r.URL.Query().Get("q"),
			Type:  "posts",
			Limit: 10,
		})
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"results": results})
	})

	// ----- Custom endpoint ---------------------------------------------
	fwApp.Router.GetFunc("/posts/{id}/publish",
		framework.RequirePermission("posts:write")(
			http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte(`{"ok":true}`))
			})).ServeHTTP)

	// ----- A no-op plugin ----------------------------------------------
	fwApp.RegisterPlugin(&noopPlugin{})
	_ = fwApp.InitPlugins()

	// ----- UI host with one screen -------------------------------------
	site := app.NewApp("full-bench")
	layout := app.NewLayout("main")
	site.SetDefaultLayout(layout)
	site.Register("/", &HomeScreen{}, nil)
	host := uihost.New(site)
	fwApp.Mount(host)

	// ----- Lifecycle hooks ---------------------------------------------
	fwApp.OnStart(func(_ context.Context) error {
		log.Printf("full starting; uploads at %s", filepath.Clean(uploadDir))
		return nil
	})
	fwApp.OnStop(func() error {
		_ = os.RemoveAll(uploadDir)
		return nil
	})

	// ----- Health -------------------------------------------------------
	fwApp.Router.GetFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})

	addr := ":" + getEnv("PORT", "18082")
	log.Printf("full listening on %s (OpenAPI at /openapi.json, Swagger at /docs/)", addr)
	if err := fwApp.Start(addr); err != nil {
		log.Fatal(err)
	}
}

// HomeScreen is the minimal UI surface: one page rendered through the
// uihost + runtime stack. The point is to exercise the SSR/hydration
// machinery, not to look like anything.
type HomeScreen struct{}

func (s *HomeScreen) ScreenTitle() string        { return "Home" }
func (s *HomeScreen) ScreenDescription() string  { return "full bench app" }
func (s *HomeScreen) ScreenType() app.ScreenType { return app.ScreenPage }
func (s *HomeScreen) Render() render.HTML {
	return render.Tag("main", nil,
		render.Tag("h1", nil, render.Text("full")),
		render.Tag("p", nil, render.Text("Everything wired.")),
	)
}

var _ component.Component = (*HomeScreen)(nil)

// noopPlugin shows the plugin surface compiles without forcing the binary
// to carry any extra functionality. Init runs once at InitPlugins time.
type noopPlugin struct{}

func (noopPlugin) Name() string                     { return "noop" }
func (noopPlugin) Init(_ *framework.App) error      { return nil }

func getEnv(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
