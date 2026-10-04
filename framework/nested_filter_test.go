package framework

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/crud"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/framework/filter"
)

// ============================================================================
// BelongsTo filter. ?author.name=Alice picks only posts whose author matches
// ============================================================================

func TestNestedFilter_BelongsTo(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *sql.DB, _ Dialect) {
		seedBlogDB(t, db)
		app := nestedBlogApp(t, db)
		ta := TestHarness(t, app).AsUser(struct{ ID string }{ID: "u1"})

		resp := ta.Get("/posts?author.name=Alice")
		resp.AssertStatus(t, http.StatusOK)
		var env crud.ListResponse
		if err := json.Unmarshal([]byte(resp.Body()), &env); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if env.Total != 1 {
			t.Fatalf("expected 1 post for Alice, got %d", env.Total)
		}
		if env.Data[0]["id"] != "p1" {
			t.Fatalf("expected p1, got %v", env.Data[0])
		}
	})
}

// ============================================================================
// BelongsTo filter with _like suffix
// ============================================================================

func TestNestedFilter_BelongsTo_Like(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *sql.DB, _ Dialect) {
		seedBlogDB(t, db)
		app := nestedBlogApp(t, db)
		ta := TestHarness(t, app).AsUser(struct{ ID string }{ID: "u1"})

		// `_like` is a literal substring at every depth now, the value
		// is escaped and wrapped in wildcards by the framework, so a
		// caller-supplied "%" matches a literal percent sign rather than
		// acting as a pattern. "Alice" is found by naming part of it.
		resp := ta.Get("/posts?author.name_like=" + url.QueryEscape("Ali"))
		resp.AssertStatus(t, http.StatusOK)
		var env crud.ListResponse
		json.Unmarshal([]byte(resp.Body()), &env)
		if env.Total != 1 {
			t.Fatalf("expected 1 post for like Ali, got %d", env.Total)
		}

		// The old raw-pattern spelling must now match nothing: there is
		// no author whose name literally contains "A%".
		raw := ta.Get("/posts?author.name_like=" + url.QueryEscape("A%"))
		raw.AssertStatus(t, http.StatusOK)
		var rawEnv crud.ListResponse
		json.Unmarshal([]byte(raw.Body()), &rawEnv)
		if rawEnv.Total != 0 {
			t.Fatalf("nested _like still treats %% as a wildcard: got %d posts", rawEnv.Total)
		}
	})
}

// ============================================================================
// HasMany filter. ?comments.body=nice picks parents with a matching child.
// No row duplication (EXISTS, not JOIN).
// ============================================================================

func TestNestedFilter_HasMany_NoDuplication(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *sql.DB, _ Dialect) {
		seedBlogDB(t, db)
		// Add another comment on p1 so the EXISTS would dup p1 twice under a
		// naive JOIN, but EXISTS returns p1 once.
		if _, err := db.Exec(
			"INSERT INTO comments(id, body, post_id) VALUES ($1, $2, $3)",
			"c3", "nice as well", "p1"); err != nil {
			t.Fatalf("seed extra: %v", err)
		}
		app := nestedBlogApp(t, db)
		ta := TestHarness(t, app).AsUser(struct{ ID string }{ID: "u1"})

		// Literal substring: no wildcards needed (or honoured).
		resp := ta.Get("/posts?comments.body_like=" + url.QueryEscape("nice"))
		resp.AssertStatus(t, http.StatusOK)
		var env crud.ListResponse
		json.Unmarshal([]byte(resp.Body()), &env)
		if env.Total != 1 {
			t.Fatalf("expected 1 post (no dup despite 2 matching comments), got %d", env.Total)
		}
	})
}

// ============================================================================
// ManyToMany filter. ?tags.name=go
// ============================================================================

func TestNestedFilter_ManyToMany(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *sql.DB, _ Dialect) {
		seedBlogDB(t, db)
		app := nestedBlogApp(t, db)
		ta := TestHarness(t, app).AsUser(struct{ ID string }{ID: "u1"})

		resp := ta.Get("/posts?tags.name=go")
		resp.AssertStatus(t, http.StatusOK)
		var env crud.ListResponse
		json.Unmarshal([]byte(resp.Body()), &env)
		if env.Total != 1 {
			t.Fatalf("expected 1 post tagged go, got %d", env.Total)
		}
		if env.Data[0]["id"] != "p1" {
			t.Fatalf("expected p1, got %v", env.Data[0])
		}
	})
}

// ============================================================================
// Unknown relation returns 400
// ============================================================================

func TestNestedFilter_UnknownRelation_400(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *sql.DB, _ Dialect) {
		seedBlogDB(t, db)
		app := nestedBlogApp(t, db)
		ta := TestHarness(t, app).AsUser(struct{ ID string }{ID: "u1"})

		resp := ta.Get("/posts?bogus.name=alice")
		resp.AssertStatus(t, http.StatusBadRequest).
			AssertBodyContains(t, "bogus")
	})
}

// ============================================================================
// Unknown field on the target returns 400
// ============================================================================

