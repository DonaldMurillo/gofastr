package framework

import (
	"context"
	"database/sql"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/handler"
	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/framework/owner"
)

// Erasure follows foreign keys the way ON DELETE CASCADE would: a row whose
// BelongsTo key points at a row being erased goes too, owner field or not,
// recursively and children first, in the same transaction.

func cascadeExec(t *testing.T, db *sql.DB, stmts ...string) {
	t.Helper()
	for _, s := range stmts {
		if _, err := db.Exec(s); err != nil {
			t.Fatalf("exec %q: %v", s, err)
		}
	}
}

func cascadeIDs(t *testing.T, db *sql.DB, table string) []string {
	t.Helper()
	rows, err := db.Query(`SELECT id FROM ` + table + ` ORDER BY id`)
	if err != nil {
		t.Fatalf("select %s: %v", table, err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			t.Fatal(err)
		}
		out = append(out, id)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return out
}

func wantIDs(t *testing.T, db *sql.DB, table string, want ...string) {
	t.Helper()
	got := cascadeIDs(t, db, table)
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("%s ids = %v, want %v", table, got, want)
	}
}

func cascadeResult(rep EraseReport, table string) (EraseTableResult, bool) {
	for _, r := range rep.Cascaded {
		if r.Table == table {
			return r, true
		}
	}
	return EraseTableResult{}, false
}

// lists is owner-scoped; items belongs to lists with no owner; item_notes
// belongs to items, two hops from the owned row.
func newListsApp(t *testing.T, db *sql.DB) *App {
	t.Helper()
	app := NewApp(WithDB(db))
	app.Registry.Register(entity.Define("lists", entity.EntityConfig{
		Table:  "lists",
		Scope:  &entity.ScopeConfig{OwnerField: "owner_id"},
		Fields: []schema.Field{{Name: "owner_id", Type: schema.String}},
	}.WithTimestamps(false)))
	app.Registry.Register(entity.Define("items", entity.EntityConfig{
		Table:     "items",
		Fields:    []schema.Field{{Name: "list_id", Type: schema.String}},
		Relations: []entity.Relation{entity.BelongsTo("list", "lists", "list_id")},
	}.WithTimestamps(false)))
	app.Registry.Register(entity.Define("item_notes", entity.EntityConfig{
		Table:     "item_notes",
		Fields:    []schema.Field{{Name: "item_id", Type: schema.String}},
		Relations: []entity.Relation{entity.BelongsTo("item", "items", "item_id")},
	}.WithTimestamps(false)))
	if err := AutoMigrate(db, app.Registry); err != nil {
		t.Fatalf("AutoMigrate: %v", err)
	}
	return app
}

func TestEraseDeletesUnownedChildRows(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *sql.DB, _ Dialect) {
		app := newListsApp(t, db)
		cascadeExec(t, db,
			`INSERT INTO lists (id, owner_id) VALUES ('l1','u1'), ('l2','u2')`,
			`INSERT INTO items (id, list_id) VALUES ('i1','l1'), ('i2','l2')`,
			`INSERT INTO item_notes (id, item_id) VALUES ('n1','i1'), ('n2','i2')`,
		)

		rep, err := app.EraseUserData(context.Background(), "u1")
		if err != nil {
			t.Fatalf("EraseUserData: %v", err)
		}
		wantIDs(t, db, "lists", "l2")
		wantIDs(t, db, "items", "i2")
		wantIDs(t, db, "item_notes", "n2")

		for table, want := range map[string]int{"items": 1, "item_notes": 1} {
			r, ok := cascadeResult(rep, table)
			if !ok {
				t.Errorf("report.Cascaded has no %s entry: %+v", table, rep.Cascaded)
				continue
			}
			if r.Source != "cascade" || r.Mode != "delete" || r.RowsAffected != want {
				t.Errorf("Cascaded[%s] = %+v, want Source=cascade Mode=delete Rows=%d", table, r, want)
			}
		}
		if got := rep.TotalErased(); got != 3 {
			t.Errorf("TotalErased = %d, want 3 (list + item + note)", got)
		}

		again, err := app.EraseUserData(context.Background(), "u1")
		if err != nil || again.TotalErased() != 0 {
			t.Errorf("re-erase = %d, %v; want 0, nil", again.TotalErased(), err)
		}
	})
}

