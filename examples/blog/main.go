package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"log"
	"net/http"
	"os"
	"strings"

	"github.com/DonaldMurillo/gofastr/battery/search"
	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework"
	_ "github.com/mattn/go-sqlite3"
)

func main() {
	db, err := sql.Open("sqlite3", "./blog.db")
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()

	app := framework.NewApp(
		framework.WithDB(db),
		framework.WithConfig(framework.AppConfig{Name: "blog"}),
	)

	// --- Entities ---
	// Declared in Go so `go run ./examples/blog` works from any directory
	// (no external entity files to locate). The same schema is mirrored in
	// gofastr.yml for the no-code / codegen path.
	registerEntities(app)

	// --- Custom endpoints ---

	// GET /posts/published — filter published posts
	app.Router().Get("/posts/published", postsByStatus(app, "published"))
	searchIndex := search.NewMemory()
	app.Router().Get("/posts/search", searchPosts(searchIndex))

	// --- Seed ---
	// AutoMigrate before seeding so the tables exist (app.Start migrates
	// too, but that runs after this and is idempotent).
	if err := framework.AutoMigrate(db, app.Registry); err != nil {
		log.Fatal(err)
	}
	seed(db)
	seedSearch(searchIndex)

	// --- Start (auto-migrates, auto-routes CRUD, auto-serves OpenAPI + Swagger) ---

	if err := app.Start(listenAddr()); err != nil {
		log.Fatal(err)
	}
}

// registerEntities declares the blog's three entities in Go.
func registerEntities(app *framework.App) {
	app.Entity("users", framework.EntityConfig{
		// email is PII: accounts are staff-managed records, so every operation
		// is RBAC-gated (fail-closed 403 for anonymous) — hard rule #6. This
		// mirrors the access: block in the blueprint twin (gofastr.yml).
		Access: framework.AccessControl{
			Read:   "users:read",
			Create: "users:write",
			Update: "users:write",
			Delete: "users:admin",
		},
		Fields: []schema.Field{
			{Name: "name", Type: schema.String, Required: true},
			{Name: "email", Type: schema.String, Required: true, Unique: true},
			{Name: "role", Type: schema.Enum, Values: []string{"admin", "author", "reader"}, Default: "reader"},
		},
		Relations: []framework.Relation{
			framework.HasMany("posts", "posts", "author_id"),
			framework.HasMany("comments", "comments", "author_id"),
		},
	})
	app.Entity("posts", framework.EntityConfig{
		// public demo content — see "Default CRUD authentication" in the security docs
		Public:     true,
		SoftDelete: true,
		Fields: []schema.Field{
			{Name: "title", Type: schema.String, Required: true, Max: ptr(300.0)},
			{Name: "body", Type: schema.Text},
			{Name: "status", Type: schema.Enum, Values: []string{"draft", "published"}, Default: "draft"},
			{Name: "author_id", Type: schema.String},
			{Name: "published_at", Type: schema.Timestamp},
		},
		Relations: []framework.Relation{
			framework.BelongsTo("author", "users", "author_id"),
			framework.HasMany("comments", "comments", "post_id"),
		},
	})
	app.Entity("comments", framework.EntityConfig{
		// public demo content — see "Default CRUD authentication" in the security docs
		Public: true,
		Fields: []schema.Field{
			{Name: "body", Type: schema.Text, Required: true},
			{Name: "post_id", Type: schema.String, Required: true},
			{Name: "author_id", Type: schema.String},
		},
		Relations: []framework.Relation{
			framework.BelongsTo("post", "posts", "post_id"),
			framework.BelongsTo("author", "users", "author_id"),
		},
	})
}

// listenAddr resolves the bind address, normalizing a bare numeric PORT
// (e.g. "8088" as injected by most PaaS providers) to ":8088".
func listenAddr() string {
	port := os.Getenv("PORT")
	if port == "" {
		return ":8080"
	}
	if !strings.Contains(port, ":") {
		return ":" + port
	}
	return port
}

func ptr[T any](v T) *T { return &v }

func searchPosts(index search.Backend) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		results, err := index.Search(r.Context(), search.Query{
			Text:  r.URL.Query().Get("q"),
			Type:  "posts",
			Limit: 10,
		})
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"data": results})
	}
}

// postsByStatus returns a handler that lists posts filtered by status.
func postsByStatus(app *framework.App, status string) http.HandlerFunc {
	entity, _ := app.Registry.Get("posts")
	handler := framework.NewCrudHandler(entity, app.DB)
	listHandler := handler.List()

	return func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		q.Set("status", status)
		r.URL.RawQuery = q.Encode()
		listHandler.ServeHTTP(w, r)
	}
}

func seed(db *sql.DB) {
	db.Exec(`INSERT OR IGNORE INTO users (id, name, email, role) VALUES ('u1', 'Alice', 'alice@example.com', 'author')`)
	db.Exec(`INSERT OR IGNORE INTO users (id, name, email, role) VALUES ('u2', 'Bob', 'bob@example.com', 'reader')`)
	db.Exec(`INSERT OR IGNORE INTO posts (id, title, body, status, author_id) VALUES ('p1', 'Hello World', 'My first post!', 'published', 'u1')`)
	db.Exec(`INSERT OR IGNORE INTO posts (id, title, body, status, author_id) VALUES ('p2', 'GoFastr Framework', 'Building the future', 'published', 'u1')`)
	db.Exec(`INSERT OR IGNORE INTO posts (id, title, body, status, author_id) VALUES ('p3', 'Draft', 'Work in progress...', 'draft', 'u1')`)
	db.Exec(`INSERT OR IGNORE INTO comments (id, body, post_id, author_id) VALUES ('c1', 'Great post!', 'p1', 'u2')`)
	db.Exec(`INSERT OR IGNORE INTO comments (id, body, post_id, author_id) VALUES ('c2', 'Interesting', 'p2', 'u2')`)
}

func seedSearch(index search.Backend) {
	ctx := context.Background()
	_ = index.Index(ctx, search.Document{ID: "p1", Type: "posts", Text: "Hello World My first post", Fields: map[string]any{"status": "published"}})
	_ = index.Index(ctx, search.Document{ID: "p2", Type: "posts", Text: "GoFastr Framework Building the future", Fields: map[string]any{"status": "published"}})
	_ = index.Index(ctx, search.Document{ID: "p3", Type: "posts", Text: "Draft Work in progress", Fields: map[string]any{"status": "draft"}})
}
