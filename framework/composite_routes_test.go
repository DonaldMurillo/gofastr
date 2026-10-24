package framework

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
)

// compositeApp serves the composite-keyed enrollments over HTTP.
func compositeApp(t *testing.T, db *sql.DB) *App {
	t.Helper()
	if err := AutoMigrate(db, enrollmentRegistry(t, true)); err != nil {
		t.Fatalf("AutoMigrate: %v", err)
	}
	app := NewApp(WithDB(db), WithoutDefaultMiddleware())
	app.Use(stubAuthMiddleware)
	app.Entity("enrollment", enrollment(true).Config)
	return app
}

func serveKeyed(t *testing.T, app *App, method, path string, body any) *httptest.ResponseRecorder {
	t.Helper()
	var buf bytes.Buffer
	if body != nil {
		if err := json.NewEncoder(&buf).Encode(body); err != nil {
			t.Fatal(err)
		}
	}
	req := httptest.NewRequest(method, path, &buf)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	app.Router().ServeHTTP(rec, req)
	return rec
}

// A composite-keyed entity names a record with one path segment per key
// column, and a value holding a slash travels escaped in its segment.
func TestCompositeKeyRoutes(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *sql.DB, _ Dialect) {
		app := compositeApp(t, db)
		rec := serveKeyed(t, app, "POST", "/enrollments", map[string]any{"student_id": "s/1", "course_id": "c9", "grade": "A"})
		if rec.Code != http.StatusCreated {
			t.Fatalf("POST = %d %s", rec.Code, rec.Body)
		}
		serveKeyed(t, app, "POST", "/enrollments", map[string]any{"student_id": "s/1", "course_id": "c8", "grade": "C"})

		grade := func(path string) (int, string) {
			rec := serveKeyed(t, app, "GET", path, nil)
			var env struct{ Data map[string]any }
			_ = json.Unmarshal(rec.Body.Bytes(), &env)
			g, _ := env.Data["grade"].(string)
			return rec.Code, g
		}
		if code, g := grade("/enrollments/s%2F1/c9"); code != http.StatusOK || g != "A" {
			t.Fatalf("GET = %d grade %q, want 200 A", code, g)
		}
		if rec := serveKeyed(t, app, "PATCH", "/enrollments/s%2F1/c9", map[string]any{"grade": "B"}); rec.Code != http.StatusOK {
			t.Fatalf("PATCH = %d %s", rec.Code, rec.Body)
		}
		if _, g := grade("/enrollments/s%2F1/c9"); g != "B" {
			t.Fatalf("grade after PATCH = %q, want B", g)
		}
		if _, g := grade("/enrollments/s%2F1/c8"); g != "C" {
			t.Fatalf("PATCH reached the sibling row: grade %q, want C", g)
		}
		if code, _ := grade("/enrollments/s1/c9"); code != http.StatusNotFound {
			t.Fatalf("GET unescaped student = %d, want 404", code)
		}
		if rec := serveKeyed(t, app, "DELETE", "/enrollments/s%2F1/c9", nil); rec.Code >= 300 {
			t.Fatalf("DELETE = %d %s", rec.Code, rec.Body)
		}
		if code, _ := grade("/enrollments/s%2F1/c9"); code != http.StatusNotFound {
			t.Fatalf("GET after DELETE = %d, want 404", code)
		}
		if code, g := grade("/enrollments/s%2F1/c8"); code != http.StatusOK || g != "C" {
			t.Fatalf("DELETE reached the sibling row: %d %q", code, g)
		}
	})
}

