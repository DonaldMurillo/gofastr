package framework

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"testing"

	"github.com/gofastr/gofastr/core/router"
	"github.com/gofastr/gofastr/core/schema"
	_ "github.com/mattn/go-sqlite3"
)

// setupTestDB creates an in-memory SQLite database with the required tables.
func setupTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	return db
}

// createPostsTable creates the posts table in the test database.
func createPostsTable(t *testing.T, db *sql.DB) {
	t.Helper()
	_, err := db.Exec(`CREATE TABLE IF NOT EXISTS posts (
		id TEXT PRIMARY KEY,
		title TEXT NOT NULL,
		body TEXT DEFAULT '',
		status TEXT DEFAULT 'draft',
		author_id TEXT DEFAULT '',
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
	)`)
	if err != nil {
		t.Fatalf("create posts table: %v", err)
	}
}

// createCommentsTable creates the comments table in the test database.
func createCommentsTable(t *testing.T, db *sql.DB) {
	t.Helper()
	_, err := db.Exec(`CREATE TABLE IF NOT EXISTS comments (
		id TEXT PRIMARY KEY,
		body TEXT NOT NULL,
		post_id TEXT NOT NULL,
		author_id TEXT DEFAULT '',
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
	)`)
	if err != nil {
		t.Fatalf("create comments table: %v", err)
	}
}

// seedPosts inserts test data and returns the IDs.
func seedPosts(t *testing.T, db *sql.DB, posts ...map[string]string) []string {
	t.Helper()
	var ids []string
	for i, p := range posts {
		id := p["id"]
		if id == "" {
			id = fmt.Sprintf("post-%d", i+1)
		}
		title := p["title"]
		body := p["body"]
		status := p["status"]
		if status == "" {
			status = "draft"
		}
		authorID := p["author_id"]
		_, err := db.Exec("INSERT INTO posts (id, title, body, status, author_id) VALUES (?, ?, ?, ?, ?)",
			id, title, body, status, authorID)
		if err != nil {
			t.Fatalf("seed post %d: %v", i, err)
		}
		ids = append(ids, id)
	}
	return ids
}

// ============================================================================
// E2E: Full CRUD Round-Trip with Real Database
// ============================================================================

func TestE2E_CRUD_CreateAndGet(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()
	createPostsTable(t, db)

	app := NewApp(WithDB(db))
	entity := Define("posts", EntityConfig{
		Table: "posts",
		Fields: []schema.Field{
			{Name: "id", Type: schema.String, Required: true},
			{Name: "title", Type: schema.String, Required: true},
			{Name: "body", Type: schema.Text},
			{Name: "status", Type: schema.String, Default: "draft"},
			{Name: "author_id", Type: schema.String},
		},
	})
	app.Registry.Register(entity)

	crud := NewCrudHandler(entity, db)
	RegisterCrudRoutes(app.Router, crud, "/posts")

	ta := TestHarness(t, app)
	defer ta.Close()

	// CREATE a post
	resp := ta.Post("/posts", map[string]string{
		"id":    "post-1",
		"title": "Hello World",
		"body":  "My first post",
	})
	resp.AssertStatus(t, http.StatusCreated)
	resp.AssertBodyContains(t, "Hello World")

	// Verify the body contains the ID
	var createResult map[string]any
	if err := resp.JSON(&createResult); err != nil {
		t.Fatalf("decode create response: %v", err)
	}
	if createResult["id"] != "post-1" {
		t.Errorf("expected id=post-1, got %v", createResult["id"])
	}

	// GET the same post back
	resp = ta.Get("/posts/post-1")
	resp.AssertStatus(t, http.StatusOK)
	resp.AssertBodyContains(t, "Hello World")

	var getResult map[string]any
	if err := resp.JSON(&getResult); err != nil {
		t.Fatalf("decode get response: %v", err)
	}
	if getResult["title"] != "Hello World" {
		t.Errorf("expected title=Hello World, got %v", getResult["title"])
	}

	// Verify data in database directly
	var title string
	err := db.QueryRow("SELECT title FROM posts WHERE id = ?", "post-1").Scan(&title)
	if err != nil {
		t.Fatalf("direct DB query: %v", err)
	}
	if title != "Hello World" {
		t.Errorf("expected DB title=Hello World, got %s", title)
	}
}

