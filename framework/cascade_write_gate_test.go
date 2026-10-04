package framework

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/core/handler"
	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/access"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/framework/event"
	"github.com/DonaldMurillo/gofastr/framework/hook"
	"github.com/DonaldMurillo/gofastr/framework/owner"
)

// gateCascadeApp: a public parent whose cascade relations point at a
// default-gated entity (no owner, no Access, not Public), whose own route
// answers 401 to an anonymous caller.
func gateCascadeApp(t *testing.T, db *sql.DB) *App {
	t.Helper()
	stmts := []string{
		`CREATE TABLE guestbooks (id TEXT PRIMARY KEY, title TEXT NOT NULL, note_id TEXT)`,
		`CREATE TABLE gated_notes (id TEXT PRIMARY KEY, guestbook_id TEXT, body TEXT NOT NULL)`,
		`CREATE TABLE guestbook_notes (guestbook_id TEXT NOT NULL, note_id TEXT NOT NULL, PRIMARY KEY(guestbook_id, note_id))`,
	}
	for _, s := range stmts {
		if _, err := db.Exec(s); err != nil {
			t.Fatalf("setup %q: %v", s, err)
		}
	}
	app := NewApp(WithDB(db), WithoutDefaultMiddleware())
	app.Entity("guestbooks", entity.EntityConfig{
		Table: "guestbooks",
		Fields: []schema.Field{
			{Name: "title", Type: schema.String, Required: true},
			{Name: "note_id", Type: schema.String},
		},
		Relations: []entity.Relation{
			entity.HasMany("notes", "gated_notes", "guestbook_id").WithCascadeWrite(true),
			entity.BelongsTo("pinned", "gated_notes", "note_id").WithCascadeWrite(true),
			entity.ManyToMany("linked", "gated_notes", "guestbook_notes", "guestbook_id", "note_id").WithCascadeWrite(true),
		},
		Exposure: &entity.ExposureConfig{Public: true},
	}.WithTimestamps(false))
	app.Entity("gated_notes", entity.EntityConfig{
		Table: "gated_notes",
		Fields: []schema.Field{
			{Name: "guestbook_id", Type: schema.String},
			{Name: "body", Type: schema.String, Required: true},
		},
	}.WithTimestamps(false))
	return app
}

// TestCascadeWriteHonorsSessionGate: an anonymous write to a public parent
// cannot create rows in a session-gated entity through any cascade relation.
func TestCascadeWriteHonorsSessionGate(t *testing.T) {
	cases := map[string]map[string]any{
		"has-many":     {"title": "t", "notes": []any{map[string]any{"body": "x"}}},
		"belongs-to":   {"title": "t", "pinned": map[string]any{"body": "x"}},
		"many-to-many": {"title": "t", "linked": []any{map[string]any{"body": "x"}}},
	}
	forEachDialect(t, func(t *testing.T, db *sql.DB, _ Dialect) {
		app := gateCascadeApp(t, db)
		anon := TestHarness(t, app)
		anon.Post("/gated_notes", map[string]any{"body": "direct"}).AssertStatus(t, http.StatusUnauthorized)
		for name, payload := range cases {
			if got := anon.Post("/guestbooks", payload).Status(); got < 400 {
				t.Errorf("%s: anonymous cascade answered %d, want a refusal", name, got)
			}
		}
		var n int
		if err := db.QueryRow("SELECT COUNT(*) FROM gated_notes").Scan(&n); err != nil {
			t.Fatal(err)
		}
		if n != 0 {
			t.Fatalf("anonymous cascades wrote %d gated rows, want 0", n)
		}

		user := TestHarness(t, app).AsUser(struct{ ID string }{ID: "u1"})
		for name, payload := range cases {
			if got := user.Post("/guestbooks", payload).Status(); got != http.StatusCreated {
				t.Errorf("%s: signed-in cascade answered %d, want 201", name, got)
			}
		}
	})
}

