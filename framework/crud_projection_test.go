package framework

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/gofastr/gofastr/core/schema"
)

func projectionApp(t *testing.T, db *sql.DB) *App {
	t.Helper()
	if _, err := db.Exec(`CREATE TABLE posts (
		id TEXT PRIMARY KEY,
		title TEXT NOT NULL,
		body TEXT DEFAULT '',
		secret TEXT DEFAULT ''
	)`); err != nil {
		t.Fatalf("create: %v", err)
	}
	if _, err := db.Exec("INSERT INTO posts(id, title, body, secret) VALUES ($1, $2, $3, $4)", "p1", "hi", "world", "sssh"); err != nil {
		t.Fatalf("seed: %v", err)
	}
	app := NewApp(WithDB(db), WithoutDefaultMiddleware())
	app.Entity("posts", EntityConfig{
		Table: "posts",
		Fields: []schema.Field{
			{Name: "title", Type: schema.String, Required: true},
			{Name: "body", Type: schema.Text},
			{Name: "secret", Type: schema.String},
		},
	}.WithTimestamps(false))
	return app
}

// ============================================================================
// Get with ?fields= returns only requested columns + pk
// ============================================================================

func TestProjection_Get(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *sql.DB, _ Dialect) {
		app := projectionApp(t, db)
		ta := TestHarness(t, app)

		resp := ta.Get("/posts/p1?fields=title")
		resp.AssertStatus(t, http.StatusOK)

		var got map[string]any
		if err := json.Unmarshal([]byte(resp.Body()), &got); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if _, ok := got["id"]; !ok {
			t.Fatalf("expected id always included, got %v", got)
		}
		if got["title"] != "hi" {
			t.Fatalf("expected title=hi, got %v", got["title"])
		}
		if _, present := got["body"]; present {
			t.Fatalf("body should be projected out, got %v", got)
		}
		if _, present := got["secret"]; present {
			t.Fatalf("secret should be projected out, got %v", got)
		}
	})
}

// ============================================================================
// List with ?fields=title,body returns those + pk only
// ============================================================================

func TestProjection_List(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *sql.DB, _ Dialect) {
		app := projectionApp(t, db)
		ta := TestHarness(t, app)

		resp := ta.Get("/posts?fields=title,body")
		resp.AssertStatus(t, http.StatusOK)

		var env ListResponse
		if err := json.Unmarshal([]byte(resp.Body()), &env); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if len(env.Data) != 1 {
			t.Fatalf("expected 1 row, got %d", len(env.Data))
		}
		row := env.Data[0]
		if _, ok := row["id"]; !ok {
			t.Fatalf("expected id, got %v", row)
		}
		if row["title"] != "hi" || row["body"] != "world" {
			t.Fatalf("expected title+body present, got %v", row)
		}
		if _, present := row["secret"]; present {
			t.Fatalf("secret should be projected out")
		}
	})
}

// ============================================================================
// Unknown field name returns 400
// ============================================================================

func TestProjection_UnknownField_400(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *sql.DB, _ Dialect) {
		app := projectionApp(t, db)
		ta := TestHarness(t, app)

		resp := ta.Get("/posts?fields=bogus")
		resp.AssertStatus(t, http.StatusBadRequest).
			AssertBodyContains(t, "bogus")
	})
}