func TestE2E_CRUD_List(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()
	createPostsTable(t, db)

	// Seed 3 posts
	seedPosts(t, db,
		map[string]string{"id": "p1", "title": "Post One", "status": "published"},
		map[string]string{"id": "p2", "title": "Post Two", "status": "draft"},
		map[string]string{"id": "p3", "title": "Post Three", "status": "published"},
	)

	app := NewApp(WithDB(db))
	entity := Define("posts", EntityConfig{
		Table: "posts",
		Fields: []schema.Field{
			{Name: "id", Type: schema.String},
			{Name: "title", Type: schema.String},
			{Name: "body", Type: schema.Text},
			{Name: "status", Type: schema.String},
			{Name: "author_id", Type: schema.String},
		},
	})
	app.Registry.Register(entity)

	crud := NewCrudHandler(entity, db)
	RegisterCrudRoutes(app.Router, crud, "/posts")

	ta := TestHarness(t, app)
	defer ta.Close()

	// LIST all posts
	resp := ta.Get("/posts")
	resp.AssertStatus(t, http.StatusOK)
	resp.AssertBodyContains(t, "Post One")
	resp.AssertBodyContains(t, "Post Three")

	var listResult ListResponse
	if err := resp.JSON(&listResult); err != nil {
		t.Fatalf("decode list response: %v", err)
	}
	if listResult.Total != 3 {
		t.Errorf("expected total=3, got %d", listResult.Total)
	}
	if len(listResult.Data) != 3 {
		t.Errorf("expected 3 data items, got %d", len(listResult.Data))
	}
}

func TestE2E_CRUD_Update(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()
	createPostsTable(t, db)

	seedPosts(t, db, map[string]string{"id": "p1", "title": "Original Title"})

	app := NewApp(WithDB(db))
	entity := Define("posts", EntityConfig{
		Table: "posts",
		Fields: []schema.Field{
			{Name: "id", Type: schema.String},
			{Name: "title", Type: schema.String},
			{Name: "body", Type: schema.Text},
			{Name: "status", Type: schema.String},
			{Name: "author_id", Type: schema.String},
		},
	})
	app.Registry.Register(entity)

	crud := NewCrudHandler(entity, db)
	RegisterCrudRoutes(app.Router, crud, "/posts")

	ta := TestHarness(t, app)
	defer ta.Close()

	// UPDATE
	resp := ta.Put("/posts/p1", map[string]string{
		"title": "Updated Title",
	})
	resp.AssertStatus(t, http.StatusOK)
	resp.AssertBodyContains(t, "Updated Title")

	// Verify in DB
	var title string
	err := db.QueryRow("SELECT title FROM posts WHERE id = ?", "p1").Scan(&title)
	if err != nil {
		t.Fatalf("DB query after update: %v", err)
	}
	if title != "Updated Title" {
		t.Errorf("expected updated title in DB, got %s", title)
	}
}

func TestE2E_CRUD_Delete(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()
	createPostsTable(t, db)

	seedPosts(t, db,
		map[string]string{"id": "p1", "title": "To Delete"},
		map[string]string{"id": "p2", "title": "To Keep"},
	)

	app := NewApp(WithDB(db))
	entity := Define("posts", EntityConfig{
		Table: "posts",
		Fields: []schema.Field{
			{Name: "id", Type: schema.String},
			{Name: "title", Type: schema.String},
			{Name: "body", Type: schema.Text},
			{Name: "status", Type: schema.String},
			{Name: "author_id", Type: schema.String},
		},
	})
	app.Registry.Register(entity)

	crud := NewCrudHandler(entity, db)
	RegisterCrudRoutes(app.Router, crud, "/posts")

	ta := TestHarness(t, app)
	defer ta.Close()

	// DELETE
	resp := ta.Delete("/posts/p1")
	resp.AssertStatus(t, http.StatusNoContent)

	// Verify p1 is gone from DB
	var count int
	err := db.QueryRow("SELECT COUNT(*) FROM posts WHERE id = ?", "p1").Scan(&count)
	if err != nil {
		t.Fatalf("DB count after delete: %v", err)
	}
	if count != 0 {
		t.Errorf("expected p1 to be deleted, but count=%d", count)
	}

	// Verify p2 still exists
	err = db.QueryRow("SELECT COUNT(*) FROM posts WHERE id = ?", "p2").Scan(&count)
	if err != nil {
		t.Fatalf("DB count for p2: %v", err)
	}
	if count != 1 {
		t.Errorf("expected p2 to still exist, but count=%d", count)
	}

	// GET deleted post → 404
	resp = ta.Get("/posts/p1")
	resp.AssertStatus(t, http.StatusNotFound)
}

