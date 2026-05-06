package main

import (
	"database/sql"
	"encoding/json"
	"log"
	"net/http"

	"github.com/gofastr/gofastr/core/schema"
	"github.com/gofastr/gofastr/framework"
)

func main() {
	// Database — replace with your real connection string.
	db, err := sql.Open("postgres", "postgres://user:password@localhost:5432/blog?sslmode=disable")
	if err != nil {
		log.Printf("warning: database open: %v", err)
	}
	if db != nil {
		defer db.Close()
	}

	app := framework.NewApp(framework.WithDB(db))

	defineEntities(app)
	registerRoutes(app, db)

	log.Println("blog: server starting on :8080")
	if err := app.Start(":8080"); err != nil {
		log.Fatalf("blog: server error: %v", err)
	}
}

func defineEntities(app *framework.App) {
	// Users
	app.Entity("users", framework.EntityConfig{
		Table: "users",
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

	// Posts
	app.Entity("posts", framework.EntityConfig{
		Table:      "posts",
		SoftDelete: true,
		Fields: []schema.Field{
			{Name: "title", Type: schema.String, Required: true},
			{Name: "body", Type: schema.Text},
			{Name: "status", Type: schema.Enum, Values: []string{"draft", "published"}, Default: "draft"},
			{Name: "author_id", Type: schema.String, Required: true},
			{Name: "published_at", Type: schema.Timestamp},
		},
		Relations: []framework.Relation{
			framework.HasMany("comments", "comments", "post_id"),
			framework.BelongsTo("author", "users", "author_id"),
		},
	})

	// Comments
	app.Entity("comments", framework.EntityConfig{
		Table: "comments",
		Fields: []schema.Field{
			{Name: "body", Type: schema.Text, Required: true},
			{Name: "post_id", Type: schema.String, Required: true},
			{Name: "author_id", Type: schema.String, Required: true},
		},
		Relations: []framework.Relation{
			framework.BelongsTo("post", "posts", "post_id"),
			framework.BelongsTo("author", "users", "author_id"),
		},
	})
}

func registerRoutes(app *framework.App, db *sql.DB) {
	r := app.Router

	// Build CRUD handlers
	userEntity, _ := app.Registry.Get("users")
	postEntity, _ := app.Registry.Get("posts")
	commentEntity, _ := app.Registry.Get("comments")

	users := framework.NewCrudHandler(userEntity, db)
	posts := framework.NewCrudHandler(postEntity, db)
	comments := framework.NewCrudHandler(commentEntity, db)

	// Public read routes
	r.Get("/users", users.List())
	r.Get("/users/{id}", users.Get())
	r.Get("/posts", posts.List())
	r.Get("/posts/{id}", posts.Get())
	r.Get("/comments", comments.List())
	r.Get("/comments/{id}", comments.Get())

	// Custom endpoint: GET /posts/published
	r.Get("/posts/published", http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		q := req.URL.Query()
		q.Set("status", "published")
		req.URL.RawQuery = q.Encode()
		posts.List().ServeHTTP(w, req)
	}))

	// Custom endpoint with MCP: GET /posts/search?q=keyword
	r.Get("/posts/search", http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		keyword := req.URL.Query().Get("q")
		if keyword == "" {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(map[string]any{
				"error":   "missing query parameter: q",
				"success": false,
			})
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"query": keyword,
			"posts": []any{},
		})
	}))

	// Auth middleware for write routes
	authMW := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Header.Get("Authorization") == "" {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusUnauthorized)
				json.NewEncoder(w).Encode(map[string]any{
					"error":   "authorization required",
					"success": false,
				})
				return
			}
			next.ServeHTTP(w, r)
		})
	}

	write := r.Group("", authMW)
	write.Post("/users", users.Create())
	write.Put("/users/{id}", users.Update())
	write.Delete("/users/{id}", users.Delete())
	write.Post("/posts", posts.Create())
	write.Put("/posts/{id}", posts.Update())
	write.Delete("/posts/{id}", posts.Delete())
	write.Post("/comments", comments.Create())
	write.Put("/comments/{id}", comments.Update())
	write.Delete("/comments/{id}", comments.Delete())
}

// registerMCPTools shows how to expose entity operations as MCP tools.
func registerMCPTools(app *framework.App, db *sql.DB) {
	_ = db
	_ = app.MCP
	// MCP tools would be registered here if MCP server is configured:
	// app.MCP.RegisterTool("search_posts", ...)
	// The MCP field is nil unless WithMCPServer is passed to NewApp.
}
