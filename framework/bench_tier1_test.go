package framework

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
)

// ============================================================================
// Tier 1: claims-defending end-to-end benchmarks (dual-DB)
//
// These map directly to README assertions. Numbers either back up the
// pitch or expose holes, both are useful.
// ============================================================================

// setupBlogDomain creates a posts/authors/comments tri-table domain on the
// given DB and seeds it with the requested counts. Returns the app and
// router ready for HTTP-style benchmarks.
func setupBlogDomain(b *testing.B, db *sql.DB, numPosts, commentsPerPost int) *App {
	b.Helper()
	ctx := context.Background()

	// DDL, portable between SQLite and Postgres. Include timestamps so the
	// framework's default `Timestamps: true` doesn't reference missing columns.
	for _, ddl := range []string{
		`CREATE TABLE IF NOT EXISTS authors (
			id         TEXT PRIMARY KEY,
			name       TEXT NOT NULL,
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
			updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE TABLE IF NOT EXISTS posts (
			id         TEXT PRIMARY KEY,
			title      TEXT NOT NULL,
			body       TEXT DEFAULT '',
			status     TEXT DEFAULT 'draft',
			author_id  TEXT NOT NULL DEFAULT '',
			views      INTEGER DEFAULT 0,
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
			updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE TABLE IF NOT EXISTS comments (
			id         TEXT PRIMARY KEY,
			body       TEXT NOT NULL,
			post_id    TEXT NOT NULL,
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
			updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
		)`,
	} {
		if _, err := db.ExecContext(ctx, ddl); err != nil {
			b.Fatalf("ddl: %v", err)
		}
	}

	// Seed authors (one author per ~10 posts so the BelongsTo join is meaningful).
	numAuthors := numPosts/10 + 1
	for i := range numAuthors {
		if _, err := db.ExecContext(ctx,
			"INSERT INTO authors (id, name) VALUES ($1, $2)",
			fmt.Sprintf("a%d", i), fmt.Sprintf("Author %d", i)); err != nil {
			b.Fatalf("seed authors: %v", err)
		}
	}

	// Seed posts.
	for i := range numPosts {
		status := "published"
		if i%5 == 0 {
			status = "draft"
		}
		if _, err := db.ExecContext(ctx,
			"INSERT INTO posts (id, title, body, status, author_id, views) VALUES ($1, $2, $3, $4, $5, $6)",
			fmt.Sprintf("p%d", i),
			fmt.Sprintf("Post %d", i),
			"lorem ipsum dolor sit amet",
			status,
			fmt.Sprintf("a%d", i%numAuthors),
			i*3,
		); err != nil {
			b.Fatalf("seed post %d: %v", i, err)
		}
	}

	// Seed comments.
	for p := range numPosts {
		for c := range commentsPerPost {
			if _, err := db.ExecContext(ctx,
				"INSERT INTO comments (id, body, post_id) VALUES ($1, $2, $3)",
				fmt.Sprintf("c%d_%d", p, c),
				fmt.Sprintf("Comment %d on post %d", c, p),
				fmt.Sprintf("p%d", p),
			); err != nil {
				b.Fatalf("seed comment p%d_c%d: %v", p, c, err)
			}
		}
	}

	app := NewApp(WithDB(db), WithoutDefaultMiddleware())

	posts := Define("posts", EntityConfig{
		Table: "posts",
		Fields: []schema.Field{
			{Name: "title", Type: schema.String, Required: true},
			{Name: "body", Type: schema.Text},
			{Name: "status", Type: schema.String, Default: "draft"},
			{Name: "author_id", Type: schema.String},
			{Name: "views", Type: schema.Int, Default: 0},
		},
		Relations: []Relation{
			BelongsTo("author", "authors", "author_id"),
			HasMany("comments", "comments", "post_id"),
		},
	})
	authors := Define("authors", EntityConfig{
		Table: "authors",
		Fields: []schema.Field{
			{Name: "name", Type: schema.String, Required: true},
		},
	})
	comments := Define("comments", EntityConfig{
		Table: "comments",
		Fields: []schema.Field{
			{Name: "body", Type: schema.Text, Required: true},
			{Name: "post_id", Type: schema.String},
		},
	})
	app.Registry.Register(posts)
	app.Registry.Register(authors)
	app.Registry.Register(comments)

	benchMountCrud(b, app, posts, "/posts")
	benchMountCrud(b, app, authors, "/authors")
	benchMountCrud(b, app, comments, "/comments")
	return app
}