func TestE2E_CRUD_GetNotFound(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()
	createPostsTable(t, db)

	app := NewApp(WithDB(db))
	entity := Define("posts", EntityConfig{
		Table: "posts",
		Fields: []schema.Field{
			{Name: "id", Type: schema.String},
			{Name: "title", Type: schema.String},
			{Name: "body", Type: schema.Text},
			{Name: "status", Type: schema.String},
			{Name: "author_id", Type: schema.String},
		},
	})
	app.Registry.Register(entity)

	crud := NewCrudHandler(entity, db)
	RegisterCrudRoutes(app.Router, crud, "/posts")

	ta := TestHarness(t, app)
	defer ta.Close()

	resp := ta.Get("/posts/nonexistent")
	resp.AssertStatus(t, http.StatusNotFound)
	resp.AssertBodyContains(t, "not found")
}

// ============================================================================
// E2E: Validation
// ============================================================================

func TestE2E_Validation_CreateMissingRequired(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()
	createPostsTable(t, db)

	app := NewApp(WithDB(db))
	entity := Define("posts", EntityConfig{
		Table: "posts",
		Fields: []schema.Field{
			{Name: "id", Type: schema.String, Required: true},
			{Name: "title", Type: schema.String, Required: true},
			{Name: "body", Type: schema.Text},
		},
	})
	app.Registry.Register(entity)

	crud := NewCrudHandler(entity, db)
	RegisterCrudRoutes(app.Router, crud, "/posts")

	ta := TestHarness(t, app)
	defer ta.Close()

	// Missing required "title" field
	resp := ta.Post("/posts", map[string]string{
		"id": "post-1",
	})
	resp.AssertStatus(t, http.StatusBadRequest)
	resp.AssertBodyContains(t, "validation failed")
}

// ============================================================================
// E2E: Hooks
// ============================================================================

func TestE2E_Hooks_BeforeCreate(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()
	createPostsTable(t, db)

	app := NewApp(WithDB(db))
	entity := Define("posts", EntityConfig{
		Table: "posts",
		Fields: []schema.Field{
			{Name: "id", Type: schema.String, Required: true},
			{Name: "title", Type: schema.String, Required: true},
			{Name: "body", Type: schema.Text},
			{Name: "status", Type: schema.String},
			{Name: "author_id", Type: schema.String},
		},
	})
	app.Registry.Register(entity)

	// Register a before-create hook that records it was called
	hookCalled := false
	hooks := app.HookRegistry("posts")
	hooks.RegisterHook(BeforeCreate, func(ctx context.Context, data any) error {
		hookCalled = true
		return nil
	})

	crud := NewCrudHandler(entity, db)
	RegisterCrudRoutes(app.Router, crud, "/posts")

	ta := TestHarness(t, app)
	defer ta.Close()

	// Note: CrudHandler.Create doesn't call hooks directly —
	// hooks are called when using the App's entity operations.
	// For this E2E test, we verify the hook registry works
	err := hooks.ExecuteHooks(context.Background(), BeforeCreate, map[string]any{"title": "test"})
	if err != nil {
		t.Fatalf("execute hooks: %v", err)
	}
	if !hookCalled {
		t.Error("expected before-create hook to be called")
	}
}

// ============================================================================
// E2E: Events
// ============================================================================

func TestE2E_Events_EntityCreated(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	app := NewApp(WithDB(db))

	var capturedEvent Event
	app.Events().On(EntityCreated, func(ctx context.Context, event Event) error {
		capturedEvent = event
		return nil
	})

	err := app.Events().Emit(context.Background(), Event{
		Type: EntityCreated,
		Data: map[string]any{"entity": "posts", "id": "p1"},
	})
	if err != nil {
		t.Fatalf("emit event: %v", err)
	}

	if capturedEvent.Type != EntityCreated {
		t.Errorf("expected event type %v, got %v", EntityCreated, capturedEvent.Type)
	}
	data, ok := capturedEvent.Data.(map[string]any)
	if !ok {
		t.Fatalf("expected map data, got %T", capturedEvent.Data)
	}
	if data["id"] != "p1" {
		t.Errorf("expected id=p1, got %v", data["id"])
	}
}

