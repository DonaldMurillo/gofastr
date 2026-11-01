package framework

import (
	"database/sql"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/datexport"
	"github.com/DonaldMurillo/gofastr/core/migrate"
	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
)

// usersWithProfiles registers a users table a battery delete eraser owns,
// the way battery/auth erases its user row, and a profiles table that
// belongs to it with ON DELETE CASCADE and no owner field.
func usersWithProfiles(t *testing.T, db *sql.DB, store *recordingDeleteStore) *App {
	t.Helper()
	datexport.Reset(t)
	app := NewApp(WithDB(db), WithoutDefaultMiddleware(), WithFileStorage(store))
	app.Entity("users", entity.EntityConfig{
		Table: "users",
		Fields: []schema.Field{
			{Name: "login", Type: schema.String},
			{Name: "avatar", Type: schema.Image},
		},
	}.WithTimestamps(false))
	profile := entity.BelongsTo("user", "users", "user_id")
	profile.OnDelete = entity.OnDeleteCascade
	app.Entity("profiles", entity.EntityConfig{
		Table: "profiles",
		Fields: []schema.Field{
			{Name: "user_id", Type: schema.String},
			{Name: "photo", Type: schema.Image},
		},
		Relations: []entity.Relation{profile},
	}.WithTimestamps(false))
	if err := AutoMigrate(db, app.Registry); err != nil {
		t.Fatalf("AutoMigrate: %v", err)
	}
	app.WithAuditLog(AuditConfig{})
	datexport.RegisterEraser(datexport.DataEraser{
		Name: "users", Source: "test", Table: "users", Column: "login", Mode: datexport.EraseDelete,
	})
	return app
}

func batteryRows(rep EraseReport, name string) int {
	for _, b := range rep.Batteries {
		if b.Name == name {
			return b.RowsAffected
		}
	}
	return -1
}

// A battery delete eraser removed the user row, and the database's ON DELETE
// CASCADE removed the profile with it. The cascade plan was seeded from
// owner-scoped tables alone, so the profile's photo and its audit diff
// survived an erasure that reported success (2026-10-27 S14). Battery delete
// matches now seed the same plan, so the profile is staged, read and
// reported like any cascaded row, and so is the user row itself.
func TestEraseBatteryDeleteSeedsCascade(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *sql.DB, _ Dialect) {
		store := &recordingDeleteStore{present: map[string]bool{
			"av/u1.png": true, "ph/u1.png": true, "ph/u2.png": true,
		}}
		app := usersWithProfiles(t, db, store)
		cascadeExec(t, db, `INSERT INTO users (id, login, avatar) VALUES ('i1','u1','av/u1.png'), ('i2','u2',NULL)`,
			`INSERT INTO profiles (id, user_id, photo) VALUES ('p1','i1','ph/u1.png'), ('p2','i2','ph/u2.png')`,
			`INSERT INTO audit_log (id, entity, op, record_id, actor_id, created_at, diff) VALUES
			('r0','users','create','i1','admin','2024-01-01T00:00:00Z','{"new":{"login":"u1"}}'),
			('r1','profiles','create','p1','admin','2024-01-01T00:00:00Z','{"new":{"photo":"ph/u1.png"}}'),
			('r2','profiles','create','p2','admin','2024-01-01T00:00:00Z','{"new":{"photo":"ph/u2.png"}}')`)

		rep := eraseBoth(t, app, "u1")

		wantIDs(t, db, "users", "i2")
		wantIDs(t, db, "profiles", "p2")
		if store.present["av/u1.png"] || store.present["ph/u1.png"] || !store.present["ph/u2.png"] {
			t.Errorf("objects after erasure = %v, want only ph/u2.png", store.present)
		}
		for id, want := range map[string]string{
			"r0": erasedAuditDiff, "r1": erasedAuditDiff, "r2": `{"new":{"photo":"ph/u2.png"}}`,
		} {
			var diff string
			if err := db.QueryRow(`SELECT diff FROM audit_log WHERE id = $1`, id).Scan(&diff); err != nil {
				t.Fatal(err)
			}
			if diff != want {
				t.Errorf("audit %s diff = %q, want %q", id, diff, want)
			}
		}
		profiles, _ := cascadeResult(rep, "profiles")
		if n := batteryRows(rep, "users"); n != 1 || profiles.RowsAffected != 1 {
			t.Errorf("report: users eraser %d, profiles cascade %d; want 1 and 1 (%+v)", n, profiles.RowsAffected, rep)
		}
	})
}

// SQLite lets a TEXT key hold NULL, and no foreign key can name such a row.
// The seed leaves it to the eraser's own delete, so it is erased and counted
// once instead of being staged where the staged delete cannot match it.
func TestEraseSeedLeavesNullKeyToEraser(t *testing.T) {
	db := openTestDB(t, migrate.DialectSQLite)
	app := usersWithProfiles(t, db, &recordingDeleteStore{present: map[string]bool{}})
	cascadeExec(t, db, `INSERT INTO users (id, login) VALUES ('i1','u1'), (NULL,'u1'), ('i2','u2')`)

	rep := eraseBoth(t, app, "u1")

	wantIDs(t, db, "users", "i2")
	if n := batteryRows(rep, "users"); n != 2 {
		t.Errorf("users eraser rows = %d, want 2 (%+v)", n, rep)
	}
}
