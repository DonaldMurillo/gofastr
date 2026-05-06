package main

import (
	"database/sql"
	"log"
	"net/http"

	"github.com/gofastr/gofastr/core/schema"
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

	app.Entity("users", framework.EntityConfig{
		Fields: []schema.Field{
			{Name: "name", Type: schema.String, Required: true},
			{Name: "email", Type: schema.String, Required: true, Unique: true},
			{Name: "role", Type: schema.Enum, Values: []string{"admin", "author", "reader"}, Default: "reader"},
		},
	})

	app.Entity("posts", framework.EntityConfig{
		SoftDelete: true,
		Fields: []schema.Field{
			{Name: "title", Type: schema.String, Required: true},
			{Name: "body", Type: schema.Text},
			{Name: "status", Type: schema.Enum, Values: []string{"draft", "published"}, Default: "draft"},
			{Name: "author_id", Type: schema.Relation, To: "users"},
			{Name: "published_at", Type: schema.Timestamp},
		},
	})

	app.Entity("comments", framework.EntityConfig{
		Fields: []schema.Field{
			{Name: "body", Type: schema.Text, Required: true},
			{Name: "post_id", Type: schema.Relation, To: "posts", Required: true},
			{Name: "author_id", Type: schema.Relation, To: "users", Required: true},
		},
	})

	// --- Custom endpoints ---

	// GET /posts/published — filter published posts
	app.Router.Get("/posts/published", postsByStatus(app, "published"))

	// --- Seed ---

	seed(db)

	// --- Start (auto-migrates, auto-routes CRUD, auto-serves OpenAPI + Swagger) ---

	if err := app.Start(":8080"); err != nil {
		log.Fatal(err)
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