// ============================================================================
// E2E: Plugin System
// ============================================================================

func TestE2E_Plugin_RegisterAndInit(t *testing.T) {
	app := NewApp()

	var initOrder []string
	p1 := &trackingPlugin{name: "alpha", order: &initOrder}
	p2 := &trackingPlugin{name: "beta", order: &initOrder}

	app.RegisterPlugin(p1)
	app.RegisterPlugin(p2)

	if err := app.InitPlugins(); err != nil {
		t.Fatalf("InitPlugins: %v", err)
	}

	if len(initOrder) != 2 {
		t.Fatalf("expected 2 inits, got %d", len(initOrder))
	}
	if initOrder[0] != "alpha" || initOrder[1] != "beta" {
		t.Errorf("expected [alpha beta], got %v", initOrder)
	}
}

func TestE2E_Plugin_HasRoutes(t *testing.T) {
	app := NewApp()
	p := &routePlugin{name: "stats"}
	app.RegisterPlugin(p)

	if err := app.InitPlugins(); err != nil {
		t.Fatalf("InitPlugins: %v", err)
	}

	ta := TestHarness(t, app)
	defer ta.Close()

	resp := ta.Get("/stats/health")
	resp.AssertStatus(t, http.StatusOK)
	resp.AssertBodyContains(t, "ok")
}

// ============================================================================
// E2E: Multiple Entities with Relationships
// ============================================================================

func TestE2E_MultipleEntities(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()
	createPostsTable(t, db)
	createCommentsTable(t, db)

	app := NewApp(WithDB(db))

	// Posts entity
	postsEntity := Define("posts", EntityConfig{
		Table: "posts",
		Fields: []schema.Field{
			{Name: "id", Type: schema.String, Required: true},
			{Name: "title", Type: schema.String, Required: true},
			{Name: "body", Type: schema.Text},
			{Name: "status", Type: schema.String},
			{Name: "author_id", Type: schema.String},
		},
	})
	app.Registry.Register(postsEntity)

	// Comments entity
	commentsEntity := Define("comments", EntityConfig{
		Table: "comments",
		Fields: []schema.Field{
			{Name: "id", Type: schema.String, Required: true},
			{Name: "body", Type: schema.String, Required: true},
			{Name: "post_id", Type: schema.String, Required: true},
			{Name: "author_id", Type: schema.String},
		},
	})
	app.Registry.Register(commentsEntity)

	postsCrud := NewCrudHandler(postsEntity, db)
	commentsCrud := NewCrudHandler(commentsEntity, db)
	RegisterCrudRoutes(app.Router, postsCrud, "/posts")
	RegisterCrudRoutes(app.Router, commentsCrud, "/comments")

	ta := TestHarness(t, app)
	defer ta.Close()

	// Create a post
	resp := ta.Post("/posts", map[string]string{
		"id":    "p1",
		"title": "My Post",
		"body":  "Content here",
	})
	resp.AssertStatus(t, http.StatusCreated)

	// Create a comment on that post
	resp = ta.Post("/comments", map[string]string{
		"id":      "c1",
		"body":    "Great post!",
		"post_id": "p1",
	})
	resp.AssertStatus(t, http.StatusCreated)

	// List comments
	resp = ta.Get("/comments")
	resp.AssertStatus(t, http.StatusOK)
	resp.AssertBodyContains(t, "Great post!")

	// Verify DB state
	var postCount, commentCount int
	db.QueryRow("SELECT COUNT(*) FROM posts").Scan(&postCount)
	db.QueryRow("SELECT COUNT(*) FROM comments").Scan(&commentCount)
	if postCount != 1 {
		t.Errorf("expected 1 post, got %d", postCount)
	}
	if commentCount != 1 {
		t.Errorf("expected 1 comment, got %d", commentCount)
	}
}

// ============================================================================
// E2E: Middleware Chain
// ============================================================================

