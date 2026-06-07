package main

import (
	"database/sql"
	"encoding/json"
	"log"
	"net/http"
	"os"
	"path/filepath"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/core/static"
	"github.com/DonaldMurillo/gofastr/framework"
	_ "github.com/mattn/go-sqlite3"
)

func main() {
	db, err := sql.Open("sqlite3", "./spa.db")
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()

	app := framework.NewApp(
		framework.WithDB(db),
		// Auto-CRUD mounts under /api (GET /api/articles, …) so Vue Router
		// owns the bare paths (/articles, /projects) for client-side routes.
		framework.WithConfig(framework.AppConfig{Name: "spa-example", APIPrefix: "/api"}),
	)

	// --- Entities — auto-CRUD is on (DB set); APIPrefix puts the routes under /api. ---

	app.Entity("articles", framework.EntityConfig{
		Fields: []schema.Field{
			{Name: "title", Type: schema.String, Required: true},
			{Name: "summary", Type: schema.Text},
			{Name: "body", Type: schema.Text},
			{Name: "category", Type: schema.String},
		},
	})

	app.Entity("projects", framework.EntityConfig{
		Fields: []schema.Field{
			{Name: "name", Type: schema.String, Required: true},
			{Name: "description", Type: schema.Text},
			{Name: "url", Type: schema.String},
		},
	})

	// --- Seed data (must run after tables exist) ---
	framework.AutoMigrate(db, app.Registry)
	seed(db)

	// Custom API endpoint — site metadata. Entity CRUD already mounts under
	// /api via APIPrefix; this just adds one more route alongside it.
	app.Router().Get("/api/site", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"name":   "GoFastr SPA Demo",
			"nav":    []string{"home", "articles", "projects", "about"},
			"footer": "Built with GoFastr",
		})
	}))

	// --- SPA static file serving ---
	// Vue Router uses History API — real URLs like /articles, /about.
	// The server must serve index.html for all non-API, non-static routes.

	staticDir := resolveStaticDir()

	spaHandler := static.Handler(static.Config{
		FS:     os.DirFS(staticDir),
		Prefix: "/",
		SPA:    true,
	})

	// Catch-all: serves static files, falls back to index.html for SPA routes
	app.Router().Get("/{path...}", spaHandler)

	// --- Start ---

	log.Println("SPA example starting on :3090")
	log.Println("Open http://localhost:3090 in your browser")
	if err := app.Start(":3090"); err != nil {
		log.Fatal(err)
	}
}

func resolveStaticDir() string {
	candidates := []string{
		"static",
		filepath.Join("examples", "spa", "static"),
	}
	for _, dir := range candidates {
		if info, err := os.Stat(dir); err == nil && info.IsDir() {
			abs, _ := filepath.Abs(dir)
			return abs
		}
	}
	log.Fatal("Could not find static/ directory. Run from examples/spa/")
	return ""
}

func seed(db *sql.DB) {
	db.Exec(`INSERT OR IGNORE INTO articles (id, title, summary, body, category) VALUES ('a1', 'Getting Started with Go', 'A beginner''s guide to Go.', 'Go is a statically typed, compiled language designed at Google. It is simple, fast, and has excellent concurrency support. In this article we cover the basics: installation, hello world, and your first HTTP server.', 'tutorial')`)
	db.Exec(`INSERT OR IGNORE INTO articles (id, title, summary, body, category) VALUES ('a2', 'Why We Built GoFastr', 'The story behind the framework.', 'Every framework we tried assumed humans write every route by hand. We wanted something designed for AI coding agents as the primary author. GoFastr is a two-layer framework: core primitives for control, entity system for speed.', 'story')`)
	db.Exec(`INSERT OR IGNORE INTO articles (id, title, summary, body, category) VALUES ('a3', 'MCP-Native Apps', 'Making your app an MCP server.', 'The Model Context Protocol lets AI tools interact with your app directly. With GoFastr, every entity can auto-generate MCP tools — list, get, create, update, delete — so any MCP client can work with your data out of the box.', 'tutorial')`)

	db.Exec(`INSERT OR IGNORE INTO projects (id, name, description, url) VALUES ('p1', 'GoFastr', 'Go fullstack framework for the AI era', 'https://github.com/DonaldMurillo/gofastr')`)
	db.Exec(`INSERT OR IGNORE INTO projects (id, name, description, url) VALUES ('p2', 'GoFastr CLI', 'Command-line tools for scaffolding and dev', 'https://github.com/DonaldMurillo/gofastr')`)
	db.Exec(`INSERT OR IGNORE INTO projects (id, name, description, url) VALUES ('p3', 'Example Apps', 'Reference applications built with GoFastr', 'https://github.com/DonaldMurillo/gofastr/tree/main/examples')`)
}
