package framework

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/framework/hook"
	"github.com/DonaldMurillo/gofastr/framework/softdelete"
)

// A typed query's bulk writes run on the transaction in ctx: UpdateAll
// and DeleteAll inside App.InTx used the pool, so they committed (or, on
// SQLite, blocked) whatever the transaction's owner decided.
func TestBulkWritesJoinInTx(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *sql.DB, _ Dialect) {
		app := NewApp(WithDB(db), WithoutDefaultMiddleware())
		for _, name := range []string{"bnotes", "bsoft"} {
			cfg := entity.EntityConfig{Table: name, Fields: []schema.Field{{Name: "title", Type: schema.String}}}.WithTimestamps(false)
			if name == "bsoft" {
				cfg.Scope = &entity.ScopeConfig{SoftDelete: true}
			}
			app.Entity(name, cfg)
			if _, err := db.Exec(fmt.Sprintf(`CREATE TABLE %s (id TEXT PRIMARY KEY, title TEXT, deleted_at TIMESTAMP)`, name)); err != nil {
				t.Fatal(err)
			}
		}
		sentinel := errors.New("roll back")
		ops := map[string]func(ctx context.Context, table string) error{
			"UpdateAll": func(ctx context.Context, table string) error {
				ch, err := app.CrudHandler(table)
				if err != nil {
					return err
				}
				_, err = NewTypedQuery[struct{}](ch).UpdateAll(ctx, map[string]any{"title": "changed"})
				return err
			},
			"DeleteAll": func(ctx context.Context, table string) error {
				ch, err := app.CrudHandler(table)
				if err != nil {
					return err
				}
				_, err = NewTypedQuery[struct{}](ch).DeleteAll(ctx)
				return err
			},
		}
		for _, table := range []string{"bnotes", "bsoft"} {
			for name, op := range ops {
				if _, err := db.Exec(fmt.Sprintf(`DELETE FROM %s`, table)); err != nil {
					t.Fatal(err)
				}
				if _, err := db.Exec(fmt.Sprintf(`INSERT INTO %s (id, title) VALUES ('n1', 'keep')`, table)); err != nil {
					t.Fatal(err)
				}
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				err := app.InTx(ctx, func(ctx context.Context, _ *sql.Tx) error {
					if err := op(ctx, table); err != nil {
						return err
					}
					return sentinel
				})
				cancel()
				if !errors.Is(err, sentinel) {
					t.Errorf("%s %s in InTx: %v", table, name, err)
				}
				var title string
				var deleted sql.NullString
				if err := db.QueryRow(fmt.Sprintf(`SELECT title, deleted_at FROM %s WHERE id = 'n1'`, table)).Scan(&title, &deleted); err != nil {
					t.Errorf("%s %s survived the rollback: %v", table, name, err)
					continue
				}
				if title != "keep" || deleted.Valid {
					t.Errorf("%s %s survived the rollback: title %q, deleted_at %v", table, name, title, deleted)
				}
			}
		}
		// A read in the transaction sees the transaction's own write.
		ch, err := app.CrudHandler("bnotes")
		if err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = app.InTx(ctx, func(ctx context.Context, tx *sql.Tx) error {
			if _, err := tx.ExecContext(ctx, `INSERT INTO bnotes (id, title) VALUES ('n2', 'new')`); err != nil {
				t.Fatal(err)
			}
			if n, err := NewTypedQuery[struct{}](ch).Count(ctx); err != nil || n != 2 {
				t.Errorf("Count in the transaction = %d, %v; want 2", n, err)
			}
			return sentinel
		})
	})
}

type bigIntHookRow struct {
	Title string         `json:"title,omitempty"`
	Meta  map[string]any `json:"meta,omitempty"`
}

