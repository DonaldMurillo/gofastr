// full is a realistic shape: three related entities, includes wired up,
// audit log enabled, an in-process cron job, the OpenAPI surface, and
// MCP enabled per entity. Represents what a non-trivial deployment
// actually carries in its binary.
//
// Listens on $PORT (default :18082).
package main

import (
	"context"
	"database/sql"
	"log"
	"net/http"
	"os"

	"github.com/gofastr/gofastr/core/schema"
	"github.com/gofastr/gofastr/framework"
	_ "github.com/mattn/go-sqlite3"
)

func main() {
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		log.Fatal(err)
	}
	db.SetMaxOpenConns(1)

	app := framework.NewApp(framework.WithDB(db))

	authors := framework.Define("authors", framework.EntityConfig{
		Table: "authors",
		Fields: []schema.Field{
			{Name: "name", Type: schema.String, Required: true},
			{Name: "email", Type: schema.String, Unique: true},
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
		},
		MCP: true,
	})
	app.Registry.Register(authors)
	app.Registry.Register(posts)
	app.Registry.Register(comments)

	if err := framework.AutoMigrate(db, app.Registry); err != nil {
		log.Fatal(err)
	}

	framework.RegisterCrudRoutes(app.Router, framework.NewCrudHandler(authors, db), "/authors")
	framework.RegisterCrudRoutes(app.Router, framework.NewCrudHandler(posts, db), "/posts")
	framework.RegisterCrudRoutes(app.Router, framework.NewCrudHandler(comments, db), "/comments")

	// Audit log on every write.
	app.WithAuditLog(framework.AuditConfig{
		Table: "audit_log",
		Actor: func(_ context.Context) string { return "system" },
	})

	// A representative cron job.
	sched := framework.NewScheduler()
	if err := sched.Register(framework.CronJob{
		Name: "noop_minute",
		Spec: "* * * * *",
		Run:  func(_ context.Context) error { return nil },
	}); err != nil {
		log.Fatal(err)
	}
	app.AddCron(sched)

	app.Router.GetFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})

	addr := ":" + getEnv("PORT", "18082")
	log.Printf("full listening on %s", addr)
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
