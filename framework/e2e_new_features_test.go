package framework

import (
	"context"
	"database/sql"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gofastr/gofastr/core/schema"
	"github.com/gofastr/gofastr/framework/cron"
	"github.com/gofastr/gofastr/framework/entity"
)

// ============================================================================
// E2E coverage for the features added in this batch:
//   - composite cursor pagination
//   - nested filters (?author.name=Alice)
//   - scoped includes (?include=comments(status=draft))
//   - streaming JSON for large lists
//   - audit log (WithAuditLog)
//   - cron scheduler firing inside the app
//
// One app per dialect, multiple t.Run subtests so failures isolate.
// ============================================================================

// newFeaturesEnv wires the relevant features into a fresh app and starts a
// real httptest.Server so the codepath includes Go's HTTP stack — not just
// direct ServeHTTP.
type newFeaturesEnv struct {
	app     *App
	server  *httptest.Server
	client  *http.Client
	actor   *atomic.Value // string — read by the audit Actor func
	postIDs []string      // captured during seed; subtests reference these
}

func setupNewFeaturesE2E(t *testing.T, db *sql.DB) *newFeaturesEnv {
	t.Helper()

	// Tables: posts + authors + comments. Joined enough to exercise nested
	// filters across BelongsTo and HasMany.
	for _, ddl := range []string{
		`CREATE TABLE authors (
			id TEXT PRIMARY KEY,
			name TEXT NOT NULL
		)`,
		`CREATE TABLE posts (
			id TEXT PRIMARY KEY,
			title TEXT NOT NULL,
			author_id TEXT NOT NULL,
			created_at TEXT NOT NULL
		)`,
		`CREATE TABLE comments (
			id TEXT PRIMARY KEY,
			body TEXT NOT NULL,
			status TEXT DEFAULT 'published',
			post_id TEXT NOT NULL
		)`,
	} {
		if _, err := db.Exec(ddl); err != nil {
			t.Fatalf("create: %v", err)
		}
	}

	app := NewApp(WithDB(db), WithoutDefaultMiddleware())
	app.Entity("authors", entity.EntityConfig{
		Table: "authors",
		Fields: []schema.Field{
			{Name: "name", Type: schema.String, Required: true},
		},
	}.WithTimestamps(false))
	app.Entity("posts", entity.EntityConfig{
		Table: "posts",
		Fields: []schema.Field{
			{Name: "title", Type: schema.String, Required: true},
			{Name: "author_id", Type: schema.String, Required: true},
			{Name: "created_at", Type: schema.String, Required: true},
		},
		// Composite cursor: order by created_at then id so duplicate
		// timestamps still produce a stable page boundary.
		CursorFields: []string{"created_at"},
		Relations: []entity.Relation{
			entity.BelongsTo("author", "authors", "author_id"),
			entity.HasMany("comments", "comments", "post_id"),
		},
	}.WithTimestamps(false))
	app.Entity("comments", entity.EntityConfig{
		Table: "comments",
		Fields: []schema.Field{
			{Name: "body", Type: schema.String, Required: true},
			{Name: "status", Type: schema.String},
			{Name: "post_id", Type: schema.String, Required: true},
		},
	}.WithTimestamps(false))

	// Audit log over every entity. The Actor callback reads from an atomic
	// holder so subtests can set "who" without rebuilding the app.
	actor := &atomic.Value{}
	actor.Store("")
	app.WithAuditLog(AuditConfig{
		Actor: func(_ context.Context) string {
			if v, ok := actor.Load().(string); ok {
				return v
			}
			return ""
		},
	})

	srv := httptest.NewServer(app.Router)
	t.Cleanup(srv.Close)

	return &newFeaturesEnv{
		app:    app,
		server: srv,
		client: &http.Client{Timeout: 10 * time.Second},
		actor:  actor,
	}
}