// benchMountCrud mounts ent's CRUD routes at path through App.CrudHandler, so
// the handler carries the wiring a real app gives it (the registry that
// ?include= resolves against, JSON casing, hooks, events) rather than the
// bare crud.NewCrudHandler defaults.
func benchMountCrud(b *testing.B, app *App, ent *entity.Entity, path string) {
	b.Helper()
	ch, err := app.CrudHandler(ent.GetName())
	if err != nil {
		b.Fatalf("crud handler %s: %v", ent.GetName(), err)
	}
	RegisterCrudRoutes(app.Router(), ch, path)
}

// ----------------------------------------------------------------------------
// 1.1. Includes vs N+1: the "no N+1, one query per relation per level" claim
// ----------------------------------------------------------------------------

// BenchmarkTier1_IncludesVsN1 compares:
//   - GET /posts?limit=N&include=author,comments: the framework's eager-load.
//   - GET /posts?limit=N (then per-row author + comments fetches): naive N+1.
//
// The framework should win by an O(rows) factor at meaningful page sizes.
func BenchmarkTier1_IncludesVsN1(b *testing.B) {
	forEachBenchDialect(b, func(b *testing.B, db *sql.DB, _ Dialect) {
		const commentsPerPost = 5
		app := setupBlogDomain(b, db, 200, commentsPerPost)

		for _, limit := range []int{20, 100} {
			b.Run(fmt.Sprintf("eager-include/limit=%d", limit), func(b *testing.B) {
				path := fmt.Sprintf("/posts?limit=%d&include=author,comments", limit)
				req := benchAuthed(b, http.MethodGet, path, nil)
				b.ResetTimer()
				for i := 0; i < b.N; i++ {
					rec := httptest.NewRecorder()
					app.Router().ServeHTTP(rec, req)
					if rec.Code != http.StatusOK {
						b.Fatalf("status %d: %s", rec.Code, rec.Body.String())
					}
				}
			})

			b.Run(fmt.Sprintf("naive-n+1/limit=%d", limit), func(b *testing.B) {
				listPath := fmt.Sprintf("/posts?limit=%d", limit)
				listReq := benchAuthed(b, http.MethodGet, listPath, nil)
				b.ResetTimer()
				for i := 0; i < b.N; i++ {
					rec := httptest.NewRecorder()
					app.Router().ServeHTTP(rec, listReq)
					if rec.Code != http.StatusOK {
						b.Fatalf("list: %d", rec.Code)
					}
					var list struct {
						Data []map[string]any `json:"data"`
					}
					if err := json.Unmarshal(rec.Body.Bytes(), &list); err != nil {
						b.Fatalf("decode list: %v", err)
					}
					// Naive: one GET /authors/{id} + one GET /comments?post_id={id} per row.
					for _, row := range list.Data {
						authorID, _ := row["authorId"].(string)
						if authorID == "" {
							authorID, _ = row["author_id"].(string)
						}
						if authorID != "" {
							authReq := benchAuthed(b, http.MethodGet, "/authors/"+authorID, nil)
							rec := httptest.NewRecorder()
							app.Router().ServeHTTP(rec, authReq)
						}
						postID, _ := row["id"].(string)
						if postID != "" {
							commReq := benchAuthed(b, http.MethodGet, "/comments?post_id="+postID, nil)
							rec := httptest.NewRecorder()
							app.Router().ServeHTTP(rec, commReq)
						}
					}
				}
			})
		}
	})
}

