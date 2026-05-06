package main

import (
	"database/sql"
	"log"

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

	// Define entities — CRUD routes auto-registered, tables auto-migrated
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

	// Seed data
	seed(db)

	// Start auto-migrates, registers OpenAPI + Swagger UI, starts server
	if err := app.Start(":8080"); err != nil {
		log.Fatal(err)
	}
}

func seed(db *sql.DB) {
	db.Exec(`INSERT OR IGNORE INTO users (id, name, email, role) VALUES ('u1', 'Alice', 'alice@example.com', 'author')`)
	db.Exec(`INSERT OR IGNORE INTO users (id, name, email, role) VALUES ('u2', 'Bob', 'bob@example.com', 'reader')`)
	db.Exec(`INSERT OR IGNORE INTO posts (id, title, body, status, author_id) VALUES ('p1', 'Hello World', 'My first post!', 'published', 'u1')`)
	db.Exec(`INSERT OR IGNORE INTO posts (id, title, body, status, author_id) VALUES ('p2', 'GoFastr Framework', 'Building the future', 'published', 'u1')`)
	db.Exec(`INSERT OR IGNORE INTO posts (id, title, body, status, author_id) VALUES ('p3', 'Draft Post', 'Work in progress...', 'draft', 'u1')`)
	db.Exec(`INSERT OR IGNORE INTO comments (id, body, post_id, author_id) VALUES ('c1', 'Great post!', 'p1', 'u2')`)
	db.Exec(`INSERT OR IGNORE INTO comments (id, body, post_id, author_id) VALUES ('c2', 'Interesting', 'p2', 'u2')`)
}
