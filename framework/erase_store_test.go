package framework

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/core/datexport"
	"github.com/DonaldMurillo/gofastr/core/migrate"
	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
)

// fakeStore stands in for a store outside the database: it holds items per
// user and records the order of calls against the database's commit.
type fakeStore struct {
	items   map[string]int
	calls   []string
	fail    error
	checkDB func() int // the user's rows still in the database when Erase runs
	rowsAt  []int
}

func (f *fakeStore) erase(_ context.Context, userID string, dryRun bool) (int, error) {
	if dryRun {
		f.calls = append(f.calls, "dry:"+userID)
	} else {
		f.calls = append(f.calls, "erase:"+userID)
	}
	if f.checkDB != nil {
		f.rowsAt = append(f.rowsAt, f.checkDB())
	}
	if f.fail != nil {
		return 0, f.fail
	}
	n := f.items[userID]
	if !dryRun {
		delete(f.items, userID)
	}
	return n, nil
}

func storeEraseApp(t *testing.T) (*App, *fakeStore) {
	t.Helper()
	datexport.Reset(t)
	t.Cleanup(func() { datexport.Reset(t) })
	db := openTestDB(t, migrate.DialectSQLite)
	app := NewApp(WithDB(db), WithoutDefaultMiddleware())
	app.Entity("notes", entity.EntityConfig{
		Table:  "notes",
		Scope:  &entity.ScopeConfig{OwnerField: "owner_id"},
		Fields: []schema.Field{{Name: "owner_id", Type: schema.String}},
	}.WithTimestamps(false))
	if err := AutoMigrate(db, app.Registry); err != nil {
		t.Fatalf("AutoMigrate: %v", err)
	}
	cascadeExec(t, db, `INSERT INTO notes (id, owner_id) VALUES ('n1','u1'), ('n2','u2')`)
	store := &fakeStore{items: map[string]int{"u1": 3, "u2": 1}}
	store.checkDB = func() int {
		var n int
		if err := db.QueryRow(`SELECT COUNT(*) FROM notes WHERE owner_id = 'u1'`).Scan(&n); err != nil {
			t.Error(err)
		}
		return n
	}
	datexport.RegisterStoreEraser(datexport.StoreEraser{Name: "files", Source: "test", Erase: store.erase})
	return app, store
}

// A store outside the database (a semantic index's files, a Redis queue)
// held the erased user's data after EraseUserData reported success
// (2026-10-27 S24, S25). Registered store erasers now run after the SQL
// erasure commits, count in the dry run without removing, and report.
func TestEraseRunsStoreErasersAfterCommit(t *testing.T) {
	app, store := storeEraseApp(t)

	rep := eraseBoth(t, app, "u1")

	if want := []string{"dry:u1", "erase:u1"}; strings.Join(store.calls, ",") != strings.Join(want, ",") {
		t.Errorf("store calls = %v, want %v", store.calls, want)
	}
	// The dry run saw the row still there (it rolls back); the erasure
	// ran its store eraser after the commit.
	if len(store.rowsAt) != 2 || store.rowsAt[1] != 0 {
		t.Errorf("user rows in the database at each store call = %v, want the last to be 0", store.rowsAt)
	}
	if store.items["u1"] != 0 || store.items["u2"] != 1 {
		t.Errorf("store items after erasure = %v, want only u2's", store.items)
	}
	if len(rep.Stores) != 1 || rep.Stores[0].Name != "files" || rep.Stores[0].RowsAffected != 3 {
		t.Errorf("report stores = %+v, want files with 3", rep.Stores)
	}
}

// The SQL part is committed before a store eraser runs, so its failure
// cannot roll anything back: the erasure returns an error naming the store
// and does not report the store erased.
func TestEraseReportsFailedStoreEraser(t *testing.T) {
	app, store := storeEraseApp(t)
	store.fail = errors.New("disk full")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	rep, err := app.EraseUserData(ctx, "u1")

	if err == nil || !strings.Contains(err.Error(), "files") || !strings.Contains(err.Error(), "disk full") {
		t.Fatalf("EraseUserData error = %v, want one naming the files store and its cause", err)
	}
	if len(rep.Stores) != 0 {
		t.Errorf("report stores = %+v, want none reported erased", rep.Stores)
	}
	if store.rowsAt[0] != 0 {
		t.Errorf("the store eraser ran before the commit (%d rows left)", store.rowsAt[0])
	}
	if len(rep.Entities) != 1 || rep.Entities[0].RowsAffected != 1 {
		t.Errorf("report entities = %+v, want the committed notes row", rep.Entities)
	}
}

// A panicking store eraser is host code on the erasure path: it surfaces
// as the store's error, not a crashed request.
func TestStoreEraserPanicIsAnError(t *testing.T) {
	app, _ := storeEraseApp(t)
	datexport.RegisterStoreEraser(datexport.StoreEraser{Name: "files", Source: "test",
		Erase: func(context.Context, string, bool) (int, error) { panic("boom") }})

	_, err := app.EraseUserData(context.Background(), "u1")

	if err == nil || !strings.Contains(err.Error(), "files") || !strings.Contains(err.Error(), "boom") {
		t.Fatalf("EraseUserData error = %v, want the store's panic as an error", err)
	}
}