// ----------------------------------------------------------------------------
// 1.2: Cursor pagination at depth vs offset
// ----------------------------------------------------------------------------

// BenchmarkTier1_PaginationDepth compares offset and cursor pagination at
// the same page depth. Offset should degrade linearly with depth; cursor
// should stay flat.
func BenchmarkTier1_PaginationDepth(b *testing.B) {
	forEachBenchDialect(b, func(b *testing.B, db *sql.DB, _ Dialect) {
		const N = 10000
		app := setupBlogDomain(b, db, N, 0)

		const pageSize = 50

		// Walk the cursor chain once, outside the timer, to the cursor that
		// opens deepPage, so cursor/page=180 times the same depth as
		// offset/page=180. Fetching page k yields the cursor for page k+1.
		const deepPage = 180
		var deepCursor string
		{
			path := fmt.Sprintf("/posts?cursor=&limit=%d", pageSize)
			req := benchAuthed(b, http.MethodGet, path, nil)
			for range deepPage - 1 {
				rec := httptest.NewRecorder()
				app.Router().ServeHTTP(rec, req)
				var page struct {
					Cursor string `json:"cursor"`
				}
				_ = json.Unmarshal(rec.Body.Bytes(), &page)
				if page.Cursor == "" {
					break
				}
				deepCursor = page.Cursor
				req = benchAuthed(b, http.MethodGet,
					fmt.Sprintf("/posts?cursor=%s&limit=%d", deepCursor, pageSize), nil)
			}
		}

		b.Run("offset/page=1", func(b *testing.B) {
			req := benchAuthed(b, http.MethodGet,
				fmt.Sprintf("/posts?page=1&limit=%d", pageSize), nil)
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				rec := httptest.NewRecorder()
				app.Router().ServeHTTP(rec, req)
				if rec.Code != http.StatusOK {
					b.Fatalf("status %d", rec.Code)
				}
			}
		})

		b.Run("offset/page=180", func(b *testing.B) {
			req := benchAuthed(b, http.MethodGet,
				fmt.Sprintf("/posts?page=180&limit=%d", pageSize), nil)
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				rec := httptest.NewRecorder()
				app.Router().ServeHTTP(rec, req)
				if rec.Code != http.StatusOK {
					b.Fatalf("status %d", rec.Code)
				}
			}
		})

		b.Run("cursor/page=1", func(b *testing.B) {
			req := benchAuthed(b, http.MethodGet,
				fmt.Sprintf("/posts?cursor=&limit=%d", pageSize), nil)
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				rec := httptest.NewRecorder()
				app.Router().ServeHTTP(rec, req)
				if rec.Code != http.StatusOK {
					b.Fatalf("status %d", rec.Code)
				}
			}
		})

		if deepCursor != "" {
			b.Run("cursor/page=180", func(b *testing.B) {
				req := benchAuthed(b, http.MethodGet,
					fmt.Sprintf("/posts?cursor=%s&limit=%d", deepCursor, pageSize), nil)
				b.ResetTimer()
				for i := 0; i < b.N; i++ {
					rec := httptest.NewRecorder()
					app.Router().ServeHTTP(rec, req)
					if rec.Code != http.StatusOK {
						b.Fatalf("status %d", rec.Code)
					}
				}
			})
		}
	})
}

// ----------------------------------------------------------------------------
// 1.3: Batch endpoint vs N individual writes
// ----------------------------------------------------------------------------

