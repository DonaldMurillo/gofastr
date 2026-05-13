// crud is the typical entity-CRUD shape: one entity backed by SQLite,
// auto-migrate, full CRUD routes. Represents a "small REST API" deployment.
//
// Listens on $PORT (default :18081).
package main

import (
	"database/sql"
	"log"
	"net/http"
	"os"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework"
	_ "github.com/mattn/go-sqlite3"
)

func main() {
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		log.Fatal(err)
	}
	db.SetMaxOpenConns(1)

	app := framework.NewApp(framework.WithDB(db))
	posts := framework.Define("posts", framework.EntityConfig{
		Table: "posts",
		Fields: []schema.Field{
			{Name: "title", Type: schema.String, Required: true},
			{Name: "body", Type: schema.Text},
			{Name: "status", Type: schema.String, Default: "draft"},
		},
	})
	app.Registry.Register(posts)
	if err := framework.AutoMigrate(db, app.Registry); err != nil {
		log.Fatal(err)
	}
	framework.RegisterCrudRoutes(app.Router, framework.NewCrudHandler(posts, db), "/posts")

	// Health probe so the resource runner can wait for readiness.
	app.Router.GetFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})

	addr := ":" + getEnv("PORT", "18081")
	log.Printf("crud listening on %s", addr)
	if err := app.Start(addr); err != nil {
		log.Fatal(err)
	}
}

func getEnv(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
