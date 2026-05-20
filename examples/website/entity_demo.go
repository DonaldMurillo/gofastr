package main

import (
	"database/sql"

	_ "github.com/mattn/go-sqlite3"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework"
	"github.com/DonaldMurillo/gofastr/framework/entity"
)

// setupDemoEntity wires a sqlite-backed "articles" entity onto the
// framework app so the CRUD routes (and their /llm.md documentation)
// are live. Uses an in-file sqlite database under /tmp so the data
// survives across dev-watch restarts within the same session.
//
// The framework's Start() method calls AutoMigrate, so we only need
// to set the DB and register the entity — no explicit migration call.
func setupDemoEntity(fwApp *framework.App) error {
	db, err := sql.Open("sqlite3", "/tmp/gofastr-website-demo.db")
	if err != nil {
		return err
	}
	fwApp.DB = db

	// Define the articles entity with a few representative fields.
	// CRUD routes auto-register because DB is set.
	fwApp.Entity("articles", entity.EntityConfig{
		Fields: []schema.Field{
			{Name: "title", Type: schema.String, Required: true},
			{Name: "body", Type: schema.Text},
			{Name: "status", Type: schema.Enum, Values: []string{"draft", "published", "archived"}, Default: "draft"},
			{Name: "author", Type: schema.String},
			{Name: "views", Type: schema.Int, Default: 0},
		},
	})

	return nil
}