// BenchmarkTier1_BatchVsN compares:
//   - POST /posts/_batch with 50 items   (one transaction, one HTTP call).
//   - 50 sequential POST /posts          (50 transactions, 50 HTTP calls).
//
// The batch path should win by both the per-tx overhead and the per-request
// overhead.
func BenchmarkTier1_BatchVsN(b *testing.B) {
	forEachBenchDialect(b, func(b *testing.B, db *sql.DB, _ Dialect) {
		app := setupBlogDomain(b, db, 0, 0)

		const batchSize = 50

		// Pre-build payload bodies so the benchmark doesn't pay for the
		// per-iter Marshal cost.
		buildBatch := func(start int) []byte {
			items := make([]map[string]any, batchSize)
			for i := range batchSize {
				items[i] = map[string]any{
					"title":  fmt.Sprintf("Bench post %d", start+i),
					"body":   "lorem ipsum",
					"status": "draft",
				}
			}
			body, _ := json.Marshal(map[string]any{"items": items})
			return body
		}
		single := func(idx int) []byte {
			body, _ := json.Marshal(map[string]any{
				"title": fmt.Sprintf("Bench post %d", idx),
				"body":  "lorem ipsum",
			})
			return body
		}

		b.Run("batch-50", func(b *testing.B) {
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				body := buildBatch(i * batchSize)
				req := benchAuthed(b, http.MethodPost, "/posts/_batch", bytes.NewReader(body))
				req.Header.Set("Content-Type", "application/json")
				rec := httptest.NewRecorder()
				app.Router().ServeHTTP(rec, req)
				if rec.Code >= 400 {
					b.Fatalf("batch failed: %d %s", rec.Code, rec.Body.String())
				}
			}
		})

		b.Run("n-individual-50", func(b *testing.B) {
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				for j := range batchSize {
					body := single(i*batchSize + j + 1_000_000) // disjoint IDs
					req := benchAuthed(b, http.MethodPost, "/posts", bytes.NewReader(body))
					req.Header.Set("Content-Type", "application/json")
					rec := httptest.NewRecorder()
					app.Router().ServeHTTP(rec, req)
					if rec.Code >= 400 {
						b.Fatalf("single failed: %d %s", rec.Code, rec.Body.String())
					}
				}
			}
		})
	})
}

// ----------------------------------------------------------------------------
// 1.4: Streaming JSON vs buffered for large result sets
// ----------------------------------------------------------------------------

// BenchmarkTier1_StreamingVsBuffered compares the buffered list response
// (default) against the streaming JSON response (?stream=true).
//
// Caveat: parsePagination clamps `?limit=` to ≤100 (MaxPageSize), so the
// `?limit=N` knob can't push the page large enough for streaming to show
// its memory + first-byte advantage. This benchmark therefore measures the
// per-row encode/write overhead at the max client-controllable page size
// of 100, not the streaming surface's full intended workload. The auto-
// stream trigger (perPage ≥ streamListThreshold = 1000) is unreachable
// through the documented HTTP surface, flagged as a feature gap.
func BenchmarkTier1_StreamingVsBuffered(b *testing.B) {
	forEachBenchDialect(b, func(b *testing.B, db *sql.DB, _ Dialect) {
		const N = 5000
		app := setupBlogDomain(b, db, N, 0)

		const limit = 100 // ceiling enforced by parsePagination

		b.Run("buffered", func(b *testing.B) {
			req := benchAuthed(b, http.MethodGet,
				fmt.Sprintf("/posts?limit=%d", limit), nil)
			b.ResetTimer()
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				rec := httptest.NewRecorder()
				app.Router().ServeHTTP(rec, req)
				if rec.Code != http.StatusOK {
					b.Fatalf("status %d", rec.Code)
				}
				b.ReportMetric(float64(rec.Body.Len()), "response_bytes")
			}
		})

		b.Run("streaming", func(b *testing.B) {
			req := benchAuthed(b, http.MethodGet,
				fmt.Sprintf("/posts?limit=%d&stream=true", limit), nil)
			b.ResetTimer()
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				rec := httptest.NewRecorder()
				app.Router().ServeHTTP(rec, req)
				if rec.Code != http.StatusOK {
					b.Fatalf("status %d", rec.Code)
				}
				b.ReportMetric(float64(rec.Body.Len()), "response_bytes")
			}
		})
	})
}