func TestEraseDryRunCountsCascade(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *sql.DB, _ Dialect) {
		app := newListsApp(t, db)
		cascadeExec(t, db,
			`INSERT INTO lists (id, owner_id) VALUES ('l1','u1')`,
			`INSERT INTO items (id, list_id) VALUES ('i1','l1'), ('i3','l1')`,
		)
		rep, err := app.EraseUserData(context.Background(), "u1", WithEraseDryRun())
		if err != nil {
			t.Fatalf("dry run: %v", err)
		}
		if r, _ := cascadeResult(rep, "items"); r.RowsAffected != 2 {
			t.Errorf("dry-run Cascaded[items] = %+v, want 2 rows", r)
		}
		wantIDs(t, db, "lists", "l1")
		wantIDs(t, db, "items", "i1", "i3")
	})
}

// Both tables owner-scoped, but the child row is another user's: u2's
// comment on u1's post must go with the post, and u2's comment on u2's
// own post must stay.
func TestEraseDeletesOtherUsersChildRows(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *sql.DB, _ Dialect) {
		app := NewApp(WithDB(db))
		app.Registry.Register(entity.Define("posts", entity.EntityConfig{
			Table:  "posts",
			Scope:  &entity.ScopeConfig{OwnerField: "owner_id"},
			Fields: []schema.Field{{Name: "owner_id", Type: schema.String}},
		}.WithTimestamps(false)))
		app.Registry.Register(entity.Define("comments", entity.EntityConfig{
			Table: "comments",
			Scope: &entity.ScopeConfig{OwnerField: "owner_id"},
			Fields: []schema.Field{
				{Name: "owner_id", Type: schema.String},
				{Name: "post_id", Type: schema.String},
				{Name: "parent_id", Type: schema.String},
			},
			Relations: []entity.Relation{
				entity.BelongsTo("post", "posts", "post_id"),
				entity.BelongsTo("parent", "comments", "parent_id"),
			},
		}.WithTimestamps(false)))
		if err := AutoMigrate(db, app.Registry); err != nil {
			t.Fatalf("AutoMigrate: %v", err)
		}
		cascadeExec(t, db,
			`INSERT INTO posts (id, owner_id) VALUES ('p1','u1'), ('p2','u2')`,
			// c1 u2 on u1's post; c2 u2 on u2's post; c3 u1 on u2's post;
			// c4 u2 replying to c3 on u2's post (reached through parent_id
			// only); c5 u2 on u2's post, untouched.
			`INSERT INTO comments (id, owner_id, post_id, parent_id) VALUES
				('c1','u2','p1',NULL), ('c2','u2','p2',NULL), ('c3','u1','p2',NULL)`,
			`INSERT INTO comments (id, owner_id, post_id, parent_id) VALUES ('c4','u2','p2','c3')`,
		)

		rep, err := app.EraseUserData(context.Background(), "u1")
		if err != nil {
			t.Fatalf("EraseUserData: %v", err)
		}
		wantIDs(t, db, "posts", "p2")
		wantIDs(t, db, "comments", "c2")

		r, ok := cascadeResult(rep, "comments")
		if !ok || r.RowsAffected != 2 {
			t.Errorf("Cascaded[comments] = %+v (found %v), want 2 rows (c1 via post, c4 via parent)", r, ok)
		}
		for _, e := range rep.Entities {
			if e.Table == "comments" && e.RowsAffected != 1 {
				t.Errorf("Entities[comments] = %+v, want 1 owned row (c3)", e)
			}
		}
	})
}

// A composite key is followed column for column.
func TestEraseFollowsCompositeForeignKey(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *sql.DB, _ Dialect) {
		app := NewApp(WithDB(db))
		app.Registry.Register(entity.Define("orders", entity.EntityConfig{
			Table:      "orders",
			PrimaryKey: []string{"store", "no"},
			Scope:      &entity.ScopeConfig{OwnerField: "owner_id"},
			Fields: []schema.Field{
				{Name: "store", Type: schema.String},
				{Name: "no", Type: schema.String},
				{Name: "owner_id", Type: schema.String},
			},
		}.WithTimestamps(false)))
		app.Registry.Register(entity.Define("order_lines", entity.EntityConfig{
			Table: "order_lines",
			Fields: []schema.Field{
				{Name: "order_store", Type: schema.String},
				{Name: "order_no", Type: schema.String},
			},
			Relations: []entity.Relation{entity.BelongsTo("order", "orders", "order_store", "order_no")},
		}.WithTimestamps(false)))
		if err := AutoMigrate(db, app.Registry); err != nil {
			t.Fatalf("AutoMigrate: %v", err)
		}
		cascadeExec(t, db,
			`INSERT INTO orders (store, no, owner_id) VALUES ('s1','1','u1'), ('s1','2','u2'), ('s2','1','u2')`,
			// l2 shares store with u1's order, l3 shares the number: only
			// the pair names the order.
			`INSERT INTO order_lines (id, order_store, order_no) VALUES ('l1','s1','1'), ('l2','s1','2'), ('l3','s2','1')`,
		)
		if _, err := app.EraseUserData(context.Background(), "u1"); err != nil {
			t.Fatalf("EraseUserData: %v", err)
		}
		wantIDs(t, db, "order_lines", "l2", "l3")
	})
}