// An integer key takes its canonical decimal only: "01" would reach the
// row keyed 1 through either database's coercion, under an id a Decider
// asked about the string never saw.
func TestIntKeyRefusesAliases(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *sql.DB, _ Dialect) {
		cfg := entity.EntityConfig{
			Table: "seats",
			Fields: []schema.Field{
				{Name: "room_no", Type: schema.Int, Required: true},
				{Name: "seat_no", Type: schema.Int, Required: true},
				{Name: "label", Type: schema.String},
			},
			PrimaryKey: []string{"room_no", "seat_no"},
		}.WithTimestamps(false)
		reg := NewRegistry()
		if err := reg.Register(entity.Define("seat", cfg)); err != nil {
			t.Fatal(err)
		}
		if err := AutoMigrate(db, reg); err != nil {
			t.Fatalf("AutoMigrate: %v", err)
		}
		app := NewApp(WithDB(db), WithoutDefaultMiddleware())
		app.Use(stubAuthMiddleware)
		app.Entity("seat", cfg)
		if rec := serveKeyed(t, app, "POST", "/seats", map[string]any{"room_no": 1, "seat_no": 2, "label": "kept"}); rec.Code != http.StatusCreated {
			t.Fatalf("POST = %d %s", rec.Code, rec.Body)
		}
		if rec := serveKeyed(t, app, "GET", "/seats/1/2", nil); rec.Code != http.StatusOK {
			t.Fatalf("GET 1/2 = %d %s", rec.Code, rec.Body)
		}
		for _, path := range []string{"/seats/01/2", "/seats/1/+2", "/seats/1/2.0"} {
			if rec := serveKeyed(t, app, "GET", path, nil); rec.Code != http.StatusNotFound {
				t.Errorf("GET %s = %d, want 404", path, rec.Code)
			}
			if rec := serveKeyed(t, app, "PATCH", path, map[string]any{"label": "aliased"}); rec.Code != http.StatusNotFound {
				t.Errorf("PATCH %s = %d, want 404", path, rec.Code)
			}
		}
		serveKeyed(t, app, "PATCH", "/seats/_batch", map[string]any{"items": []any{map[string]any{"roomNo": "01", "seatNo": 2, "label": "aliased"}}})
		var label string
		if err := db.QueryRow(`SELECT label FROM seats WHERE room_no = 1 AND seat_no = 2`).Scan(&label); err != nil {
			t.Fatal(err)
		}
		if label != "kept" {
			t.Errorf("an aliased key wrote the row keyed 1/2: label = %q", label)
		}
	})
}

// A key column whose type the database chose compares a second spelling
// equal: a Postgres UUID matches either case, as does a SQLite NOCASE
// column. A record id still names one spelling, the row's own.
func TestUnmanagedKeyRefusesAliases(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *sql.DB, dialect Dialect) {
		ddl := `CREATE TABLE claims (room TEXT COLLATE NOCASE, slot TEXT, label TEXT, PRIMARY KEY (room, slot))`
		if dialect == DialectPostgres {
			ddl = `CREATE TABLE claims (room UUID, slot TEXT, label TEXT, PRIMARY KEY (room, slot))`
		}
		if _, err := db.Exec(ddl); err != nil {
			t.Fatal(err)
		}
		const room = "550e8400-e29b-41d4-a716-446655440000"
		if _, err := db.Exec(`INSERT INTO claims (room, slot, label) VALUES ($1, 'x', 'kept')`, room); err != nil {
			t.Fatal(err)
		}
		cfg := entity.EntityConfig{
			Table: "claims",
			Fields: []schema.Field{
				{Name: "room", Type: schema.UUID, Required: true},
				{Name: "slot", Type: schema.String, Required: true},
				{Name: "label", Type: schema.String},
			},
			PrimaryKey: []string{"room", "slot"},
			Unmanaged:  true,
		}.WithTimestamps(false)
		app := NewApp(WithDB(db), WithoutDefaultMiddleware())
		app.Use(stubAuthMiddleware)
		app.Entity("claim", cfg)
		if rec := serveKeyed(t, app, "GET", "/claims/"+room+"/x", nil); rec.Code != http.StatusOK {
			t.Fatalf("GET the row's own id = %d %s", rec.Code, rec.Body)
		}
		alias := "/claims/" + strings.ToUpper(room) + "/x"
		if rec := serveKeyed(t, app, "GET", alias, nil); rec.Code != http.StatusNotFound {
			t.Errorf("GET %s = %d, want 404", alias, rec.Code)
		}
		if rec := serveKeyed(t, app, "PATCH", alias, map[string]any{"label": "aliased"}); rec.Code != http.StatusNotFound {
			t.Errorf("PATCH %s = %d, want 404", alias, rec.Code)
		}
		var label string
		if err := db.QueryRow(`SELECT label FROM claims`).Scan(&label); err != nil {
			t.Fatal(err)
		}
		if label != "kept" {
			t.Errorf("an aliased id wrote the row: label = %q", label)
		}
	})
}

