package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"

	"github.com/DonaldMurillo/gofastr/battery/search"
	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework"
	"github.com/DonaldMurillo/gofastr/framework/db"
	"github.com/DonaldMurillo/gofastr/framework/hook"
	"github.com/DonaldMurillo/gofastr/framework/isolation"
	_ "github.com/DonaldMurillo/gofastr/sqlite/stdlib"
)

// databaseURL is DATABASE_URL when set, so a test or a deploy keeps the
// database out of the source tree, and ./blog.db otherwise.
func databaseURL() string {
	if dsn := os.Getenv("DATABASE_URL"); dsn != "" {
		return dsn
	}
	return "./blog.db"
}

func main() {
	db, err := sql.Open("sqlite3", databaseURL())
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()

	app := framework.NewApp(
		framework.WithDB(db),
		framework.WithConfig(framework.AppConfig{Name: "blog"}),
	)

	// --- Entities ---
	// Declared in Go so `go run ./examples/blog` works from any directory (no
	// external entity files to locate). gofastr.yml describes the same app for
	// the no-code / codegen path.
	//
	// The two are NOT byte-for-byte equivalent and nothing enforces that they
	// are: the yml additionally declares mcp, indices, cursor pagination, a
	// users.avatar field, and display properties this file omits, because those
	// drive generated surfaces that `go run` does not build. What DOES have to
	// match is the EXPOSURE, which entity is readable and writable by whom.
	// That is the half that drifted once, leaving this runnable twin accepting
	// anonymous deletes the app spec had already closed, so keep the
	// Exposure blocks below in step with gofastr.yml.
	registerEntities(app)

	// --- Custom endpoints ---

	// GET /posts/published, filter published posts
	app.Router().Get("/posts/published", postsByStatus(app, "published"))
	searchIndex := search.NewMemory()
	syncSearch(app, searchIndex)
	app.Router().Get("/posts/search", searchPosts(searchIndex))

	// --- Seed ---
	// AutoMigrate before seeding so the tables exist (app.Start migrates
	// too, but that runs after this and is idempotent).
	if err := framework.AutoMigrate(db, app.Registry); err != nil {
		log.Fatal(err)
	}
	seed(db)
	if err := indexPosts(context.Background(), app, searchIndex); err != nil {
		log.Fatal(err)
	}

	// --- Start (auto-migrates, auto-routes CRUD, auto-serves OpenAPI + Swagger) ---

	addr, err := isolation.ListenAddr(".", ":8080")
	if err != nil {
		log.Fatal(err)
	}
	if err := app.Start(addr); err != nil {
		log.Fatal(err)
	}
}

// registerEntities declares the blog's three entities in Go.
func registerEntities(app *framework.App) {
	app.Entity("users", framework.EntityConfig{Scope:
	// email is PII: accounts are staff-managed records, so every operation
	// is RBAC-gated (fail-closed 403 for anonymous), hard rule #6. This
	// mirrors the access: block in the app spec twin (gofastr.yml).
	&framework.ScopeConfig{}, Exposure: &framework.ExposureConfig{Access: framework.AccessControl{
		Read:   "users:read",
		Create: "users:write",
		Update: "users:write",
		Delete: "users:admin",
	}}, Fields: []schema.Field{
		{Name: "name", Type: schema.String, Required: true},
		{Name: "email", Type: schema.String, Required: true, Unique: true},
		{Name: "role", Type: schema.Enum, Values: []string{"admin", "author", "reader"}, Default: "reader"},
	},
		Relations: []framework.Relation{
			framework.HasMany("posts", "posts", "author_id"),
			framework.HasMany("comments", "comments", "author_id"),
		},
	})
	app.Entity("posts", framework.EntityConfig{Pagination:
	// public demo content. See "Default CRUD authentication" in the security docs.
	&framework.PaginationConfig{}, Exposure: &framework.ExposureConfig{Access: framework.AccessControl{
		// Mirrors gofastr.yml: readable by anyone, every write gated.
		// Public: true would also grant anonymous create/update/delete.
		Read: "", Create: "posts:write", Update: "posts:write", Delete: "posts:admin",
	},
		// Readable by anyone means published posts: a draft stays out of
		// every read (list, get, ?include=, MCP) unless the caller may
		// write posts.
		ReadScope: postsReadScope,
	}, Scope: &framework.ScopeConfig{SoftDelete: true}, Fields: []schema.Field{
		{Name: "title", Type: schema.String, Required: true, Max: new(300.0)},
		{Name: "body", Type: schema.Text},
		{Name: "status", Type: schema.Enum, Values: []string{"draft", "published"}, Default: "draft"},
		{Name: "author_id", Type: schema.String},
		{Name: "published_at", Type: schema.Timestamp},
	},
		Relations: []framework.Relation{
			framework.BelongsTo("author", "users", "author_id"),
			framework.HasMany("comments", "comments", "post_id"),
		},
	})
	app.Entity("comments", framework.EntityConfig{Scope:
	// public demo content. See "Default CRUD authentication" in the security docs.
	&framework.ScopeConfig{}, Exposure: &framework.ExposureConfig{Access: framework.AccessControl{
		// Mirrors gofastr.yml. See the posts entity above.
		Read: "", Create: "comments:write", Update: "comments:write", Delete: "comments:admin",
	}}, Fields: []schema.Field{
		{Name: "body", Type: schema.Text, Required: true},
		{Name: "post_id", Type: schema.String, Required: true},
		{Name: "author_id", Type: schema.String},
	},
		Relations: []framework.Relation{
			framework.BelongsTo("post", "posts", "post_id"),
			framework.BelongsTo("author", "users", "author_id"),
		},
	})
	scopeCommentsToReadablePosts(app)
}

