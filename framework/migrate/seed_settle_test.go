package migrate

import (
	"context"
	"database/sql"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
)

// A shutdown that lands as a seed finishes must not drop its ledger row:
// the row was written on the boot's context, failed once it was
// cancelled, and the next boot seeded again (2026-10-27 bug bash,
// settlectx).
func TestSeedLedgerSurvivesCancel(t *testing.T) {
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Skip("sqlite3 driver not available")
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { db.Close() })
	if _, err := db.Exec("CREATE TABLE posts (id TEXT PRIMARY KEY, title TEXT)"); err != nil {
		t.Fatalf("create table: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	runs := 0
	e := rawEnt("posts", "posts",
		[]schema.Field{{Name: "id", Type: schema.String}, {Name: "title", Type: schema.String}},
		nil, "id")
	e.Config.Seed = func(context.Context, *sql.DB) error {
		runs++
		cancel() // shutdown arrives as the seed finishes
		return nil
	}
	reg := multiVersionRegistry{e}

	_ = RunSeeds(ctx, db, reg)
	if err := RunSeeds(context.Background(), db, reg); err != nil {
		t.Fatalf("second boot: %v", err)
	}
	if runs != 1 {
		t.Fatalf("seed ran %d times: its ledger row was dropped at the cancel", runs)
	}
}
