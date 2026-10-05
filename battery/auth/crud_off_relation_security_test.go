package auth_test

import (
	"database/sql"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/battery/auth"
	"github.com/DonaldMurillo/gofastr/core/handler"
	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	_ "github.com/DonaldMurillo/gofastr/sqlite/stdlib"
)

func relScopeDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite3", filepath.Join(t.TempDir(), "relscope.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func relScopeDo(app *framework.App, method, path, body string, user any, bearer string) (int, string) {
	var rd io.Reader
	if body != "" {
		rd = strings.NewReader(body)
	}
	req := httptest.NewRequest(method, path, rd)
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	if user != nil {
		req = req.WithContext(handler.SetUser(req.Context(), user))
	}
	rec := httptest.NewRecorder()
	app.Router().ServeHTTP(rec, req)
	return rec.Code, strings.TrimSpace(rec.Body.String())
}

// auth.UserEntityConfig is CRUD:false, so no generated surface may reach
// user rows: not their own routes, and not another entity's ?include= or
// ?rel.field= filter.
func TestCRUDOffTargetRefusedOnRelations(t *testing.T) {
	cfg := auth.UserEntityConfig()
	if cfg.Exposure == nil || cfg.Exposure.CRUD == nil || *cfg.Exposure.CRUD {
		t.Fatalf("auth.UserEntityConfig is not CRUD:false: %+v", cfg.Exposure)
	}
	db := relScopeDB(t)
	app := framework.NewApp(framework.WithDB(db), framework.WithoutDefaultMiddleware())
	app.Entity("users", cfg)
	app.Entity("notes", entity.EntityConfig{
		Fields: []schema.Field{
			{Name: "owner_id", Type: schema.String},
			{Name: "title", Type: schema.String},
			{Name: "author_id", Type: schema.String},
		},
		Scope:     &entity.ScopeConfig{OwnerField: "owner_id"},
		Relations: []entity.Relation{entity.BelongsTo("author", "users", "author_id")},
	}.WithTimestamps(false))
	if err := framework.AutoMigrate(db, app.Registry); err != nil {
		t.Fatalf("automigrate: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO users (id, email, password_hash, roles, password_set, created_at, updated_at) VALUES ('u-alice','alice@example.test','HASH','["admin"]',1,CURRENT_TIMESTAMP,CURRENT_TIMESTAMP)`); err != nil {
		t.Fatal(err)
	}
	bob := &auth.BasicUser{ID: "u-bob", Email: "bob@example.test", Roles: []string{"user"}}
	if code, body := relScopeDo(app, "POST", "/notes", `{"title":"n","authorId":"u-alice"}`, bob, ""); code != http.StatusCreated {
		t.Fatalf("POST /notes = %d %s", code, body)
	}
	for _, p := range []string{"/notes?include=author", "/notes?author.email_like=alice", "/notes?author.email_like=zzz"} {
		code, body := relScopeDo(app, "GET", p, "", bob, "")
		if code != http.StatusForbidden || strings.Contains(body, "alice@example.test") {
			t.Errorf("SECURITY: GET %s = %d %s, want 403", p, code, body)
		}
	}
}

// A cascade write is a generated surface too: a parent route must not create
// rows in a CRUD:false child, nor link a CRUD:false row through ManyToMany.
func TestCRUDOffTargetRefusedOnCascade(t *testing.T) {
	db := relScopeDB(t)
	app := framework.NewApp(framework.WithDB(db), framework.WithoutDefaultMiddleware())
	app.Entity("users", auth.UserEntityConfig())
	off := false
	app.Entity("secrets", entity.EntityConfig{
		Fields: []schema.Field{
			{Name: "project_id", Type: schema.String},
			{Name: "value", Type: schema.String},
		},
		Exposure: &entity.ExposureConfig{CRUD: &off},
	}.WithTimestamps(false))
	secrets := entity.HasMany("secrets", "secrets", "project_id")
	secrets.CascadeWrite = true
	members := entity.ManyToMany("members", "users", "project_members", "project_id", "user_id")
	members.CascadeWrite = true
	app.Entity("projects", entity.EntityConfig{
		Fields:    []schema.Field{{Name: "name", Type: schema.String}},
		Relations: []entity.Relation{secrets, members},
	}.WithTimestamps(false))
	if err := framework.AutoMigrate(db, app.Registry); err != nil {
		t.Fatalf("automigrate: %v", err)
	}
	mustExecRel(t, db, `INSERT INTO users (id, email, password_hash, roles, password_set, created_at, updated_at) VALUES ('u-alice','alice@example.test','HASH','["admin"]',1,CURRENT_TIMESTAMP,CURRENT_TIMESTAMP)`)
	bob := &auth.BasicUser{ID: "u-bob", Email: "bob@example.test", Roles: []string{"user"}}
	for _, tc := range []struct{ body, table string }{
		{`{"name":"p1","secrets":[{"value":"CASCADE-SECRET"}]}`, "secrets"},
		{`{"name":"p2","members":["u-alice"]}`, "project_members"},
	} {
		code, body := relScopeDo(app, "POST", "/projects", tc.body, bob, "")
		var n int
		if err := db.QueryRow(`SELECT COUNT(*) FROM ` + tc.table).Scan(&n); err != nil {
			t.Fatal(err)
		}
		if code < 400 || n != 0 {
			t.Errorf("SECURITY: POST /projects %s = %d %s, %d %s rows written", tc.body, code, body, n, tc.table)
		}
	}
}

func mustExecRel(t *testing.T, db *sql.DB, q string) {
	t.Helper()
	if _, err := db.Exec(q); err != nil {
		t.Fatal(err)
	}
}