func TestNestedFilter_UnknownField_400(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *sql.DB, _ Dialect) {
		seedBlogDB(t, db)
		app := nestedBlogApp(t, db)
		ta := TestHarness(t, app).AsUser(struct{ ID string }{ID: "u1"})

		resp := ta.Get("/posts?author.does_not_exist=x")
		resp.AssertStatus(t, http.StatusBadRequest).
			AssertBodyContains(t, "does_not_exist")
	})
}

// ============================================================================
// Multi-level paths: 2-hop and 3-hop recursive EXISTS subqueries
// ============================================================================

func TestNestedFilter_MultiHop_2Hops(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *sql.DB, _ Dialect) {
		seedBlogDB(t, db)
		app := nestedBlogApp(t, db)
		ta := TestHarness(t, app).AsUser(struct{ ID string }{ID: "u1"})

		// 2 hops: posts -> author (users) -> profile (profiles)
		resp := ta.Get("/posts?author.profile.bio_like=" + url.QueryEscape("Alice"))
		resp.AssertStatus(t, http.StatusOK)
		var env crud.ListResponse
		if err := json.Unmarshal([]byte(resp.Body()), &env); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if env.Total != 1 {
			t.Fatalf("expected 1 post matching author.profile.bio_like=Alice, got %d", env.Total)
		}
		if env.Data[0]["id"] != "p1" {
			t.Fatalf("expected p1, got %v", env.Data[0])
		}

		// Non-matching query returns 0
		respNone := ta.Get("/posts?author.profile.bio_like=" + url.QueryEscape("Bob"))
		respNone.AssertStatus(t, http.StatusOK)
		var envNone crud.ListResponse
		json.Unmarshal([]byte(respNone.Body()), &envNone)
		if envNone.Total != 0 {
			t.Fatalf("expected 0 posts for Bob's profile, got %d", envNone.Total)
		}

		// 2 hops: comments -> post (posts) -> author (users)
		respComments := ta.Get("/comments?post.author.name=Alice")
		respComments.AssertStatus(t, http.StatusOK)
		var envComments crud.ListResponse
		json.Unmarshal([]byte(respComments.Body()), &envComments)
		if envComments.Total != 2 {
			t.Fatalf("expected 2 comments on Alice's posts, got %d", envComments.Total)
		}
	})
}

func TestNestedFilter_MultiHop_3Hops(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *sql.DB, _ Dialect) {
		seedBlogDB(t, db)
		app := nestedBlogApp(t, db)
		ta := TestHarness(t, app).AsUser(struct{ ID string }{ID: "u1"})

		// 3 hops: comments -> post (posts) -> author (users) -> profile (profiles)
		resp := ta.Get("/comments?post.author.profile.bio_like=" + url.QueryEscape("Alice"))
		resp.AssertStatus(t, http.StatusOK)
		var env crud.ListResponse
		if err := json.Unmarshal([]byte(resp.Body()), &env); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if env.Total != 2 {
			t.Fatalf("expected 2 comments for 3-hop author.profile.bio_like=Alice, got %d", env.Total)
		}

		respNone := ta.Get("/comments?post.author.profile.bio_like=" + url.QueryEscape("Nonexistent"))
		respNone.AssertStatus(t, http.StatusOK)
		var envNone crud.ListResponse
		json.Unmarshal([]byte(respNone.Body()), &envNone)
		if envNone.Total != 0 {
			t.Fatalf("expected 0 comments, got %d", envNone.Total)
		}
	})
}

func TestNestedFilter_ExceedsMaxDepth_400(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *sql.DB, _ Dialect) {
		seedBlogDB(t, db)
		app := nestedBlogApp(t, db)
		ta := TestHarness(t, app).AsUser(struct{ ID string }{ID: "u1"})

		resp := ta.Get("/posts?a.b.c.d.e.name=x")
		resp.AssertStatus(t, http.StatusBadRequest).
			AssertBodyContains(t, "too many relation hops")
	})
}

// ============================================================================
// Composes with top-level filters: ?author.name=Alice&title_like=First
// ============================================================================

func TestNestedFilter_ComposesWithTopLevel(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *sql.DB, _ Dialect) {
		seedBlogDB(t, db)
		app := nestedBlogApp(t, db)
		ta := TestHarness(t, app).AsUser(struct{ ID string }{ID: "u1"})

		// _like is a literal "contains" (caller wildcards are escaped), so
		// the substring "Fir" matches a title like "First …".
		resp := ta.Get("/posts?author.name=Alice&title_like=" + url.QueryEscape("Fir"))
		resp.AssertStatus(t, http.StatusOK)
		var env crud.ListResponse
		json.Unmarshal([]byte(resp.Body()), &env)
		if env.Total != 1 {
			t.Fatalf("expected exactly 1 match for Alice+First, got %d", env.Total)
		}
	})
}

// ============================================================================
// Custom primary key on target entity in BelongsTo
// ============================================================================