// A relation the database answers with SET NULL keeps the child row; the
// erasure must not delete it in the database's place.
func TestEraseLeavesSetNullChildren(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *sql.DB, _ Dialect) {
		app := NewApp(WithDB(db))
		app.Registry.Register(entity.Define("lists", entity.EntityConfig{
			Table:  "lists",
			Scope:  &entity.ScopeConfig{OwnerField: "owner_id"},
			Fields: []schema.Field{{Name: "owner_id", Type: schema.String}},
		}.WithTimestamps(false)))
		app.Registry.Register(entity.Define("items", entity.EntityConfig{
			Table:     "items",
			Fields:    []schema.Field{{Name: "list_id", Type: schema.String}},
			Relations: []entity.Relation{entity.BelongsTo("list", "lists", "list_id").OnDeleteAction(entity.OnDeleteSetNull)},
		}.WithTimestamps(false)))
		if err := AutoMigrate(db, app.Registry); err != nil {
			t.Fatalf("AutoMigrate: %v", err)
		}
		cascadeExec(t, db,
			`INSERT INTO lists (id, owner_id) VALUES ('l1','u1')`,
			`INSERT INTO items (id, list_id) VALUES ('i1','l1')`,
		)
		if _, err := app.EraseUserData(context.Background(), "u1"); err != nil {
			t.Fatalf("EraseUserData: %v", err)
		}
		wantIDs(t, db, "items", "i1")
		var list sql.NullString
		if err := db.QueryRow(`SELECT list_id FROM items WHERE id = 'i1'`).Scan(&list); err != nil {
			t.Fatal(err)
		}
		if list.Valid {
			t.Errorf("items.list_id = %q, want NULL", list.String)
		}
	})
}

// newRestrictApp is lists (owner-scoped) and tasks, whose list relation
// declares action: a child row blocks its list's delete. tasks carries its
// own owner, so a user's own task is erased with the user.
func newRestrictApp(t *testing.T, db *sql.DB, action entity.OnDeleteAction) *App {
	t.Helper()
	app := NewApp(WithDB(db))
	app.Registry.Register(entity.Define("lists", entity.EntityConfig{
		Table:  "lists",
		Scope:  &entity.ScopeConfig{OwnerField: "owner_id"},
		Fields: []schema.Field{{Name: "owner_id", Type: schema.String}},
	}.WithTimestamps(false)))
	app.Registry.Register(entity.Define("tasks", entity.EntityConfig{
		Table: "tasks",
		Scope: &entity.ScopeConfig{OwnerField: "owner_id"},
		Fields: []schema.Field{
			{Name: "owner_id", Type: schema.String},
			{Name: "list_id", Type: schema.String},
		},
		Relations: []entity.Relation{entity.BelongsTo("list", "lists", "list_id").OnDeleteAction(action)},
	}.WithTimestamps(false)))
	if err := AutoMigrate(db, app.Registry); err != nil {
		t.Fatalf("AutoMigrate: %v", err)
	}
	return app
}

var restrictActions = []entity.OnDeleteAction{entity.OnDeleteRestrict, entity.OnDeleteNoAction}

