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
	return usersWithProfilesAt(t, db, store, "users")
}

// usersWithProfilesAt is usersWithProfiles with the eraser naming its table
// as eraserTable.
func usersWithProfilesAt(t *testing.T, db *sql.DB, store *recordingDeleteStore, eraserTable string) *App {
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
		Name: "users", Source: "test", Table: eraserTable, Column: "login", Mode: datexport.EraseDelete,
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

// The entity names its table users and the battery eraser public.users
// (main.users on SQLite; here the test's own schema): one table, but
// matched by spelling the eraser seeded nothing, and the profile its delete
// cascaded to kept its photo and audit diff (2026-10-31 review). The
// eraser's table is matched by the table the catalog resolves it to, on
// both dialects.
func TestEraseSeedMatchesSchemaQualified(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *sql.DB, dialect Dialect) {
		eraseSeedQualified(t, db, dialect)
	})
}

func eraseSeedQualified(t *testing.T, db *sql.DB, dialect Dialect) {
	schemaName := "main"
	if dialect == DialectPostgres {
		if err := db.QueryRow(`SELECT current_schema()`).Scan(&schemaName); err != nil {
			t.Fatal(err)
		}
	}
	store := &recordingDeleteStore{present: map[string]bool{"av/u1.png": true, "ph/u1.png": true}}
	app := usersWithProfilesAt(t, db, store, schemaName+".users")
	cascadeExec(t, db, `INSERT INTO users (id, login, avatar) VALUES ('i1','u1','av/u1.png'), ('i2','u2',NULL)`,
		`INSERT INTO profiles (id, user_id, photo) VALUES ('p1','i1','ph/u1.png'), ('p2','i2',NULL)`,
		`INSERT INTO audit_log (id, entity, op, record_id, actor_id, created_at, diff) VALUES
		('r0','users','create','i1','admin','2024-01-01T00:00:00Z','{"new":{"login":"u1"}}'),
		('r1','profiles','create','p1','admin','2024-01-01T00:00:00Z','{"new":{"photo":"ph/u1.png"}}')`)

	rep := eraseBoth(t, app, "u1")

	wantIDs(t, db, "users", "i2")
	wantIDs(t, db, "profiles", "p2")
	if store.present["av/u1.png"] || store.present["ph/u1.png"] {
		t.Errorf("objects after erasure = %v, want none of u1's", store.present)
	}
	for _, id := range []string{"r0", "r1"} {
		var diff string
		if err := db.QueryRow(`SELECT diff FROM audit_log WHERE id = $1`, id).Scan(&diff); err != nil {
			t.Fatal(err)
		}
		if diff != erasedAuditDiff {
			t.Errorf("audit %s diff = %q, want it erased", id, diff)
		}
	}
	profiles, _ := cascadeResult(rep, "profiles")
	if n := batteryRows(rep, "users"); n != 1 || profiles.RowsAffected != 1 {
		t.Errorf("report: users eraser %d, profiles cascade %d; want 1 and 1 (%+v)", n, profiles.RowsAffected, rep)
	}
}

// An anonymizer that names its table schema-qualified scrubs a file column
// and an audited row of the entity's table: the battery plane finds the
// table's key, file columns and entities by the same catalog match.
func TestEraseAnonymizerMatchesQualified(t *testing.T) {
	db := openTestDB(t, migrate.DialectPostgres)
	var schemaName string
	if err := db.QueryRow(`SELECT current_schema()`).Scan(&schemaName); err != nil {
		t.Fatal(err)
	}
	datexport.Reset(t)
	store := &recordingDeleteStore{present: map[string]bool{"ph/u1.png": true}}
	app := NewApp(WithDB(db), WithoutDefaultMiddleware(), WithFileStorage(store))
	app.Entity("bios", entity.EntityConfig{
		Table: "bios",
		Fields: []schema.Field{
			{Name: "user_id", Type: schema.String},
			{Name: "photo", Type: schema.Image},
		},
	}.WithTimestamps(false))
	if err := AutoMigrate(db, app.Registry); err != nil {
		t.Fatalf("AutoMigrate: %v", err)
	}
	app.WithAuditLog(AuditConfig{})
	datexport.RegisterEraser(datexport.DataEraser{
		Name: "bios", Source: "test", Table: schemaName + ".bios", Column: "user_id",
		Mode: datexport.EraseAnonymize, ScrubColumns: []string{"photo"},
	})
	cascadeExec(t, db, `INSERT INTO bios (id, user_id, photo) VALUES ('b1','u1','ph/u1.png')`,
		`INSERT INTO audit_log (id, entity, op, record_id, actor_id, created_at, diff) VALUES
		('r1','bios','create','b1','admin','2024-01-01T00:00:00Z','{"new":{"photo":"ph/u1.png"}}')`)

	eraseBoth(t, app, "u1")

	if store.present["ph/u1.png"] {
		t.Error("the scrubbed photo's object survived the erasure")
	}
	var diff string
	if err := db.QueryRow(`SELECT diff FROM audit_log WHERE id = 'r1'`).Scan(&diff); err != nil {
		t.Fatal(err)
	}
	if diff != erasedAuditDiff {
		t.Errorf("audit diff = %q, want it erased", diff)
	}
}

