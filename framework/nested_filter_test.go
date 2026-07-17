package framework

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"net/url"
	"testing"

	"github.com/DonaldMurillo/gofastr/framework/crud"
)

// ============================================================================
// BelongsTo filter — ?author.name=Alice picks only posts whose author matches
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

		resp := ta.Get("/posts?author.name_like=" + url.QueryEscape("A%"))
		resp.AssertStatus(t, http.StatusOK)
		var env crud.ListResponse
		json.Unmarshal([]byte(resp.Body()), &env)
		if env.Total != 1 {
			t.Fatalf("expected 1 post for like A%%, got %d", env.Total)
		}
	})
}

// ============================================================================
// HasMany filter — ?comments.body=nice picks parents with a matching child.
// No row duplication (EXISTS, not JOIN).
// ============================================================================

func TestNestedFilter_HasMany_NoDuplication(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *sql.DB, _ Dialect) {
		seedBlogDB(t, db)
		// Add another comment on p1 so the EXISTS would dup p1 twice under a
		// naive JOIN — but EXISTS returns p1 once.
		if _, err := db.Exec(
			"INSERT INTO comments(id, body, post_id) VALUES ($1, $2, $3)",
			"c3", "nice as well", "p1"); err != nil {
			t.Fatalf("seed extra: %v", err)
		}
		app := nestedBlogApp(t, db)
		ta := TestHarness(t, app).AsUser(struct{ ID string }{ID: "u1"})

		resp := ta.Get("/posts?comments.body_like=" + url.QueryEscape("%nice%"))
		resp.AssertStatus(t, http.StatusOK)
		var env crud.ListResponse
		json.Unmarshal([]byte(resp.Body()), &env)
		if env.Total != 1 {
			t.Fatalf("expected 1 post (no dup despite 2 matching comments), got %d", env.Total)
		}
	})
}

// ============================================================================
// ManyToMany filter — ?tags.name=go
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
// Multi-level paths rejected (single-level only for now)
// ============================================================================

func TestNestedFilter_MultiLevel_400(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *sql.DB, _ Dialect) {
		seedBlogDB(t, db)
		app := nestedBlogApp(t, db)
		ta := TestHarness(t, app).AsUser(struct{ ID string }{ID: "u1"})

		resp := ta.Get("/posts?author.team.name=x")
		resp.AssertStatus(t, http.StatusBadRequest).
			AssertBodyContains(t, "multi-level")
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