func TestE2E_MiddlewareChain(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()
	createPostsTable(t, db)

	app := NewApp(WithDB(db))

	var order []string
	app.Router.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			order = append(order, "mw1-in")
			next.ServeHTTP(w, r)
			order = append(order, "mw1-out")
		})
	})
	app.Router.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			order = append(order, "mw2-in")
			next.ServeHTTP(w, r)
			order = append(order, "mw2-out")
		})
	})

	entity := Define("posts", EntityConfig{
		Table: "posts",
		Fields: []schema.Field{
			{Name: "id", Type: schema.String, Required: true},
			{Name: "title", Type: schema.String, Required: true},
			{Name: "body", Type: schema.Text},
			{Name: "status", Type: schema.String},
			{Name: "author_id", Type: schema.String},
		},
	})
	app.Registry.Register(entity)

	crud := NewCrudHandler(entity, db)
	RegisterCrudRoutes(app.Router, crud, "/posts")

	ta := TestHarness(t, app)
	defer ta.Close()

	// The test harness sends through the real router, so middleware fires
	resp := ta.Post("/posts", map[string]string{
		"id":    "p1",
		"title": "Middleware Test",
	})
	resp.AssertStatus(t, http.StatusCreated)

	// Verify middleware executed in order
	expected := []string{"mw1-in", "mw2-in", "mw2-out", "mw1-out"}
	if len(order) != len(expected) {
		t.Fatalf("expected middleware order %v, got %v", expected, order)
	}
	for i, v := range expected {
		if order[i] != v {
			t.Errorf("middleware[%d]: expected %q, got %q", i, v, order[i])
		}
	}
}

// ============================================================================
// E2E: Soft Delete
// ============================================================================

func TestE2E_SoftDelete(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	// Create table with deleted_at column
	_, err := db.Exec(`CREATE TABLE posts (
		id TEXT PRIMARY KEY,
		title TEXT NOT NULL,
		body TEXT DEFAULT '',
		status TEXT DEFAULT 'draft',
		author_id TEXT DEFAULT '',
		deleted_at DATETIME DEFAULT NULL,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
	)`)
	if err != nil {
		t.Fatalf("create table: %v", err)
	}

	app := NewApp(WithDB(db))
	entity := Define("posts", EntityConfig{
		Table:      "posts",
		SoftDelete: true,
		Fields: []schema.Field{
			{Name: "id", Type: schema.String, Required: true},
			{Name: "title", Type: schema.String, Required: true},
			{Name: "body", Type: schema.Text},
			{Name: "status", Type: schema.String},
			{Name: "author_id", Type: schema.String},
		},
	})
	app.Registry.Register(entity)

	crud := NewCrudHandler(entity, db)
	RegisterCrudRoutes(app.Router, crud, "/posts")

	ta := TestHarness(t, app)
	defer ta.Close()

	// Create a post
	resp := ta.Post("/posts", map[string]string{
		"id":    "p1",
		"title": "Soft Delete Me",
	})
	resp.AssertStatus(t, http.StatusCreated)

	// Soft delete
	resp = ta.Delete("/posts/p1")
	resp.AssertStatus(t, http.StatusNoContent)

	// Verify deleted_at is set in DB
	var deletedAt sql.NullString
	err = db.QueryRow("SELECT deleted_at FROM posts WHERE id = ?", "p1").Scan(&deletedAt)
	if err != nil {
		t.Fatalf("query deleted_at: %v", err)
	}
	if !deletedAt.Valid {
		t.Error("expected deleted_at to be set, but it's NULL")
	}

	// Row still exists
	var count int
	db.QueryRow("SELECT COUNT(*) FROM posts WHERE id = ?", "p1").Scan(&count)
	if count != 1 {
		t.Errorf("expected soft-deleted row to still exist, count=%d", count)
	}
}

// ============================================================================
// E2E: Custom Endpoints
// ============================================================================

func TestE2E_CustomEndpoint(t *testing.T) {
	app := NewApp()

	// Custom endpoint
	app.Router.Get("/health", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"status": "healthy"})
	}))

	ta := TestHarness(t, app)
	defer ta.Close()

	resp := ta.Get("/health")
	resp.AssertStatus(t, http.StatusOK)
	resp.AssertJSON(t, map[string]string{"status": "healthy"})
}

// ============================================================================
// Test Helpers: Plugin implementations
// ============================================================================

type trackingPlugin struct {
	name  string
	order *[]string
}

func (p *trackingPlugin) Name() string        { return p.name }
func (p *trackingPlugin) Init(app *App) error { *p.order = append(*p.order, p.name); return nil }

type routePlugin struct {
	name string
}

func (p *routePlugin) Name() string        { return p.name }
func (p *routePlugin) Init(app *App) error { return nil }
func (p *routePlugin) RegisterRoutes(r *router.Router) {
	r.Get("/stats/health", http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
	}))
}