// TestCascadeRollbackEmitsNoChildEvent: a cascade that rolls back on its
// second child publishes no created event for the first one.
func TestCascadeRollbackEmitsNoChildEvent(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *sql.DB, _ Dialect) {
		seedCascadeDB(t, db)
		app := cascadeTestApp(t, db)
		var mu sync.Mutex
		items := 0
		cancel := app.Events().Subscribe(event.EntityCreated, func(_ context.Context, e event.Event) error {
			if d, ok := e.Data.(map[string]any); ok && d["entity"] == "order_items" {
				mu.Lock()
				items++
				mu.Unlock()
			}
			return nil
		})
		defer cancel()
		ta := TestHarness(t, app).AsUser(struct{ ID string }{ID: "u1"})

		ta.Post("/orders", map[string]any{
			"customer_name": "Bob",
			"items":         []any{map[string]any{"name": "ok", "price": 1}, map[string]any{"price": 2}},
		}).AssertStatus(t, http.StatusBadRequest)
		// Barrier: one committed child, so the counter has something to
		// wait for; a leaked rollback event would make it 2.
		ta.Post("/orders", map[string]any{
			"customer_name": "Ann",
			"items":         []any{map[string]any{"name": "ok", "price": 1}},
		}).AssertStatus(t, http.StatusCreated)

		deadline := time.Now().Add(2 * time.Second)
		for {
			mu.Lock()
			got := items
			mu.Unlock()
			if got >= 1 || time.Now().After(deadline) {
				break
			}
			time.Sleep(10 * time.Millisecond)
		}
		time.Sleep(100 * time.Millisecond)
		mu.Lock()
		defer mu.Unlock()
		if items != 1 {
			t.Fatalf("order_items created events = %d, want 1 (the committed child only)", items)
		}
	})
}

// TestCascadeOnlyUpdateUsesWriteScope: a PUT that carries only cascade
// relations must find its parent under the owner WRITE scope. A caller
// holding CrossOwnerRead can read another owner's deck, and must still not
// be able to link cards to it.
func TestCascadeOnlyUpdateUsesWriteScope(t *testing.T) {
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Skip("sqlite3 driver not available")
	}
	t.Cleanup(func() { db.Close() })

	app := NewApp(WithConfig(AppConfig{Name: "xowrite", APIPrefix: "/api"}), WithDB(db))
	app.Entity("decks", EntityConfig{
		Fields: []schema.Field{
			{Name: "title", Type: schema.String},
			{Name: "owner_id", Type: schema.String},
		},
		Relations: []entity.Relation{
			entity.ManyToMany("cards", "cards", "deck_cards", "deck_id", "card_id").WithCascadeWrite(true),
		},
		Scope:    &entity.ScopeConfig{OwnerField: "owner_id", CrossOwnerRead: "decks:read:all"},
		Exposure: &entity.ExposureConfig{CRUD: new(true)},
	})
	app.Entity("cards", EntityConfig{
		Fields:   []schema.Field{{Name: "name", Type: schema.String}},
		Exposure: &entity.ExposureConfig{CRUD: new(true), Public: true},
	})
	policy := access.NewRolePolicy()
	policy.Grant("auditor", "decks:read:all")
	app.Use(access.Middleware(policy, func(ctx context.Context) []string {
		if u, ok := handler.GetUser(ctx); ok && u != nil {
			if rh, ok := u.(interface{ GetRoles() []string }); ok {
				return rh.GetRoles()
			}
		}
		return nil
	}))
	stop := covStartAndStop(t, app)
	defer stop()
	for _, s := range []string{
		`INSERT INTO decks (id, title, owner_id) VALUES ('d1','theirs','u-other')`,
		`INSERT INTO cards (id, name) VALUES ('c1','card')`,
	} {
		if _, err := app.DB.Exec(s); err != nil {
			t.Fatal(err)
		}
	}
	prev := owner.GetExtractor()
	owner.SetExtractor(func(ctx context.Context) (any, bool) {
		if u, ok := handler.GetUser(ctx); ok && u != nil {
			if idh, ok := u.(interface{ GetID() string }); ok {
				return idh.GetID(), true
			}
		}
		return nil, false
	})
	t.Cleanup(func() { owner.SetExtractor(prev) })

	auditor := TestHarness(t, app).AsUser(xoUser{id: "u-self", roles: []string{"auditor"}})
	if got := auditor.Get("/api/decks/d1").Status(); got != http.StatusOK {
		t.Fatalf("the auditor cannot read d1 (%d): the CrossOwnerRead grant is not wired, so the test proves nothing", got)
	}
	if got := auditor.Put("/api/decks/d1", map[string]any{"cards": []any{"c1"}}).Status(); got != http.StatusNotFound {
		t.Errorf("cascade-only PUT on another owner's deck answered %d, want 404", got)
	}
	var n int
	if err := app.DB.QueryRow(`SELECT COUNT(*) FROM deck_cards`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("a cross-owner reader linked %d cards to another owner's deck, want 0", n)
	}
}

