package db_test

import (
	"errors"
	"testing"

	_ "github.com/DonaldMurillo/gofastr/sqlite/stdlib"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework"
	"github.com/DonaldMurillo/gofastr/kiln/db"
)

// A rebuild whose rows the declared table refuses puts the table back
// on its own, rows and indexes included, and says so with
// ErrTableRestored: a caller that does not migrate again still has the
// table it had.
func TestRebuildRefusalPutsTableBack(t *testing.T) {
	d, cleanup, err := db.EphemeralSQLite("kiln-rebuild")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(cleanup)
	posts := func(fields ...schema.Field) *framework.App {
		app := framework.NewApp(framework.WithDB(d))
		app.Entity("posts", framework.EntityConfig{
			Fields:  append([]schema.Field{{Name: "title", Type: schema.String}}, fields...),
			Indices: []framework.Index{{Name: "posts_title_idx", Columns: []string{"title"}}},
		})
		return app
	}
	if err := db.Migrate(d, posts().Registry); err != nil {
		t.Fatal(err)
	}
	if _, err := d.Exec(`INSERT INTO posts (id, title) VALUES ('p1', 'kept')`); err != nil {
		t.Fatal(err)
	}
	// The declared table adds a required column the row has no value for.
	err = db.RebuildTable(d, posts(schema.Field{Name: "code", Type: schema.String, Required: true}).Registry, "posts")
	if !errors.Is(err, db.ErrTableRestored) {
		t.Fatalf("RebuildTable = %v, want an error wrapping ErrTableRestored", err)
	}
	var n int
	if err := d.QueryRow(`SELECT count(*) FROM posts WHERE id = 'p1' AND title = 'kept'`).Scan(&n); err != nil || n != 1 {
		t.Errorf("the row after the put-back: %d (%v), want 1", n, err)
	}
	if err := d.QueryRow(`SELECT count(*) FROM pragma_table_info('posts') WHERE name = 'code'`).Scan(&n); err != nil || n != 0 {
		t.Errorf("the put-back table has the declared table's column: %d (%v)", n, err)
	}
	if err := d.QueryRow(`SELECT count(*) FROM sqlite_master WHERE type = 'index' AND name = 'posts_title_idx' AND tbl_name = 'posts'`).Scan(&n); err != nil || n != 1 {
		t.Errorf("posts_title_idx after the put-back: %d (%v), want 1", n, err)
	}
	if err := d.QueryRow(`SELECT count(*) FROM sqlite_master WHERE name LIKE '_kiln_rebuild_%'`).Scan(&n); err != nil || n != 0 {
		t.Errorf("%d set-aside tables left (%v)", n, err)
	}
}