// A typed Before hook that edits a JSON field keeps the field's other
// numbers exact: the JSON round trip into map[string]any read
// 9007199254740993 as a float64, and the merge wrote 9007199254740992.
func TestTypedHookKeepsBigJSONInt(t *testing.T) {
	for _, phase := range []hook.HookType{hook.BeforeCreate, hook.BeforeUpdate} {
		app := NewApp(WithoutDefaultMiddleware())
		redact := func(_ context.Context, v *bigIntHookRow) error {
			delete(v.Meta, "secret")
			return nil
		}
		OnBeforeCreate[bigIntHookRow](app, "rows", redact)
		OnBeforeUpdate[bigIntHookRow](app, "rows", redact)
		body := map[string]any{
			"title": "t",
			"meta":  map[string]any{"externalId": int64(9007199254740993), "secret": "x", "ratio": 0.5},
		}
		if err := app.HookRegistry("rows").ExecuteHooks(context.Background(), phase, body); err != nil {
			t.Fatalf("%s: %v", phase, err)
		}
		meta, _ := body["meta"].(map[string]any)
		if _, ok := meta["secret"]; ok {
			t.Errorf("%s: the hook's delete did not land: %v", phase, meta)
		}
		if got := meta["externalId"]; got != int64(9007199254740993) {
			t.Errorf("%s: externalId = %v (%T), want int64 9007199254740993", phase, got, got)
		}
		if got := meta["ratio"]; got != 0.5 {
			t.Errorf("%s: ratio = %v (%T), want 0.5", phase, got, got)
		}
		// The CRUD decode keeps an integer float64 would round as the
		// json.Number it read (past int64, or spelled with an exponent);
		// the hook round trip keeps its digits too.
		for _, n := range []json.Number{"9223372036854775809", "9007199254740993e0"} {
			body := map[string]any{"meta": map[string]any{"n": n, "secret": "x"}}
			if err := app.HookRegistry("rows").ExecuteHooks(context.Background(), phase, body); err != nil {
				t.Fatalf("%s: %v", phase, err)
			}
			meta, _ := body["meta"].(map[string]any)
			if got, _ := json.Marshal(meta["n"]); string(got) != string(n) {
				t.Errorf("%s: n = %s (%T), want %s", phase, got, meta["n"], n)
			}
		}
		if got := body["title"]; got != "t" {
			t.Errorf("%s: title = %v", phase, got)
		}
	}
}

// TestMapHookKeepsBigIntegers: a typed hook over a map type (the merge's
// marshal fallback) writes the payload's numbers back with their digits.
func TestMapHookKeepsBigIntegers(t *testing.T) {
	app := NewApp(WithoutDefaultMiddleware())
	OnBeforeCreate[map[string]any](app, "rows", func(_ context.Context, v *map[string]any) error {
		(*v)["title"] = "edited"
		return nil
	})
	body := map[string]any{
		"title":       "t",
		"external_id": int64(9007199254740993),
		"meta":        map[string]any{"n": int64(9007199254740993)},
	}
	if err := app.HookRegistry("rows").ExecuteHooks(context.Background(), hook.BeforeCreate, body); err != nil {
		t.Fatal(err)
	}
	got, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	if want := `{"external_id":9007199254740993,"meta":{"n":9007199254740993},"title":"edited"}`; string(got) != want {
		t.Fatalf("payload = %s, want %s", got, want)
	}
}

// A pivot insert refused by a constraint other than the link's own key
// is an error: it was swallowed as a racing duplicate, so SQLite
// committed the parent and reported a link that does not exist.
func TestPivotLinkConflictRollsBack(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *sql.DB, _ Dialect) {
		seedCascadeDB(t, db)
		app := cascadeTestApp(t, db)
		for _, s := range []string{
			`CREATE UNIQUE INDEX article_tags_one_article ON article_tags (tag_id)`,
			`INSERT INTO users (id, name) VALUES ('u1', 'Ann')`,
			`INSERT INTO articles (id, title, author_id) VALUES ('a1', 'first', 'u1')`,
			`INSERT INTO tags (id, name) VALUES ('t1', 'go')`,
			`INSERT INTO article_tags (article_id, tag_id) VALUES ('a1', 't1')`,
		} {
			if _, err := db.Exec(s); err != nil {
				t.Fatalf("%s: %v", s, err)
			}
		}
		ta := TestHarness(t, app).AsUser(struct{ ID string }{ID: "u1"})
		resp := ta.Post("/articles", map[string]any{"id": "a2", "title": "second", "author_id": "u1", "tags": []any{"t1"}})
		if resp.Status() != http.StatusConflict {
			t.Errorf("a refused link answered %d, want 409: %s", resp.Status(), resp.Body())
		}
		var n int
		if err := db.QueryRow(`SELECT COUNT(*) FROM articles WHERE id = 'a2'`).Scan(&n); err != nil || n != 0 {
			t.Errorf("the parent of a refused link was committed (%d, %v)", n, err)
		}
		// A link that already exists is still no error.
		ta.Put("/articles/a1", map[string]any{"title": "first", "author_id": "u1", "tags": []any{"t1"}}).AssertStatus(t, http.StatusOK)
	})
}