// A declared RESTRICT (or NO ACTION) means another user's child row blocks
// the parent's delete. Erasure refuses instead of deleting that row first,
// in a dry run too, and changes nothing.
func TestEraseRefusesRestrictChild(t *testing.T) {
	for _, action := range restrictActions {
		t.Run(string(action), func(t *testing.T) {
			forEachDialect(t, func(t *testing.T, db *sql.DB, _ Dialect) {
				app := newRestrictApp(t, db, action)
				cascadeExec(t, db,
					`INSERT INTO lists (id, owner_id) VALUES ('l1','u1'), ('l2','u1')`,
					`INSERT INTO tasks (id, owner_id, list_id) VALUES ('i1','u2','l1')`,
				)
				for _, opts := range [][]EraseOption{{WithEraseDryRun()}, nil} {
					_, err := app.EraseUserData(context.Background(), "u1", opts...)
					if err == nil {
						t.Fatalf("EraseUserData(%d opts) succeeded, want a refusal naming tasks.list", len(opts))
					}
					for _, want := range []string{"tasks.list", `"tasks"`} {
						if !strings.Contains(err.Error(), want) {
							t.Errorf("error %q does not name %s", err, want)
						}
					}
				}
				wantIDs(t, db, "lists", "l1", "l2")
				wantIDs(t, db, "tasks", "i1")
			})
		})
	}
}

// A RESTRICT relation with nothing to block erases normally, and so does
// one whose only child rows are the user's own, deleted first. tasks sorts
// after lists, so only the RESTRICT edge puts its rows first.
func TestEraseRestrictWithoutBlockers(t *testing.T) {
	for _, action := range restrictActions {
		t.Run(string(action), func(t *testing.T) {
			forEachDialect(t, func(t *testing.T, db *sql.DB, _ Dialect) {
				app := newRestrictApp(t, db, action)
				cascadeExec(t, db,
					`INSERT INTO lists (id, owner_id) VALUES ('l1','u1'), ('l2','u1'), ('l3','u2')`,
					`INSERT INTO tasks (id, owner_id, list_id) VALUES ('i1','u1','l1'), ('i3','u2','l3')`,
				)
				rep, err := app.EraseUserData(context.Background(), "u1")
				if err != nil {
					t.Fatalf("EraseUserData: %v", err)
				}
				wantIDs(t, db, "lists", "l3")
				wantIDs(t, db, "tasks", "i3")
				if got := rep.TotalErased(); got != 3 {
					t.Errorf("TotalErased = %d, want 3 (two lists, one task)", got)
				}
				if len(rep.Cascaded) != 0 {
					t.Errorf("Cascaded = %+v, want none: a RESTRICT edge is not followed", rep.Cascaded)
				}
			})
		})
	}
}

// Pivot rows linking an erased row go with it, and are reported.
func TestEraseDeletesPivotRows(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *sql.DB, _ Dialect) {
		app := newPivotApp(t, db)
		cascadeExec(t, db,
			`INSERT INTO posts (id, owner_id, title) VALUES ('p1','u1','a'), ('p2','u2','b')`,
			`INSERT INTO tags (id, name) VALUES ('t1','go'), ('t2','sql')`,
			`INSERT INTO post_tags (post_id, tag_id) VALUES ('p1','t1'), ('p1','t2'), ('p2','t1')`,
		)
		rep, err := app.EraseUserData(context.Background(), "u1")
		if err != nil {
			t.Fatalf("EraseUserData: %v", err)
		}
		if got := countRows(t, db, "post_tags"); got != 1 {
			t.Errorf("post_tags rows = %d, want 1 (p2's link)", got)
		}
		wantIDs(t, db, "tags", "t1", "t2")
		if r, ok := cascadeResult(rep, "post_tags"); !ok || r.RowsAffected != 2 {
			t.Errorf("Cascaded[post_tags] = %+v (found %v), want 2", r, ok)
		}
	})
}

// newPivotApp: posts (owner-scoped) many_to_many tags through post_tags.
func newPivotApp(t *testing.T, db *sql.DB) *App {
	t.Helper()
	app := NewApp(WithDB(db))
	app.Registry.Register(entity.Define("posts", entity.EntityConfig{
		Table: "posts",
		Scope: &entity.ScopeConfig{OwnerField: "owner_id"},
		Fields: []schema.Field{
			{Name: "owner_id", Type: schema.String},
			{Name: "title", Type: schema.String},
		},
		Relations: []entity.Relation{entity.ManyToMany("tags", "tags", "post_tags", "post_id", "tag_id")},
	}.WithTimestamps(false)))
	app.Registry.Register(entity.Define("tags", entity.EntityConfig{
		Table:  "tags",
		Fields: []schema.Field{{Name: "name", Type: schema.String}},
	}.WithTimestamps(false)))
	if err := AutoMigrate(db, app.Registry); err != nil {
		t.Fatalf("AutoMigrate: %v", err)
	}
	return app
}