// TestCascadeChildRedactionIsResponseOnly: a child's read hook redacts the
// HTTP response only. The parent's AfterCreate hook, which runs inside the
// write, still sees the stored child values.
func TestCascadeChildRedactionIsResponseOnly(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *sql.DB, _ Dialect) {
		seedCascadeDB(t, db)
		app := cascadeTestApp(t, db)
		app.HookRegistry("profiles").RegisterHook(hook.AfterGet, func(ctx context.Context, data any) error {
			if p, ok := data.(*hook.GetPayload); ok {
				p.Result["bio"] = "[redacted]"
			}
			return nil
		})
		var seen any
		app.HookRegistry("users").RegisterHook(hook.AfterCreate, func(ctx context.Context, data any) error {
			if row, ok := data.(map[string]any); ok {
				if prof, ok := row["profile"].(map[string]any); ok {
					seen = prof["bio"]
				}
			}
			return nil
		})
		ta := TestHarness(t, app).AsUser(struct{ ID string }{ID: "u1"})
		resp := ta.Post("/users", map[string]any{"name": "S", "profile": map[string]any{"bio": "stored bio"}})
		resp.AssertStatus(t, http.StatusCreated)
		resp.AssertBodyContains(t, "[redacted]")
		if seen != "stored bio" {
			t.Fatalf("AfterCreate saw profile.bio = %v, want the stored value", seen)
		}
	})
}

// TestBelongsToUnregisteredTargetSkips: a belongs_to FK onto a table the
// registry does not hold (a battery's self-migrated user table) is not
// scope-checked, so the write succeeds as it did before the check existed.
func TestBelongsToUnregisteredTargetSkips(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *sql.DB, _ Dialect) {
		if _, err := db.Exec(`CREATE TABLE posts (id TEXT PRIMARY KEY, title TEXT NOT NULL, author_id TEXT)`); err != nil {
			t.Fatal(err)
		}
		app := NewApp(WithDB(db), WithoutDefaultMiddleware())
		app.Entity("posts", entity.EntityConfig{
			Table: "posts",
			Fields: []schema.Field{
				{Name: "title", Type: schema.String, Required: true},
				{Name: "author_id", Type: schema.String},
			},
			Relations: []entity.Relation{entity.BelongsTo("author", "auth_users", "author_id")},
		}.WithTimestamps(false))
		ta := TestHarness(t, app).AsUser(struct{ ID string }{ID: "u1"})
		resp := ta.Post("/posts", map[string]any{"title": "t", "author_id": "u1"})
		resp.AssertStatus(t, http.StatusCreated)
	})
}

// TestCascadeParentEventOmitsChildren: the parent's created/updated event
// carries the parent row only. The cascade-written child rides its own
// event, gated and redacted as its own entity; a copy inside the parent's
// payload would reach a parent subscriber past the child's read gate.
func TestCascadeParentEventOmitsChildren(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *sql.DB, _ Dialect) {
		seedCascadeDB(t, db)
		app := cascadeTestApp(t, db)
		var mu sync.Mutex
		var userRecords []map[string]any
		profiles := 0
		capture := func(_ context.Context, e event.Event) error {
			d, ok := e.Data.(map[string]any)
			if !ok {
				return nil
			}
			mu.Lock()
			defer mu.Unlock()
			switch d["entity"] {
			case "users":
				rec, _ := d["record"].(map[string]any)
				userRecords = append(userRecords, rec)
			case "profiles":
				profiles++
			}
			return nil
		}
		defer app.Events().Subscribe(event.EntityCreated, capture)()
		defer app.Events().Subscribe(event.EntityUpdated, capture)()
		ta := TestHarness(t, app).AsUser(struct{ ID string }{ID: "u1"})

		resp := ta.Post("/users", map[string]any{"name": "Ann", "profile": map[string]any{"bio": "private"}})
		resp.AssertStatus(t, http.StatusCreated)
		var created struct {
			Data map[string]any `json:"data"`
		}
		if err := json.Unmarshal([]byte(resp.Body()), &created); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if _, ok := created.Data["profile"].(map[string]any); !ok {
			t.Fatalf("response lost the cascade child: %v", created.Data)
		}
		id, _ := created.Data["id"].(string)
		ta.Put("/users/"+id, map[string]any{"name": "Ann", "profile": map[string]any{"bio": "still private"}}).
			AssertStatus(t, http.StatusOK)

		deadline := time.Now().Add(2 * time.Second)
		for {
			mu.Lock()
			done := len(userRecords) >= 2 && profiles >= 2
			mu.Unlock()
			if done || time.Now().After(deadline) {
				break
			}
			time.Sleep(10 * time.Millisecond)
		}
		mu.Lock()
		defer mu.Unlock()
		if len(userRecords) != 2 || profiles != 2 {
			t.Fatalf("got %d users events and %d profiles events, want 2 and 2", len(userRecords), profiles)
		}
		for i, rec := range userRecords {
			if _, leaked := rec["profile"]; leaked {
				t.Errorf("users event %d carries the cascade child: %v", i, rec)
			}
		}
	})
}
