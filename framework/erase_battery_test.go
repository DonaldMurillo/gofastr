package framework

import (
	"context"
	"database/sql"
	"strings"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/core/datexport"
	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/framework/internal/testdb"
	"github.com/DonaldMurillo/gofastr/framework/migrate"
)

// Two anonymizers on one table and match column: each tombstoned the
// match column, so the second matched nothing and the phone survived.
func TestEraseTwoAnonymizersOneColumn(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *sql.DB, _ Dialect) {
		datexport.Reset(t)
		cascadeExec(t, db, `CREATE TABLE contacts (id TEXT PRIMARY KEY, user_id TEXT, email TEXT, phone TEXT)`,
			`INSERT INTO contacts VALUES ('c1','u1','a@x','555'), ('c2','u2','b@x','666')`)
		for name, col := range map[string]string{"contacts_email": "email", "contacts_phone": "phone"} {
			datexport.RegisterEraser(datexport.DataEraser{
				Name: name, Source: "test", Table: "contacts", Column: "user_id",
				Mode: datexport.EraseAnonymize, ScrubColumns: []string{col},
			})
		}
		app := NewApp(WithDB(db))
		rep := eraseBoth(t, app, "u1")
		var uid, email, phone string
		if err := db.QueryRow(`SELECT user_id, email, phone FROM contacts WHERE id = 'c1'`).Scan(&uid, &email, &phone); err != nil {
			t.Fatal(err)
		}
		if uid != "[erased]" || email != "[erased]" || phone != "[erased]" {
			t.Errorf("c1 = (%q, %q, %q), want every column erased", uid, email, phone)
		}
		for _, b := range rep.Batteries {
			if b.RowsAffected != 1 {
				t.Errorf("%s RowsAffected = %d, want 1", b.Name, b.RowsAffected)
			}
		}
		wantIDs(t, db, "contacts", "c1", "c2")
		var other string
		if err := db.QueryRow(`SELECT email FROM contacts WHERE id = 'c2'`).Scan(&other); err != nil || other != "b@x" {
			t.Errorf("u2's email = %q, %v; want it kept", other, err)
		}
	})
}

// A delete and an anonymizer on one table: the anonymizer, sorted first,
// tombstoned the match column and the delete then matched nothing.
func TestEraseDeleteAfterAnonymizer(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *sql.DB, _ Dialect) {
		datexport.Reset(t)
		cascadeExec(t, db, `CREATE TABLE notes2 (id TEXT PRIMARY KEY, user_id TEXT, body TEXT)`,
			`INSERT INTO notes2 VALUES ('n1','u1','secret')`)
		datexport.RegisterEraser(datexport.DataEraser{
			Name: "a_scrub", Source: "test", Table: "notes2", Column: "user_id",
			Mode: datexport.EraseAnonymize, ScrubColumns: []string{"body"},
		})
		datexport.RegisterEraser(datexport.DataEraser{
			Name: "b_drop", Source: "test", Table: "notes2", Column: "user_id", Mode: datexport.EraseDelete,
		})
		rep := eraseBoth(t, NewApp(WithDB(db)), "u1")
		wantIDs(t, db, "notes2")
		if len(rep.Batteries) != 2 || rep.Batteries[0].Name != "a_scrub" || rep.Batteries[1].RowsAffected != 1 {
			t.Errorf("batteries = %+v, want a_scrub then b_drop deleting 1", rep.Batteries)
		}
	})
}

// newUsersApp is a users entity no owner scopes, holding an email and an
// avatar, erased by a battery delete eraser matching its id, plus an
// anonymizer on a profiles entity, the way battery/auth erases its users.
func newUsersApp(t *testing.T, db *sql.DB, store *recordingDeleteStore) *App {
	t.Helper()
	datexport.Reset(t)
	app := NewApp(WithDB(db), WithoutDefaultMiddleware(), WithFileStorage(store))
	app.Entity("users", entity.EntityConfig{
		Table: "users",
		Fields: []schema.Field{
			{Name: "email", Type: schema.String},
			{Name: "avatar", Type: schema.Image},
		},
	}.WithTimestamps(false))
	app.Entity("bios", entity.EntityConfig{
		Table: "bios",
		Fields: []schema.Field{
			{Name: "user_id", Type: schema.String},
			{Name: "body", Type: schema.String},
			{Name: "photo", Type: schema.Image},
		},
	}.WithTimestamps(false))
	if err := AutoMigrate(db, app.Registry); err != nil {
		t.Fatalf("AutoMigrate: %v", err)
	}
	app.WithAuditLog(AuditConfig{})
	datexport.RegisterEraser(datexport.DataEraser{
		Name: "users", Source: "test", Table: "users", Column: "id", Mode: datexport.EraseDelete,
	})
	datexport.RegisterEraser(datexport.DataEraser{
		Name: "bios", Source: "test", Table: "bios", Column: "user_id",
		Mode: datexport.EraseAnonymize, ScrubColumns: []string{"body", "photo"},
	})
	return app
}