// The audit trail keeps its rows but loses the erased user's data: the
// diff of every row the user acted in, and of every row that records an
// erased row, is replaced by the erasure marker.
func TestEraseScrubsAuditDiff(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *sql.DB, _ Dialect) {
		app := NewApp(WithDB(db), WithoutDefaultMiddleware())
		app.Entity("notes", entity.EntityConfig{
			Table: "notes",
			Scope: &entity.ScopeConfig{OwnerField: "owner_id"},
			Fields: []schema.Field{
				{Name: "body", Type: schema.String, Required: true},
				{Name: "owner_id", Type: schema.String},
			},
		}.WithTimestamps(false))
		if err := AutoMigrate(db, app.Registry); err != nil {
			t.Fatalf("AutoMigrate: %v", err)
		}
		app.WithAuditLog(AuditConfig{})
		prev := owner.GetExtractor()
		owner.SetExtractor(func(ctx context.Context) (any, bool) {
			if u, ok := handler.GetUser(ctx); ok && u != nil {
				return u.(*auditActorUser).GetID(), true
			}
			return nil, false
		})
		t.Cleanup(func() { owner.SetExtractor(prev) })

		req := httptest.NewRequest(http.MethodPost, "/notes", strings.NewReader(`{"body":"my private diary"}`))
		req.Header.Set("Content-Type", "application/json")
		req = req.WithContext(handler.SetUser(req.Context(), &auditActorUser{id: "u1"}))
		rec := httptest.NewRecorder()
		app.Router().ServeHTTP(rec, req)
		if rec.Code != http.StatusCreated {
			t.Fatalf("create = %d %s", rec.Code, rec.Body.String())
		}
		var noteID string
		if err := db.QueryRow(`SELECT id FROM notes`).Scan(&noteID); err != nil {
			t.Fatal(err)
		}
		// An admin's edit of u1's note: another actor, recording an erased row.
		cascadeExec(t, db, `INSERT INTO audit_log (id, entity, op, record_id, actor_id, created_at, diff)
			VALUES ('adm1', 'notes', 'update', '`+noteID+`', 'admin', '2024-01-01T00:00:00Z',
			'{"new":{"body":"my private diary, edited","owner_id":"u1"}}')`)
		// Untouched: another user's row by another actor.
		cascadeExec(t, db, `INSERT INTO audit_log (id, entity, op, record_id, actor_id, created_at, diff)
			VALUES ('oth1', 'notes', 'create', 'n-other', 'u2', '2024-01-01T00:00:00Z', '{"new":{"body":"u2 stuff"}}')`)

		leaks := func() []string {
			rows, err := db.Query(`SELECT id, diff FROM audit_log WHERE diff IS NOT NULL`)
			if err != nil {
				t.Fatal(err)
			}
			defer rows.Close()
			var out []string
			for rows.Next() {
				var id, diff string
				if err := rows.Scan(&id, &diff); err != nil {
					t.Fatal(err)
				}
				if strings.Contains(diff, "u1") || strings.Contains(diff, "my private diary") {
					out = append(out, id+": "+diff)
				}
			}
			if err := rows.Err(); err != nil {
				t.Fatal(err)
			}
			return out
		}
		if len(leaks()) < 2 {
			t.Fatalf("fixture: want u1's create row and the admin row to name u1 before erasure, got %v", leaks())
		}

		rep, err := app.EraseUserData(context.Background(), "u1")
		if err != nil {
			t.Fatalf("EraseUserData: %v", err)
		}
		if l := leaks(); len(l) > 0 {
			t.Errorf("audit diff still holds the erased user's data: %v", l)
		}
		var marker, other string
		if err := db.QueryRow(`SELECT diff FROM audit_log WHERE id = 'adm1'`).Scan(&marker); err != nil {
			t.Fatal(err)
		}
		if marker != erasedAuditDiff {
			t.Errorf("admin row diff = %q, want %q", marker, erasedAuditDiff)
		}
		if err := db.QueryRow(`SELECT diff FROM audit_log WHERE id = 'oth1'`).Scan(&other); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(other, "u2 stuff") {
			t.Errorf("another user's audit row was scrubbed: %q", other)
		}
		if rep.Audit == nil || rep.Audit.RowsAffected != 2 {
			t.Errorf("report.Audit = %+v, want 2 rows (u1's create, the admin edit)", rep.Audit)
		}
		if got := countAll(t, db, "audit_log"); got != 3 {
			t.Errorf("audit_log rows = %d, want 3 (the trail is kept)", got)
		}
	})
}