// commentsOnReadablePosts keeps a comment to the readers of its post. A
// read scope filters only the entity's own columns, so postsReadScope
// hides a draft while its comments stayed on /comments and
// /comments/{id}, telling anyone the draft existed and quoting the
// discussion of it. The subquery is the posts read scope plus soft
// delete; the args bind, so nothing is concatenated into the SQL.
const commentsOnReadablePosts = "post_id IN (SELECT id FROM posts WHERE status = $1 AND deleted_at IS NULL)"

// scopeCommentsToReadablePosts narrows every auto-CRUD comment read
// (list, count, get, ?include=, a write's response body) for callers
// that may not read drafts, the same line postsReadScope draws.
func scopeCommentsToReadablePosts(app *framework.App) {
	mayReadDrafts := func(ctx context.Context) bool {
		return framework.Can(ctx, framework.Permission(postsReadScope.Unrestricted))
	}
	framework.OnBeforeList(app, "comments", func(ctx context.Context, p *hook.ListPayload) error {
		if !mayReadDrafts(ctx) {
			p.AddWhere(commentsOnReadablePosts, "published")
		}
		return nil
	})
	framework.OnBeforeGet(app, "comments", func(ctx context.Context, p *hook.GetPayload) error {
		if !mayReadDrafts(ctx) {
			p.AddWhere(commentsOnReadablePosts, "published")
		}
		return nil
	})
}

//go:fix inline
func ptr[T any](v T) *T { return new(v) }

func searchPosts(index search.Backend) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Public route: drafts stay indexed but never answer it.
		results, err := index.Search(r.Context(), search.Query{
			Text:        r.URL.Query().Get("q"),
			Type:        "posts",
			FieldEquals: map[string]string{"status": "published"},
			Limit:       10,
		})
		if err != nil {
			log.Printf("blog: %v", err)
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"data": results})
	}
}

// postsByStatus returns a handler that lists posts filtered by status.
func postsByStatus(app *framework.App, status string) http.HandlerFunc {
	entity, err := app.Registry.Get("posts")
	if err != nil {
		log.Fatalf("blog: posts entity: %v", err)
	}
	handler := framework.NewCrudHandler(entity, app.DB)
	listHandler := handler.List()

	return func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		q.Set("status", status)
		r.URL.RawQuery = q.Encode()
		listHandler.ServeHTTP(w, r)
	}
}