// Another tenant's link does not stand in for the caller's: the
// existence check read the pivot unscoped, found tenant A's row and
// skipped tenant B's insert.
func TestPivotLinkScopedToTenant(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *sql.DB, _ Dialect) {
		app := NewApp(WithDB(db), WithoutDefaultMiddleware())
		app.Entity("tags_sh", entity.EntityConfig{
			Table:  "tags_sh",
			Fields: []schema.Field{{Name: "id", Type: schema.String}, {Name: "name", Type: schema.String}},
		}.WithTimestamps(false))
		app.Entity("posts_sh", entity.EntityConfig{
			Table:     "posts_sh",
			Fields:    []schema.Field{{Name: "id", Type: schema.String}, {Name: "title", Type: schema.String}},
			Relations: []entity.Relation{entity.ManyToMany("tags", "tags_sh", "post_tags_tn", "post_id", "tag_id").WithCascadeWrite(true)},
		}.WithTimestamps(false))
		app.Entity("post_tags_tn", entity.EntityConfig{
			Table: "post_tags_tn", Scope: &entity.ScopeConfig{MultiTenant: true, TenantField: "tenant_id"},
			PrimaryKey: []string{"tenant_id", "post_id", "tag_id"},
			Fields:     []schema.Field{{Name: "tenant_id", Type: schema.String}, {Name: "post_id", Type: schema.String}, {Name: "tag_id", Type: schema.String}},
		}.WithTimestamps(false))
		for _, s := range []string{
			`CREATE TABLE posts_sh (id TEXT PRIMARY KEY, title TEXT)`,
			`CREATE TABLE tags_sh (id TEXT PRIMARY KEY, name TEXT)`,
			`CREATE TABLE post_tags_tn (tenant_id TEXT NOT NULL, post_id TEXT NOT NULL, tag_id TEXT NOT NULL, PRIMARY KEY (tenant_id, post_id, tag_id))`,
			`INSERT INTO posts_sh (id, title) VALUES ('p1', 'shared')`,
			`INSERT INTO tags_sh (id, name) VALUES ('t1', 'go')`,
			`INSERT INTO post_tags_tn (tenant_id, post_id, tag_id) VALUES ('tenant-a', 'p1', 't1')`,
		} {
			if _, err := db.Exec(s); err != nil {
				t.Fatalf("%s: %v", s, err)
			}
		}
		ta := TestHarness(t, app).AsUser(struct{ ID string }{ID: "u1"}).AsTenant("tenant-b")
		ta.Put("/posts_sh/p1", map[string]any{"title": "shared", "tags": []any{"t1"}}).AssertStatus(t, http.StatusOK)
		var n int
		if err := db.QueryRow(`SELECT COUNT(*) FROM post_tags_tn WHERE tenant_id = 'tenant-b' AND post_id = 'p1' AND tag_id = 't1'`).Scan(&n); err != nil || n != 1 {
			t.Errorf("tenant-b's link was not written (%d, %v)", n, err)
		}
	})
}

// softdelete.SoftDelete stamped deleted_at with NOW(), which SQLite does
// not define, so the row stayed live there.
func TestSoftDeleteHelperBothDialects(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *sql.DB, _ Dialect) {
		if _, err := db.Exec(`CREATE TABLE sd_posts (id TEXT PRIMARY KEY, deleted_at TIMESTAMP)`); err != nil {
			t.Fatal(err)
		}
		if _, err := db.Exec(`INSERT INTO sd_posts (id) VALUES ('p1')`); err != nil {
			t.Fatal(err)
		}
		if err := softdelete.SoftDelete(context.Background(), db, "sd_posts", "p1"); err != nil {
			t.Fatalf("SoftDelete: %v", err)
		}
		var deleted sql.NullString
		if err := db.QueryRow(`SELECT deleted_at FROM sd_posts WHERE id = 'p1'`).Scan(&deleted); err != nil || !deleted.Valid {
			t.Errorf("SoftDelete left the row live (%v, %v)", deleted, err)
		}
	})
}

