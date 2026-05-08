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
	"github.com/gofastr/gofastr/framework"
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

	if err := app.EntitiesFromDir(entityDir()); err != nil {
		log.Fatal(err)
	}

	// --- Custom endpoints ---

	// GET /posts/published — filter published posts
	app.Router.Get("/posts/published", postsByStatus(app, "published"))
	searchIndex := search.NewMemory()
	app.Router.Get("/posts/search", searchPosts(searchIndex))

	// --- Seed ---

	seed(db)
	seedSearch(searchIndex)

	// --- Start (auto-migrates, auto-routes CRUD, auto-serves OpenAPI + Swagger) ---

	if err := app.Start(":8080"); err != nil {
		log.Fatal(err)
	}
}

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

func entityDir() string {
	candidates := []string{
		filepath.Join("examples", "blog", "entities"),
		"entities",
	}
	for _, candidate := range candidates {
		if info, err := os.Stat(candidate); err == nil && info.IsDir() {
			return candidate
		}
	}
	return candidates[0]
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
