package framework

import (
	"context"
	"database/sql"
	"testing"

	"github.com/gofastr/gofastr/core/schema"
)

// This file is the contract test for the public typed-API surface that
// generated repository code consumes (MarshalEntity, UnmarshalEntity,
// CreateOne/UpdateOne/DeleteOne/GetOne, NewTypedQuery, the Column
// constructors). It hand-rolls a repo in the same shape codegen produces so
// any breaking change to those entry points fails here, not silently in
// downstream-generated packages.

// e2ePost mirrors what `gofastr generate` would emit for a posts entity.
type e2ePost struct {
	ID    string `json:"id,omitempty"`
	Title string `json:"title,omitempty"`
	Body  string `json:"body,omitempty"`
}

// e2ePostsRepo mirrors the generator template output.
type e2ePostsRepo struct {
	handler *CrudHandler
}

func newE2EPostsRepo(app *App) *e2ePostsRepo {
	entity, err := app.Registry.Get("posts")
	if err != nil {
		panic("posts not registered: " + err.Error())
	}
	h := NewCrudHandler(entity, app.DB)
	h.JSONCase = app.JSONCasing()
	h.Hooks = app.HookRegistry("posts")
	h.Events = app.Events()
	h.Registry = app.Registry
	return &e2ePostsRepo{handler: h}
}

func (r *e2ePostsRepo) Create(ctx context.Context, row *e2ePost) error {
	body, err := MarshalEntity(row)
	if err != nil {
		return err
	}
	out, err := r.handler.CreateOne(ctx, body)
	if err != nil {
		return err
	}
	return UnmarshalEntity(out, row)
}

func (r *e2ePostsRepo) Get(ctx context.Context, id string) (*e2ePost, error) {
	out, err := r.handler.GetOne(ctx, id, nil)
	if err != nil {
		return nil, err
	}
	var p e2ePost
	if err := UnmarshalEntity(out, &p); err != nil {
		return nil, err
	}
	return &p, nil
}

func (r *e2ePostsRepo) Update(ctx context.Context, id string, row *e2ePost) error {
	body, err := MarshalEntity(row)
	if err != nil {
		return err
	}
	delete(body, "id")
	out, err := r.handler.UpdateOne(ctx, id, body)
	if err != nil {
		return err
	}
	return UnmarshalEntity(out, row)
}

func (r *e2ePostsRepo) Delete(ctx context.Context, id string) error {
	return r.handler.DeleteOne(ctx, id)
}

func (r *e2ePostsRepo) Query() *TypedQuery[e2ePost] { return NewTypedQuery[e2ePost](r.handler) }

func (r *e2ePostsRepo) WithTx(tx *sql.Tx) *e2ePostsRepo {
	h := *r.handler
	h.DB = tx
	return &e2ePostsRepo{handler: &h}
}

// "Generated" column constants — same shape codegen emits.
var (
	e2ePostsTitle = NewStringColumn("title")
	e2ePostsBody  = NewStringColumn("body")
)

// ============================================================================
// Test: full CRUD round-trip via the typed repo
// ============================================================================

func TestTypedRepoContract_RoundTrip(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *sql.DB, _ Dialect) {
		createPostsTestTable(t, db)
		app := NewApp(WithDB(db), WithoutDefaultMiddleware())
		app.Entity("posts", EntityConfig{
			Table: "posts",
			Fields: []schema.Field{
				{Name: "title", Type: schema.String, Required: true},
				{Name: "body", Type: schema.Text},
			},
		}.WithTimestamps(false))

		repo := newE2EPostsRepo(app)
		ctx := context.Background()

		p := &e2ePost{Title: "hello", Body: "world"}
		if err := repo.Create(ctx, p); err != nil {
			t.Fatalf("create: %v", err)
		}
		if p.ID == "" {
			t.Fatal("expected ID populated post-create")
		}

		got, err := repo.Get(ctx, p.ID)
		if err != nil {
			t.Fatalf("get: %v", err)
		}
		if got.Title != "hello" || got.Body != "world" {
			t.Fatalf("get returned %+v", got)
		}

		// Update
		got.Title = "updated"
		if err := repo.Update(ctx, got.ID, got); err != nil {
			t.Fatalf("update: %v", err)
		}
		if got.Title != "updated" {
			t.Fatalf("update did not refresh struct: %+v", got)
		}

		// Query
		list, err := repo.Query().
			Where(e2ePostsTitle.Like("%date%")).
			Find(ctx)
		if err != nil {
			t.Fatalf("query: %v", err)
		}
		if len(list) != 1 || list[0].Title != "updated" {
			t.Fatalf("query result: %+v", list)
		}

		// Delete
		if err := repo.Delete(ctx, got.ID); err != nil {
			t.Fatalf("delete: %v", err)
		}
		_, err = repo.Get(ctx, got.ID)
		if !IsNotFound(err) {
			t.Fatalf("expected not-found after delete, got %v", err)
		}
	})
}

// ============================================================================
// Test: WithTx returns a tx-bound repo whose writes are atomic with the tx
// ============================================================================

func TestTypedRepoContract_WithTx(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *sql.DB, _ Dialect) {
		createPostsTestTable(t, db)
		app := NewApp(WithDB(db), WithoutDefaultMiddleware())
		app.Entity("posts", EntityConfig{
			Table: "posts",
			Fields: []schema.Field{
				{Name: "title", Type: schema.String, Required: true},
				{Name: "body", Type: schema.Text},
			},
		}.WithTimestamps(false))
		repo := newE2EPostsRepo(app)
		ctx := context.Background()

		// Open a tx, use the tx-bound repo to insert, then ROLL BACK explicitly.
		// The row must not survive.
		tx, err := db.BeginTx(ctx, nil)
		if err != nil {
			t.Fatalf("begin tx: %v", err)
		}
		txRepo := repo.WithTx(tx)
		p := &e2ePost{Title: "tentative"}
		if err := txRepo.Create(ctx, p); err != nil {
			tx.Rollback()
			t.Fatalf("tx create: %v", err)
		}
		if err := tx.Rollback(); err != nil {
			t.Fatalf("rollback: %v", err)
		}

		// Outside the tx, no rows.
		var n int
		if err := db.QueryRow("SELECT COUNT(*) FROM posts").Scan(&n); err != nil {
			t.Fatalf("count: %v", err)
		}
		if n != 0 {
			t.Fatalf("expected 0 rows post-rollback, got %d", n)
		}
	})
}
