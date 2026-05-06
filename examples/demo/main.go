package main

import (
	"database/sql"
	"log"
	"net/http"

	"github.com/gofastr/gofastr/core/openapi"
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

	// Create tables
	db.Exec(`CREATE TABLE IF NOT EXISTS users (
		id TEXT PRIMARY KEY, name TEXT NOT NULL, email TEXT NOT NULL UNIQUE,
		role TEXT DEFAULT 'reader', created_at DATETIME DEFAULT CURRENT_TIMESTAMP, updated_at DATETIME DEFAULT CURRENT_TIMESTAMP)`)
	db.Exec(`CREATE TABLE IF NOT EXISTS posts (
		id TEXT PRIMARY KEY, title TEXT NOT NULL, body TEXT DEFAULT '',
		status TEXT DEFAULT 'draft', author_id TEXT DEFAULT '', published_at TEXT DEFAULT '',
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP, updated_at DATETIME DEFAULT CURRENT_TIMESTAMP)`)
	db.Exec(`CREATE TABLE IF NOT EXISTS comments (
		id TEXT PRIMARY KEY, body TEXT NOT NULL, post_id TEXT NOT NULL, author_id TEXT NOT NULL,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP, updated_at DATETIME DEFAULT CURRENT_TIMESTAMP)`)

	// Seed some data
	db.Exec(`INSERT OR IGNORE INTO users (id, name, email, role) VALUES ('u1', 'Alice', 'alice@example.com', 'author')`)
	db.Exec(`INSERT OR IGNORE INTO users (id, name, email, role) VALUES ('u2', 'Bob', 'bob@example.com', 'reader')`)
	db.Exec(`INSERT OR IGNORE INTO posts (id, title, body, status, author_id) VALUES ('p1', 'Hello World', 'My first post!', 'published', 'u1')`)
	db.Exec(`INSERT OR IGNORE INTO posts (id, title, body, status, author_id) VALUES ('p2', 'GoFastr Framework', 'Building the future of Go frameworks', 'published', 'u1')`)
	db.Exec(`INSERT OR IGNORE INTO posts (id, title, body, status, author_id) VALUES ('p3', 'Draft Post', 'Work in progress...', 'draft', 'u1')`)
	db.Exec(`INSERT OR IGNORE INTO comments (id, body, post_id, author_id) VALUES ('c1', 'Great post!', 'p1', 'u2')`)
	db.Exec(`INSERT OR IGNORE INTO comments (id, body, post_id, author_id) VALUES ('c2', 'Interesting framework', 'p2', 'u2')`)

	app := framework.NewApp(framework.WithDB(db))

	// Define entities
	usersEntity := framework.Define("users", framework.EntityConfig{
		Table: "users",
		Fields: []schema.Field{
			{Name: "name", Type: schema.String, Required: true},
			{Name: "email", Type: schema.String, Required: true},
			{Name: "role", Type: schema.Enum, Values: []string{"admin", "author", "reader"}, Default: "reader"},
		},
	})
	app.Registry.Register(usersEntity)

	postsEntity := framework.Define("posts", framework.EntityConfig{
		Table:      "posts",
		SoftDelete: true,
		Fields: []schema.Field{
			{Name: "title", Type: schema.String, Required: true},
			{Name: "body", Type: schema.Text},
			{Name: "status", Type: schema.Enum, Values: []string{"draft", "published"}, Default: "draft"},
			{Name: "author_id", Type: schema.Relation, To: "users"},
			{Name: "published_at", Type: schema.Timestamp},
		},
	})
	app.Registry.Register(postsEntity)

	commentsEntity := framework.Define("comments", framework.EntityConfig{
		Table: "comments",
		Fields: []schema.Field{
			{Name: "body", Type: schema.Text, Required: true},
			{Name: "post_id", Type: schema.Relation, To: "posts", Required: true},
			{Name: "author_id", Type: schema.Relation, To: "users", Required: true},
		},
	})
	app.Registry.Register(commentsEntity)

	// Register CRUD routes
	framework.RegisterCrudRoutes(app.Router, framework.NewCrudHandler(usersEntity, db), "/users")
	framework.RegisterCrudRoutes(app.Router, framework.NewCrudHandler(postsEntity, db), "/posts")
	framework.RegisterCrudRoutes(app.Router, framework.NewCrudHandler(commentsEntity, db), "/comments")

	// Generate and serve OpenAPI spec
	spec := framework.EntityOpenAPI(app.Registry, "GoFastr Blog API", "1.0.0")
	app.Router.Get("/openapi.json", openapi.Handler(spec))
	app.Router.Get("/docs/", openapi.SwaggerUIHandler(spec, "/docs"))

	// Root redirect to docs
	app.Router.Get("/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/docs/", http.StatusFound)
	}))

	log.Println("GoFastr Blog API running at http://localhost:8080")
	log.Println("  OpenAPI spec: http://localhost:8080/openapi.json")
	log.Println("  Swagger UI:   http://localhost:8080/docs/")
	log.Println("  Users:        http://localhost:8080/users")
	log.Println("  Posts:        http://localhost:8080/posts")
	log.Println("  Comments:     http://localhost:8080/comments")
	if err := app.Start(":8080"); err != nil {
		log.Fatal(err)
	}
}