// A link a concurrent request committed between the check and the insert
// is no error. The pivot's BeforeCreate hook stages that race: it commits
// the same row on another connection. The insert then runs under a
// savepoint, so the refused statement leaves the Postgres transaction
// usable for the recheck that finds the link. SQLite serializes writers,
// so the race cannot happen there.
func TestPivotLinkRaceIsNoError(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *sql.DB, dialect Dialect) {
		if dialect != DialectPostgres {
			t.Skip("SQLite serializes writers: no concurrent link")
		}
		// The test pool holds one connection, which the request's
		// transaction owns, so the concurrent writer gets a pool of its
		// own and names the test's schema.
		var schemaName string
		if err := db.QueryRow(`SELECT current_schema()`).Scan(&schemaName); err != nil {
			t.Fatal(err)
		}
		dsn, err := resolvePostgresOnce()
		if err != nil {
			t.Fatal(err)
		}
		other, err := sql.Open("postgres", dsn)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { other.Close() })
		app := NewApp(WithDB(db), WithoutDefaultMiddleware())
		app.Entity("posts_rc", entity.EntityConfig{
			Table:     "posts_rc",
			Fields:    []schema.Field{{Name: "id", Type: schema.String}, {Name: "title", Type: schema.String}},
			Relations: []entity.Relation{entity.ManyToMany("tags", "tags_rc", "post_tags_rc", "post_id", "tag_id").WithCascadeWrite(true)},
		}.WithTimestamps(false))
		app.Entity("tags_rc", entity.EntityConfig{
			Table:  "tags_rc",
			Fields: []schema.Field{{Name: "id", Type: schema.String}, {Name: "name", Type: schema.String}},
		}.WithTimestamps(false))
		app.Entity("post_tags_rc", entity.EntityConfig{
			Table: "post_tags_rc", PrimaryKey: []string{"post_id", "tag_id"},
			Fields: []schema.Field{{Name: "post_id", Type: schema.String}, {Name: "tag_id", Type: schema.String}},
		}.WithTimestamps(false))
		app.HookRegistry("post_tags_rc").RegisterHook(hook.BeforeCreate, func(ctx context.Context, _ any) error {
			if _, ok := TxFromContext(ctx); !ok {
				return errors.New("no transaction in the pivot create")
			}
			_, err := other.ExecContext(ctx, `INSERT INTO `+schemaName+`.post_tags_rc (post_id, tag_id) VALUES ('p1', 't1')`)
			return err
		})
		for _, s := range []string{
			`CREATE TABLE posts_rc (id TEXT PRIMARY KEY, title TEXT)`,
			`CREATE TABLE tags_rc (id TEXT PRIMARY KEY, name TEXT)`,
			`CREATE TABLE post_tags_rc (post_id TEXT NOT NULL, tag_id TEXT NOT NULL, PRIMARY KEY (post_id, tag_id))`,
			`INSERT INTO tags_rc (id, name) VALUES ('t1', 'go')`,
		} {
			if _, err := db.Exec(s); err != nil {
				t.Fatalf("%s: %v", s, err)
			}
		}
		ta := TestHarness(t, app).AsUser(struct{ ID string }{ID: "u1"})
		ta.Post("/posts_rc", map[string]any{"id": "p1", "title": "x", "tags": []any{"t1"}}).AssertStatus(t, http.StatusCreated)
		var n int
		if err := db.QueryRow(`SELECT COUNT(*) FROM post_tags_rc WHERE post_id = 'p1'`).Scan(&n); err != nil || n != 1 {
			t.Errorf("pivot rows = %d, %v; want 1", n, err)
		}
	})
}

// A number out of float64's range is refused, as json.Unmarshal refuses
// it, rather than landing as zero.
func TestHookJSONRefusesHugeNumber(t *testing.T) {
	var dest struct {
		Meta map[string]any `json:"meta"`
	}
	if err := decodeHookJSON([]byte(`{"meta":{"x":1e400}}`), &dest); err == nil {
		t.Errorf("decoded 1e400 as %v, want an error", dest.Meta["x"])
	}
}