func (e *newFeaturesEnv) get(t *testing.T, path string) (int, []byte) {
	t.Helper()
	resp, err := e.client.Get(e.server.URL + path)
	if err != nil {
		t.Fatalf("GET %s: %v", path, err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, body
}

func (e *newFeaturesEnv) postJSON(t *testing.T, path string, body any) (int, []byte) {
	t.Helper()
	buf, _ := json.Marshal(body)
	resp, err := e.client.Post(e.server.URL+path, "application/json", strings.NewReader(string(buf)))
	if err != nil {
		t.Fatalf("POST %s: %v", path, err)
	}
	defer resp.Body.Close()
	out, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, out
}

// ============================================================================
// The actual E2E run.
// ============================================================================

func TestE2E_NewFeatures(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *sql.DB, _ Dialect) {
		env := setupNewFeaturesE2E(t, db)

		// Seed authors + posts via the HTTP surface so the audit hook also
		// observes them (it's wired through CRUD).
		env.actor.Store("seeder")
		seedAuthorsAndPosts(t, env)

		// --------------------------------------------------------------
		// Nested filter: ?author.name=Alice picks Alice's posts only.
		// --------------------------------------------------------------
		t.Run("nested_filter_belongs_to", func(t *testing.T) {
			code, body := env.get(t, "/posts?author.name=Alice")
			if code != http.StatusOK {
				t.Fatalf("status %d: %s", code, body)
			}
			var resp ListResponse
			if err := json.Unmarshal(body, &resp); err != nil {
				t.Fatalf("decode: %v", err)
			}
			if resp.Total != 2 {
				t.Fatalf("expected Alice to own 2 posts, got %d (%s)", resp.Total, body)
			}
		})

		// --------------------------------------------------------------
		// Scoped include: ?include=comments(status=draft) only attaches
		// matching comments to each parent.
		// --------------------------------------------------------------
		t.Run("scoped_include_filters_children", func(t *testing.T) {
			// Two comments per post; one draft, one published.
			for _, postID := range env.postIDs[:2] {
				env.postJSON(t, "/comments", map[string]any{
					"body": "draft on " + postID, "status": "draft", "post_id": postID,
				})
				env.postJSON(t, "/comments", map[string]any{
					"body": "pub on " + postID, "status": "published", "post_id": postID,
				})
			}
			code, body := env.get(t, "/posts?include=comments(status=draft)&limit=10")
			if code != http.StatusOK {
				t.Fatalf("status %d: %s", code, body)
			}
			var resp ListResponse
			json.Unmarshal(body, &resp)
			for _, row := range resp.Data {
				kids, _ := row["comments"].([]any)
				for _, c := range kids {
					m := c.(map[string]any)
					if m["status"] != "draft" {
						t.Fatalf("expected draft-only comments, got %v on post %v", m["status"], row["id"])
					}
				}
			}
		})

		// --------------------------------------------------------------
		// Composite cursor: walk all posts ordered by created_at then id.
		// --------------------------------------------------------------
		t.Run("composite_cursor_walks_all_rows", func(t *testing.T) {
			seen := map[string]bool{}
			cursor := ""
			for i := 0; i < 10; i++ { // 10 iterations is enough; will break early
				code, body := env.get(t, "/posts?cursor="+cursor+"&limit=2")
				if code != http.StatusOK {
					t.Fatalf("page %d: %d %s", i, code, body)
				}
				var page struct {
					Data    []map[string]any `json:"data"`
					Cursor  string           `json:"cursor"`
					HasMore bool             `json:"hasMore"`
				}
				if err := json.Unmarshal(body, &page); err != nil {
					t.Fatalf("decode: %v", err)
				}
				for _, r := range page.Data {
					seen[r["id"].(string)] = true
				}
				if !page.HasMore {
					break
				}
				cursor = page.Cursor
			}
			if len(seen) < 3 {
				t.Fatalf("composite cursor missed rows, saw %d unique", len(seen))
			}
		})

		// --------------------------------------------------------------
		// Streaming JSON: ?stream=true keeps the standard envelope shape.
		// --------------------------------------------------------------
		t.Run("streaming_envelope", func(t *testing.T) {
			code, body := env.get(t, "/posts?stream=true&limit=100")
			if code != http.StatusOK {
				t.Fatalf("status %d: %s", code, body)
			}
			var env ListResponse
			if err := json.Unmarshal(body, &env); err != nil {
				t.Fatalf("decode (stream envelope must be valid JSON): %v\nbody: %s", err, body)
			}
			if env.Total < 3 {
				t.Fatalf("expected at least 3 posts streamed, got total=%d", env.Total)
			}
		})

		// --------------------------------------------------------------
		// Audit log: every write recorded with the active actor.
		// --------------------------------------------------------------
		t.Run("audit_records_writes", func(t *testing.T) {
			env.actor.Store("bob")
			env.postJSON(t, "/comments", map[string]any{
				"body": "audited", "post_id": env.postIDs[0],
			})
			var n int
			db.QueryRow(`SELECT COUNT(*) FROM audit_log WHERE actor_id = 'bob' AND op = 'create' AND entity = 'comments'`).Scan(&n)
			if n != 1 {
				t.Fatalf("expected 1 audit row for bob, got %d", n)
			}
		})

		// --------------------------------------------------------------
		// Generated client interop: a minimal hand-rolled equivalent of
		// the generated client hits the live server and gets typed data.
		// We verify the contract the generator depends on (envelope,
		// JSON tag shape) rather than running the generator itself.
		// --------------------------------------------------------------
		t.Run("client_envelope_contract", func(t *testing.T) {
			vals := url.Values{}
			vals.Set("limit", "5")
			code, body := env.get(t, "/posts?"+vals.Encode())
			if code != http.StatusOK {
				t.Fatalf("status %d: %s", code, body)
			}
			var resp struct {
				Data       []map[string]any `json:"data"`
				Total      int              `json:"total"`
				Page       int              `json:"page"`
				PerPage    int              `json:"perPage"`
				TotalPages int              `json:"totalPages"`
			}
			if err := json.Unmarshal(body, &resp); err != nil {
				t.Fatalf("decode: %v", err)
			}
			if resp.PerPage != 5 || resp.Page != 1 {
				t.Fatalf("envelope shape regressed: %+v", resp)
			}
		})
	})
}

