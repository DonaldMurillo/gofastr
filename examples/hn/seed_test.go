package main

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"

	"github.com/DonaldMurillo/gofastr/framework"
)

// A seed that fails part-way leaves no rows behind: the next start sees
// empty tables and seeds again, rather than keeping users with no
// stories or comments for good.
func TestFailedSeedRollsBack(t *testing.T) {
	db, err := sql.Open("sqlite3", filepath.Join(t.TempDir(), "hn.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	app := framework.NewApp(framework.WithDB(db), framework.WithConfig(framework.AppConfig{Name: "hn"}))
	registerEntities(app)
	if err := framework.AutoMigrate(db, app.Registry); err != nil {
		t.Fatal(err)
	}
	st, err := newStore(app)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`DROP TABLE comments`); err != nil {
		t.Fatal(err)
	}
	if err := st.seed(context.Background()); err == nil {
		t.Fatal("premise: seed succeeded without a comments table")
	}
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM users`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("%d users survived a failed seed", n)
	}
}
