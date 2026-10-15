// Package main is a Hacker News reader in the HNPWA shape: the same app
// many frameworks have built, so their first loads can be compared on
// the same pages. Five ranked lists of thirty (top, newest, ask, show,
// jobs) paged with ?page=N, a story page with its full comment thread,
// and a user page.
//
// The data is synthetic: a fixed-seed generator (gen.go) writes a few
// hundred stories and a few thousand comments into SQLite on the first
// boot, through the framework's entity layer, and the pages read it back
// with typed queries. Nothing is fetched from the network and no real
// Hacker News content is copied. Ages are measured from a fixed date,
// so every run serves the same bytes.
//
// The pages are framework/ui components (EntryList, CommentThread,
// Pagination, DetailList, EmptyState) under the app's own top bar
// (hnheader, an owned style sheet). The app ships no other CSS.
//
// Run with:
//
//	go run ./examples/hn    # :8097, or $PORT; data in ./hn.db, or $HN_DB
package main

import (
	"context"
	"database/sql"
	"log"
	"net/http"
	"os"

	"github.com/DonaldMurillo/gofastr/framework"
	"github.com/DonaldMurillo/gofastr/framework/isolation"
	"github.com/DonaldMurillo/gofastr/framework/uihost"
	_ "github.com/DonaldMurillo/gofastr/sqlite/stdlib"
)

// defaultAddr is the fallback when $PORT is unset.
const defaultAddr = ":8097"

func main() {
	path := os.Getenv("HN_DB")
	if path == "" {
		path = "hn.db"
	}
	app, err := buildApp(path)
	if err != nil {
		log.Fatal(err)
	}
	addr, err := isolation.ListenAddr(".", defaultAddr)
	if err != nil {
		log.Fatal(err)
	}
	log.Printf("hn listening on http://localhost%s", addr)
	if err := app.Start(addr); err != nil && err != http.ErrServerClosed {
		log.Fatal(err)
	}
}

// buildApp opens the database, creates and seeds the tables when they
// are empty, and mounts the pages, without binding a port, so tests
// drive it in-process.
func buildApp(dbPath string) (*framework.App, error) {
	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		return nil, err
	}
	app := framework.NewApp(
		framework.WithDB(db),
		framework.WithConfig(framework.AppConfig{Name: "hn"}),
	)
	registerEntities(app)
	if err := framework.AutoMigrate(db, app.Registry); err != nil {
		db.Close()
		return nil, err
	}
	st, err := newStore(app)
	if err != nil {
		db.Close()
		return nil, err
	}
	if err := st.seed(context.Background()); err != nil {
		db.Close()
		return nil, err
	}
	// The pages hold no live islands, so the page asks for no
	// server-push channel: nothing stays open after the load.
	host := uihost.New(buildSite(st),
		uihost.WithNotFoundScreen(&notFoundScreen{}),
		uihost.WithNoLiveChannel(),
	)
	app.Use(host.RouteMatchMiddleware())
	app.Mount(host)
	return app, nil
}