// An Unmanaged key column's own spelling is the one the database stores:
// a Postgres CHAR(n) pads it, and its id carries the padding. A spelling
// equality accepts beside it (the unpadded value, another case under a
// nondeterministic collation) names no record.
func TestUnmanagedKeyOwnSpelling(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *sql.DB, dialect Dialect) {
		type keyCase struct{ name, column, alias string }
		cases := []keyCase{{"char", "CHAR(5)", ""}, {"nocase", "TEXT COLLATE NOCASE", "AB"}}
		if dialect == DialectPostgres {
			if _, err := db.Exec(`CREATE COLLATION IF NOT EXISTS ci_key (provider = icu, locale = 'und-u-ks-level2', deterministic = false)`); err != nil {
				t.Fatal(err)
			}
			cases = []keyCase{{"char", "CHAR(5)", "ab"}, {"collation", "TEXT COLLATE ci_key", "AB"}}
		}
		for _, c := range cases {
			t.Run(c.name, func(t *testing.T) {
				table := "codes_" + c.name
				ddl := `CREATE TABLE ` + table + ` (code ` + c.column + `, scope TEXT, label TEXT, PRIMARY KEY (code, scope))`
				if _, err := db.Exec(ddl); err != nil {
					t.Fatal(err)
				}
				if _, err := db.Exec(`INSERT INTO ` + table + ` (code, scope, label) VALUES ('ab', 's', 'kept')`); err != nil {
					t.Fatal(err)
				}
				var stored string
				if err := db.QueryRow(`SELECT code FROM ` + table).Scan(&stored); err != nil {
					t.Fatal(err)
				}
				app := NewApp(WithDB(db), WithoutDefaultMiddleware())
				app.Use(stubAuthMiddleware)
				app.Entity("code_"+c.name, entity.EntityConfig{
					Table: table,
					Fields: []schema.Field{
						{Name: "code", Type: schema.String, Required: true},
						{Name: "scope", Type: schema.String, Required: true},
						{Name: "label", Type: schema.String},
					},
					PrimaryKey: []string{"code", "scope"},
					Unmanaged:  true,
				}.WithTimestamps(false))
				own := "/" + table + "/" + entity.JoinKey([]string{stored, "s"})
				if rec := serveKeyed(t, app, "GET", own, nil); rec.Code != http.StatusOK {
					t.Fatalf("GET the row's own id %s = %d %s", own, rec.Code, rec.Body)
				}
				if c.alias == "" {
					return
				}
				alias := "/" + table + "/" + entity.JoinKey([]string{c.alias, "s"})
				if rec := serveKeyed(t, app, "GET", alias, nil); rec.Code != http.StatusNotFound {
					t.Errorf("GET %s = %d, want 404", alias, rec.Code)
				}
				if rec := serveKeyed(t, app, "PATCH", alias, map[string]any{"label": "aliased"}); rec.Code != http.StatusNotFound {
					t.Errorf("PATCH %s = %d, want 404", alias, rec.Code)
				}
				if rec := serveKeyed(t, app, "DELETE", alias, nil); rec.Code != http.StatusNotFound {
					t.Errorf("DELETE %s = %d, want 404", alias, rec.Code)
				}
				if rec := serveKeyed(t, app, "PATCH", own, map[string]any{"label": "owned"}); rec.Code != http.StatusOK {
					t.Errorf("PATCH the row's own id = %d %s", rec.Code, rec.Body)
				}
			})
		}
	})
}

// A move on an entity whose key holds a column named "key" reads the
// move from its own wildcard, not from the key column's.
func TestCompositeKeyTransition(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *sql.DB, _ Dialect) {
		cfg := entity.EntityConfig{
			Table: "settings",
			Fields: []schema.Field{
				{Name: "scope", Type: schema.String, Required: true},
				{Name: "key", Type: schema.String, Required: true},
				{Name: "status", Type: schema.Enum, Default: "draft", Values: []string{"draft", "live"}},
			},
			PrimaryKey: []string{"scope", "key"},
			States: &entity.StatesConfig{
				Field:       "status",
				Initial:     []string{"draft"},
				Transitions: []entity.Transition{{Key: "publish", From: []string{"draft"}, To: "live"}},
			},
		}.WithTimestamps(false)
		reg := NewRegistry()
		if err := reg.Register(entity.Define("setting", cfg)); err != nil {
			t.Fatal(err)
		}
		if err := AutoMigrate(db, reg); err != nil {
			t.Fatalf("AutoMigrate: %v", err)
		}
		app := NewApp(WithDB(db), WithoutDefaultMiddleware())
		app.Use(stubAuthMiddleware)
		app.Entity("setting", cfg)

		if rec := serveKeyed(t, app, "POST", "/settings", map[string]any{"scope": "site", "key": "theme", "status": "draft"}); rec.Code != http.StatusCreated {
			t.Fatalf("POST = %d %s", rec.Code, rec.Body)
		}
		if rec := serveKeyed(t, app, "POST", "/settings/site/theme/transitions/publish", nil); rec.Code != http.StatusOK {
			t.Fatalf("publish = %d %s", rec.Code, rec.Body)
		}
		rec := serveKeyed(t, app, "GET", "/settings/site/theme", nil)
		var env struct{ Data map[string]any }
		_ = json.Unmarshal(rec.Body.Bytes(), &env)
		if env.Data["status"] != "live" {
			t.Fatalf("status = %v, want live (%s)", env.Data["status"], rec.Body)
		}
	})
}