// A row a battery eraser deletes or scrubs keeps no audit copy: the
// audit plane scrubbed only the rows the entity plane staged, so an
// admin's audit diff of the user's email outlived the user.
func TestEraseBatteryRowsScrubAudit(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *sql.DB, _ Dialect) {
		app := newUsersApp(t, db, &recordingDeleteStore{present: map[string]bool{}})
		cascadeExec(t, db, `INSERT INTO users (id, email) VALUES ('u1','a@x'), ('u2','b@x')`,
			`INSERT INTO bios (id, user_id, body) VALUES ('b1','u1','about me')`,
			`INSERT INTO audit_log (id, entity, op, record_id, actor_id, created_at, diff) VALUES
			('r1','users','create','u1','admin','2024-01-01T00:00:00Z','{"new":{"email":"a@x"}}'),
			('r2','bios','create','b1','admin','2024-01-01T00:00:00Z','{"new":{"body":"about me"}}'),
			('r3','users','create','u2','admin','2024-01-01T00:00:00Z','{"new":{"email":"b@x"}}')`)
		eraseBoth(t, app, "u1")
		for id, want := range map[string]string{"r1": erasedAuditDiff, "r2": erasedAuditDiff, "r3": `{"new":{"email":"b@x"}}`} {
			var diff string
			if err := db.QueryRow(`SELECT diff FROM audit_log WHERE id = $1`, id).Scan(&diff); err != nil {
				t.Fatal(err)
			}
			if diff != want {
				t.Errorf("audit %s diff = %q, want %q", id, diff, want)
			}
		}
	})
}

// A row a battery eraser deletes takes its stored objects with it, and a
// file column an anonymizer scrubs gives up its object: only the entity
// plane's rows were read for keys, so the avatar and photo stayed.
func TestEraseBatteryRowsDropFiles(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *sql.DB, _ Dialect) {
		store := &recordingDeleteStore{present: map[string]bool{"av/u1.png": true, "av/u2.png": true, "ph/u1.png": true}}
		app := newUsersApp(t, db, store)
		cascadeExec(t, db, `INSERT INTO users (id, email, avatar) VALUES ('u1','a@x','av/u1.png'), ('u2','b@x','av/u2.png')`,
			`INSERT INTO bios (id, user_id, body, photo) VALUES ('b1','u1','hi','ph/u1.png')`)
		eraseBoth(t, app, "u1")
		if store.present["av/u1.png"] || store.present["ph/u1.png"] || !store.present["av/u2.png"] {
			t.Errorf("objects after erasure = %v, want only av/u2.png", store.present)
		}
	})
}

// The erasure locks the rows it reads stored-object keys from. Under
// Postgres READ COMMITTED an avatar swapped in between the read and the
// delete went with the row, and its object stayed reachable.
func TestEraseLocksRowsItReadsKeysOf(t *testing.T) {
	db := testdb.Open(t, migrate.DialectPostgres)
	// The swap needs a connection of its own while the erasure holds one;
	// the schema binding travels in the DSN, so a second one sees it too.
	db.SetMaxOpenConns(2)
	datexport.Reset(t)
	store := &recordingDeleteStore{present: map[string]bool{"av/old.png": true, "av/new.png": true}}
	app := NewApp(WithDB(db), WithFileStorage(store))
	app.Registry.Register(entity.Define("profiles", entity.EntityConfig{
		Table:  "profiles",
		Scope:  &entity.ScopeConfig{OwnerField: "owner_id"},
		Fields: []schema.Field{{Name: "avatar", Type: schema.Image}, {Name: "owner_id", Type: schema.String}},
	}.WithTimestamps(false)))
	if err := AutoMigrate(db, app.Registry); err != nil {
		t.Fatalf("AutoMigrate: %v", err)
	}
	cascadeExec(t, db, `INSERT INTO profiles (id, avatar, owner_id) VALUES ('p1','av/old.png','u1')`)
	// Another request swaps the avatar between the read and the delete.
	// Its UPDATE either waits on the erasure's row lock (and gives up
	// here) or lands in the row the erasure then deletes.
	var swapErr error
	eraseKeysRead = func() {
		ctx := context.Background()
		conn, err := db.Conn(ctx)
		if err != nil {
			swapErr = err
			return
		}
		defer conn.Close()
		if _, err := conn.ExecContext(ctx, `SET lock_timeout = '300ms'`); err != nil {
			swapErr = err
			return
		}
		_, swapErr = conn.ExecContext(ctx, `UPDATE profiles SET avatar = 'av/new.png' WHERE id = 'p1'`)
		_, _ = conn.ExecContext(ctx, `RESET lock_timeout`)
	}
	t.Cleanup(func() { eraseKeysRead = nil })
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if _, err := app.EraseUserData(ctx, "u1"); err != nil {
		t.Fatalf("EraseUserData: %v", err)
	}
	switch {
	case swapErr != nil && strings.Contains(swapErr.Error(), "lock timeout"):
		// The erasure held the row: the swap could not land in it.
	case swapErr != nil:
		t.Fatalf("swap: %v", swapErr)
	case store.present["av/new.png"]:
		t.Error("the avatar swapped in before the delete survived the erasure")
	}
	if store.present["av/old.png"] {
		t.Error("the avatar read before the delete survived the erasure")
	}
}