// ============================================================================
// In-process cron driven by the framework. Separate test because it doesn't
// share state with the HTTP server.
// ============================================================================

func TestE2E_CronFiresInsideApp(t *testing.T) {
	s := cron.NewScheduler()
	var fired atomic.Int32
	// Match every minute so runOnce always triggers — we drive the tick
	// manually via runOnce to avoid waiting on the wall clock.
	if err := s.Register(cron.CronJob{
		Name: "tick",
		Spec: "* * * * *",
		Run: func(_ context.Context) error {
			fired.Add(1)
			return nil
		},
	}); err != nil {
		t.Fatal(err)
	}
	s.RunOnce(context.Background(), time.Now())
	deadline := time.Now().Add(time.Second)
	for fired.Load() == 0 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if fired.Load() != 1 {
		t.Fatalf("cron job should have fired exactly once, got %d", fired.Load())
	}
}

// ============================================================================
// Seed helper
// ============================================================================

func seedAuthorsAndPosts(t *testing.T, env *newFeaturesEnv) {
	t.Helper()
	// Authors. IDs are server-generated (AutoUUID), so we capture them from
	// the response and re-use them as author_id when creating posts.
	authorIDs := map[string]string{}
	for _, name := range []string{"Alice", "Bob"} {
		code, body := env.postJSON(t, "/authors", map[string]any{"name": name})
		if code != http.StatusCreated {
			t.Fatalf("seed author %s: %d %s", name, code, body)
		}
		var created map[string]any
		if err := json.Unmarshal(body, &created); err != nil {
			t.Fatalf("decode author: %v", err)
		}
		authorIDs[name] = created["id"].(string)
	}

	// Posts — Alice owns 2, Bob owns 1. created_at strictly increasing so
	// composite cursor walks them in a deterministic order.
	base := time.Date(2025, 4, 8, 9, 0, 0, 0, time.UTC)
	postIDs := []string{}
	for i, p := range []struct{ title, author string }{
		{"Alice 1", "Alice"},
		{"Bob 1", "Bob"},
		{"Alice 2", "Alice"},
	} {
		ts := base.Add(time.Duration(i) * time.Minute).Format(time.RFC3339)
		code, body := env.postJSON(t, "/posts", map[string]any{
			"title":      p.title,
			"author_id":  authorIDs[p.author],
			"created_at": ts,
		})
		if code != http.StatusCreated {
			t.Fatalf("seed post %s: %d %s", p.title, code, body)
		}
		var created map[string]any
		json.Unmarshal(body, &created)
		postIDs = append(postIDs, created["id"].(string))
	}
	// Stash the post IDs on the env so the scoped-include subtest can use
	// them when creating comments.
	env.postIDs = postIDs
}