// On Postgres a quoted "Users" a battery made is a table apart from the
// entity's users. The eraser on "Users" matched the entity's table, by its
// lowercased name or by a lowercased catalog id, and seeded deletes there:
// the erasure removed an unrelated users row and the profile it cascaded to
// (2026-10-31 review). The entity's name resolves as its unquoted DDL folds
// it and the eraser's as its own table is spelled, and the two compare with
// their case kept.
func TestEraseKeepsCaseKeptTableApart(t *testing.T) {
	db := openTestDB(t, migrate.DialectPostgres)
	var schemaName string
	if err := db.QueryRow(`SELECT current_schema()`).Scan(&schemaName); err != nil {
		t.Fatal(err)
	}
	for _, spelling := range []string{"Users", schemaName + ".Users"} {
		t.Run(spelling, func(t *testing.T) {
			cascadeExec(t, db, `DROP TABLE IF EXISTS profiles`, `DROP TABLE IF EXISTS users`,
				`DROP TABLE IF EXISTS audit_log`, `DROP TABLE IF EXISTS "Users"`,
				`CREATE TABLE "Users" (id TEXT PRIMARY KEY, login TEXT)`)
			store := &recordingDeleteStore{present: map[string]bool{"ph/u1.png": true}}
			app := usersWithProfilesAt(t, db, store, spelling)
			cascadeExec(t, db, `INSERT INTO "Users" (id, login) VALUES ('x1','u1')`,
				`INSERT INTO users (id, login) VALUES ('i1','u1')`,
				`INSERT INTO profiles (id, user_id, photo) VALUES ('p1','i1','ph/u1.png')`)

			eraseBoth(t, app, "u1")

			var left int
			if err := db.QueryRow(`SELECT COUNT(*) FROM "Users"`).Scan(&left); err != nil {
				t.Fatal(err)
			}
			if left != 0 {
				t.Errorf(`"Users" rows after erasure = %d, want 0`, left)
			}
			wantIDs(t, db, "users", "i1")
			wantIDs(t, db, "profiles", "p1")
			if !store.present["ph/u1.png"] {
				t.Error("the entity profile's photo was deleted by an eraser on another table")
			}
		})
	}
}

// The other half of the same rule: an entity declaring Members has the
// table members, because AutoMigrate's DDL is unquoted and Postgres folds
// it. Resolved with its case kept, the entity's name found a battery's
// quoted "Members" instead, and that battery's eraser seeded deletes from
// the entity's members and the cards they cascade to.
func TestEraseEntityNameResolvesFolded(t *testing.T) {
	db := openTestDB(t, migrate.DialectPostgres)
	datexport.Reset(t)
	app := NewApp(WithDB(db), WithoutDefaultMiddleware(), WithFileStorage(&recordingDeleteStore{present: map[string]bool{}}))
	app.Entity("members", entity.EntityConfig{
		Table:  "Members",
		Fields: []schema.Field{{Name: "login", Type: schema.String}},
	}.WithTimestamps(false))
	card := entity.BelongsTo("member", "members", "member_id")
	card.OnDelete = entity.OnDeleteCascade
	app.Entity("cards", entity.EntityConfig{
		Table:     "cards",
		Fields:    []schema.Field{{Name: "member_id", Type: schema.String}},
		Relations: []entity.Relation{card},
	}.WithTimestamps(false))
	if err := AutoMigrate(db, app.Registry); err != nil {
		t.Fatalf("AutoMigrate: %v", err)
	}
	app.WithAuditLog(AuditConfig{})
	datexport.RegisterEraser(datexport.DataEraser{
		Name: "members", Source: "test", Table: "Members", Column: "login", Mode: datexport.EraseDelete,
	})
	cascadeExec(t, db, `CREATE TABLE "Members" (id TEXT PRIMARY KEY, login TEXT)`,
		`INSERT INTO "Members" (id, login) VALUES ('x1','u1')`,
		`INSERT INTO members (id, login) VALUES ('i1','u1')`,
		`INSERT INTO cards (id, member_id) VALUES ('c1','i1')`)

	eraseBoth(t, app, "u1")

	var left int
	if err := db.QueryRow(`SELECT COUNT(*) FROM "Members"`).Scan(&left); err != nil {
		t.Fatal(err)
	}
	if left != 0 {
		t.Errorf(`"Members" rows after erasure = %d, want 0`, left)
	}
	wantIDs(t, db, "members", "i1")
	wantIDs(t, db, "cards", "c1")
}