func TestNestedFilter_CustomPrimaryKey(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *sql.DB, _ Dialect) {
		app := NewApp(WithDB(db), WithoutDefaultMiddleware())

		pubEnt := entity.Define("publishers", entity.EntityConfig{
			Table: "publishers",
			Fields: []schema.Field{
				{Name: "pub_code", Type: schema.String, Required: true},
				{Name: "name", Type: schema.String, Required: true},
			},
		}.WithTimestamps(false))
		pubEnt.PrimaryKey = "pub_code"
		app.Registry.Register(pubEnt)

		app.Entity("books", entity.EntityConfig{
			Table: "books",
			Fields: []schema.Field{
				{Name: "title", Type: schema.String, Required: true},
				{Name: "publisher_id", Type: schema.String, Required: true},
			},
			Relations: []entity.Relation{
				entity.BelongsTo("publisher", "publishers", "publisher_id"),
			},
		}.WithTimestamps(false))

		if err := AutoMigrate(db, app.Registry); err != nil {
			t.Fatalf("automigrate: %v", err)
		}

		if _, err := db.Exec("INSERT INTO publishers(pub_code, name) VALUES ('pub1', 'OReilly')"); err != nil {
			t.Fatalf("insert publisher: %v", err)
		}
		if _, err := db.Exec("INSERT INTO books(id, title, publisher_id) VALUES ('b1', 'Learning Go', 'pub1')"); err != nil {
			t.Fatalf("insert book: %v", err)
		}

		ta := TestHarness(t, app).AsUser(struct{ ID string }{ID: "u1"})
		resp := ta.Get("/books?publisher.name=OReilly")
		resp.AssertStatus(t, http.StatusOK)
		var env crud.ListResponse
		json.Unmarshal([]byte(resp.Body()), &env)
		if env.Total != 1 {
			t.Fatalf("expected 1 book for publisher OReilly, got %d", env.Total)
		}
		if env.Data[0]["id"] != "b1" {
			t.Fatalf("expected book b1, got %v", env.Data[0])
		}
	})
}

// TestNestedFilter_SelfReferencing tests filtering on a self-referencing relation (e.g. category -> parent category)
// where the parent table and relation table have the same name, verifying table aliasing prevents shadowing.
func TestNestedFilter_SelfReferencing(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *sql.DB, _ Dialect) {
		app := NewApp(WithDB(db), WithoutDefaultMiddleware())
		app.Entity("categories", entity.EntityConfig{
			Table: "categories",
			Fields: []schema.Field{
				{Name: "name", Type: schema.String, Required: true},
				{Name: "parent_id", Type: schema.String},
			},
			Relations: []entity.Relation{
				entity.BelongsTo("parent", "categories", "parent_id"),
			},
		}.WithTimestamps(false))

		if err := AutoMigrate(db, app.Registry); err != nil {
			t.Fatalf("automigrate: %v", err)
		}

		if _, err := db.Exec("INSERT INTO categories(id, name, parent_id) VALUES ('c1', 'Electronics', NULL)"); err != nil {
			t.Fatalf("insert root: %v", err)
		}
		if _, err := db.Exec("INSERT INTO categories(id, name, parent_id) VALUES ('c2', 'Laptops', 'c1')"); err != nil {
			t.Fatalf("insert child: %v", err)
		}
		if _, err := db.Exec("INSERT INTO categories(id, name, parent_id) VALUES ('c3', 'Apparel', NULL)"); err != nil {
			t.Fatalf("insert other root: %v", err)
		}

		ta := TestHarness(t, app).AsUser(struct{ ID string }{ID: "u1"})
		resp := ta.Get("/categories?parent.name=Electronics")
		resp.AssertStatus(t, http.StatusOK)
		var env crud.ListResponse
		if err := json.Unmarshal([]byte(resp.Body()), &env); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if env.Total != 1 {
			t.Fatalf("expected 1 category with parent Electronics, got %d", env.Total)
		}
		if env.Data[0]["id"] != "c2" {
			t.Fatalf("expected category c2, got %v", env.Data[0])
		}
	})
}

// TestNestedFilter_UnsupportedOp_ReturnsError asserts that in-process nested filters
// with unknown/unsupported operators return an error rather than silently defaulting to =.
func TestNestedFilter_UnsupportedOp_ReturnsError(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *sql.DB, _ Dialect) {
		seedBlogDB(t, db)
		app := nestedBlogApp(t, db)
		ch, err := app.CrudHandler("posts")
		if err != nil {
			t.Fatalf("app.CrudHandler: %v", err)
		}
		q := NewTypedQuery[struct {
			ID    string
			Title string
		}](ch).WhereNested(NestedFilter{
			Relation: "author",
			Field:    "name",
			Op:       filter.FilterOp("invalid_op"),
			Value:    "Alice",
		})
		_, err = q.Find(context.Background())
		if err == nil {
			t.Fatalf("expected error for unsupported operator, got nil")
		}
		if !strings.Contains(err.Error(), "unsupported operator") {
			t.Fatalf("expected unsupported operator error message, got: %v", err)
		}
	})
}