func seed(db *sql.DB) {
	db.Exec(`INSERT OR IGNORE INTO users (id, name, email, role) VALUES ('u1', 'Alice', 'alice@example.com', 'author')`)
	db.Exec(`INSERT OR IGNORE INTO users (id, name, email, role) VALUES ('u2', 'Bob', 'bob@example.com', 'reader')`)
	db.Exec(`INSERT OR IGNORE INTO posts (id, title, body, status, author_id) VALUES ('p1', 'Hello World', 'My first post!', 'published', 'u1')`)
	db.Exec(`INSERT OR IGNORE INTO posts (id, title, body, status, author_id) VALUES ('p2', 'GoFastr Framework', 'Building the future', 'published', 'u1')`)
	db.Exec(`INSERT OR IGNORE INTO posts (id, title, body, status, author_id) VALUES ('p3', 'Draft', 'Work in progress...', 'draft', 'u1')`)
	db.Exec(`INSERT OR IGNORE INTO comments (id, body, post_id, author_id) VALUES ('c1', 'Great post!', 'p1', 'u2')`)
	db.Exec(`INSERT OR IGNORE INTO comments (id, body, post_id, author_id) VALUES ('c2', 'Interesting', 'p2', 'u2')`)
}

// postsReadScope keeps anonymous readers to published posts. A caller
// holding posts:write (an author) reads drafts too. Mirrors read_scope
// in gofastr.yml.
var postsReadScope = &framework.ReadScopeConfig{
	Filter:       []framework.RowPredicate{{Field: "status", Op: "eq", Value: "published"}},
	Unrestricted: "posts:write",
}

// indexPosts fills index from the posts table. ListAll leaves
// soft-deleted rows out, so a post deleted before a restart stays out
// of search after it. Drafts are indexed too (the public search route
// filters its answers), so the rebuild reads past postsReadScope as a
// caller holding its Unrestricted permission.
func indexPosts(ctx context.Context, app *framework.App, index search.Backend) error {
	posts, err := app.CrudHandler("posts")
	if err != nil {
		return err
	}
	policy := framework.NewRolePolicy()
	if err := policy.Grant("search-indexer", framework.Permission(postsReadScope.Unrestricted)); err != nil {
		return fmt.Errorf("index posts: %w", err)
	}
	ctx = framework.WithRoles(framework.WithPolicy(ctx, policy), []string{"search-indexer"})
	rows, err := posts.ListAll(ctx, framework.ListOptions{})
	if err != nil {
		return fmt.Errorf("index posts: %w", err)
	}
	for _, row := range rows {
		if err := index.Index(ctx, postDocument(row)); err != nil {
			return fmt.Errorf("index posts: %w", err)
		}
	}
	return nil
}

// syncSearch keeps index on the posts table: a create, an edit or a
// restore re-indexes the row, a delete (soft or purge) removes it. The
// change waits for the write's commit, so a write that rolls back
// never reaches the index.
func syncSearch(app *framework.App, index search.Backend) {
	put := func(ctx context.Context, data any) error {
		row, ok := data.(map[string]any)
		if !ok {
			return fmt.Errorf("blog: posts hook got %T, want a row", data)
		}
		doc := postDocument(row)
		afterCommit(ctx, func(ctx context.Context) error { return index.Index(ctx, doc) })
		return nil
	}
	hooks := app.HookRegistry("posts")
	hooks.RegisterHook(framework.AfterCreate, put)
	hooks.RegisterHook(framework.AfterUpdate, put)
	hooks.RegisterHook(framework.AfterDelete, func(ctx context.Context, data any) error {
		id, ok := data.(string)
		if !ok {
			return fmt.Errorf("blog: posts delete hook got %T, want an id", data)
		}
		afterCommit(ctx, func(ctx context.Context) error { return index.Delete(ctx, id) })
		return nil
	})
}

// afterCommit runs fn once the hook's transaction commits, on a context
// the request's end does not cancel. Outside a framework transaction it
// runs fn now.
func afterCommit(ctx context.Context, fn func(context.Context) error) {
	ctx = context.WithoutCancel(ctx)
	run := func() {
		if err := fn(ctx); err != nil {
			log.Printf("blog: search index: %v", err)
		}
	}
	if q, ok := db.CommitQueueFromContext(ctx); ok {
		q.Add(run)
		return
	}
	run()
}

// postDocument is a posts row as a search document: title and body are
// the text, and status rides along for searchPosts' published filter.
func postDocument(row map[string]any) search.Document {
	title, _ := row["title"].(string)
	body, _ := row["body"].(string)
	status, _ := row["status"].(string)
	return search.Document{
		ID:     fmt.Sprint(row["id"]),
		Type:   "posts",
		Text:   strings.TrimSpace(title + " " + body),
		Fields: map[string]any{"status": status},
	}
}
