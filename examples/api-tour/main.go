// Package main is a tour of the v2 API surface added to the framework:
//
//	- ?include=author.profile,comments    (eager-loading, nested)
//	- ?cursor=...&limit=...               (keyset pagination)
//	- POST/PATCH/DELETE /{table}/_batch   (atomic batches)
//	- GET /{table}/_events                (Server-Sent Events stream)
//	- multipart/form-data uploads on Image fields
//	- BelongsTo FK constraints (enforced at runtime under PRAGMA on SQLite)
//
// Run with:
//
//	go run ./examples/api-tour
//
// Then poke at:
//
//	curl http://localhost:8080/posts?include=author.profile,comments
//	curl 'http://localhost:8080/posts?cursor=&limit=2'
//	curl -X POST http://localhost:8080/posts/_batch -d '{"items":[{"title":"A","authorId":"u1"}]}'
//	curl http://localhost:8080/posts/_events
//
// The OpenAPI spec lives at /openapi.json and Swagger UI at /docs/.
package main

import (
	"database/sql"
	"log"
	"os"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/core/upload"
	"github.com/DonaldMurillo/gofastr/framework"
	_ "github.com/mattn/go-sqlite3"
)

func main() {
	db, err := sql.Open("sqlite3", "./api-tour.db")
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()
	// FK enforcement is opt-in on SQLite; the framework emits the constraints
	// regardless but we need this PRAGMA to actually have them rejected.
	if _, err := db.Exec("PRAGMA foreign_keys = ON"); err != nil {
		log.Fatal(err)
	}

	uploadDir := "./api-tour-uploads"
	_ = os.MkdirAll(uploadDir, 0o755)

	app := framework.NewApp(
		framework.WithDB(db),
		framework.WithConfig(framework.AppConfig{Name: "api-tour"}),
		framework.WithFileStorage(upload.NewLocalStorage(uploadDir)),
	)

	app.Entity("users", framework.EntityConfig{
		Table: "users",
		Fields: []schema.Field{
			{Name: "id", Type: schema.UUID, AutoGenerate: schema.AutoUUID},
			{Name: "name", Type: schema.String, Required: true},
			{Name: "avatar", Type: schema.Image},
		},
		Relations: []framework.Relation{
			framework.HasOne("profile", "profiles", "user_id"),
			framework.HasMany("posts", "posts", "author_id"),
		},
	})

	app.Entity("profiles", framework.EntityConfig{
		Table: "profiles",
		Fields: []schema.Field{
			{Name: "id", Type: schema.UUID, AutoGenerate: schema.AutoUUID},
			{Name: "user_id", Type: schema.String, Required: true},
			{Name: "bio", Type: schema.Text},
		},
		Relations: []framework.Relation{
			framework.BelongsTo("user", "users", "user_id"),
		},
	})

	app.Entity("posts", framework.EntityConfig{
		Table:       "posts",
		CursorField: "created_at", // pagination by recency, not PK
		Fields: []schema.Field{
			{Name: "id", Type: schema.UUID, AutoGenerate: schema.AutoUUID},
			{Name: "title", Type: schema.String, Required: true},
			{Name: "body", Type: schema.Text},
			{Name: "author_id", Type: schema.String, Required: true},
		},
		Relations: []framework.Relation{
			framework.BelongsTo("author", "users", "author_id"),
			framework.HasMany("comments", "comments", "post_id"),
		},
	})

	app.Entity("comments", framework.EntityConfig{
		Table: "comments",
		Fields: []schema.Field{
			{Name: "id", Type: schema.UUID, AutoGenerate: schema.AutoUUID},
			{Name: "body", Type: schema.Text, Required: true},
			{Name: "post_id", Type: schema.String, Required: true},
		},
		Relations: []framework.Relation{
			framework.BelongsTo("post", "posts", "post_id"),
		},
	})

	// Demo seed: a couple of users + one post + comments. Idempotent — only
	// inserts if the table is empty.
	seedDemoData(db)

	addr := ":8080"
	if port := os.Getenv("PORT"); port != "" {
		addr = ":" + port
	}
	log.Printf("api-tour listening on %s", addr)
	if err := app.Start(addr); err != nil {
		log.Fatal(err)
	}
}

// seedDemoData inserts a tiny graph so curling the endpoints returns
// non-empty bodies on a fresh DB. Safe to re-run.
func seedDemoData(db *sql.DB) {
	var n int
	_ = db.QueryRow("SELECT COUNT(*) FROM users").Scan(&n)
	if n > 0 {
		return
	}
	stmts := []string{
		"INSERT INTO users(id, name) VALUES ('u1', 'Alice')",
		"INSERT INTO users(id, name) VALUES ('u2', 'Bob')",
		"INSERT INTO profiles(id, user_id, bio) VALUES ('p1', 'u1', 'Hello from Alice')",
		"INSERT INTO posts(id, title, body, author_id) VALUES ('post-1', 'First', 'Hello world', 'u1')",
		"INSERT INTO posts(id, title, body, author_id) VALUES ('post-2', 'Second', 'More to say', 'u2')",
		"INSERT INTO comments(id, body, post_id) VALUES ('c1', 'great post', 'post-1')",
		"INSERT INTO comments(id, body, post_id) VALUES ('c2', 'agreed', 'post-1')",
	}
	for _, s := range stmts {
		if _, err := db.Exec(s); err != nil {
			log.Printf("seed warning: %v", err)
		}
	}
}
