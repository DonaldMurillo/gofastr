package query

import (
	"strings"
	"testing"
)

// TestSelect_WhereClausesAreParenthesised pins the security fix: when a
// caller composes multiple WHERE conditions, each one MUST be wrapped
// in parens so SQL precedence can't let an OR in one clause break
// the AND of another.
//
// Without the fix:
//   AND visibility='public' OR author_id=$2 AND owner_id=$3
//   → (X AND visibility='public') OR (author_id=$2 AND owner_id=$3)
//   The owner_id scope only applies to the OR branch — public posts
//   from OTHER owners leak.
func TestSelect_WhereClausesAreParenthesised(t *testing.T) {
	qb := Select("id").From("posts")
	qb.Where("tenant_id = $1", "tenant-a")
	// This is what a host's BeforeList hook might add — a multi-
	// condition OR. Without parens the framework's tenant_id AND
	// is captured by the inner OR's right operand only.
	qb.Where("visibility = $1 OR author_id = $1", "public", "u-1")
	qb.Where("owner_id = $1", "u-1")

	sql, _ := qb.Build()
	t.Logf("SQL: %s", sql)

	// Each user-supplied condition must appear inside its own parens.
	if !strings.Contains(sql, "(tenant_id = $1)") {
		t.Errorf("tenant clause not parenthesised: %s", sql)
	}
	if !strings.Contains(sql, "(visibility = $2 OR author_id = $3)") {
		t.Errorf("OR clause not parenthesised — host hook can bypass framework scopes: %s", sql)
	}
	if !strings.Contains(sql, "(owner_id = $4)") {
		t.Errorf("owner clause not parenthesised: %s", sql)
	}
}

// TestCount_WhereClausesAreParenthesised mirrors the fix for the
// count builder used by List's total + typed_query.Count.
func TestCount_WhereClausesAreParenthesised(t *testing.T) {
	cb := Count("posts")
	cb.Where("tenant_id = $1", "tenant-a")
	cb.Where("visibility = $1 OR author_id = $1", "public", "u-1")

	sql, _ := cb.Build()
	if !strings.Contains(sql, "(tenant_id = $1)") || !strings.Contains(sql, "(visibility = $2 OR author_id = $3)") {
		t.Errorf("Count WHERE clauses not parenthesised: %s", sql)
	}
}

// TestUpdate_WhereClausesAreParenthesised — the UPDATE path is
// where the leak is most dangerous (cross-owner UPDATE).
func TestUpdate_WhereClausesAreParenthesised(t *testing.T) {
	ub := Update("posts").Set("title", "new")
	ub.Where("tenant_id = $1", "tenant-a")
	ub.Where("visibility = $1 OR author_id = $1", "public", "u-1")

	sql, _ := ub.Build()
	if !strings.Contains(sql, "(tenant_id = $2)") || !strings.Contains(sql, "(visibility = $3 OR author_id = $4)") {
		t.Errorf("Update WHERE clauses not parenthesised: %s", sql)
	}
}

// TestDelete_WhereClausesAreParenthesised — same for DELETE.
func TestDelete_WhereClausesAreParenthesised(t *testing.T) {
	db := Delete("posts")
	db.Where("tenant_id = $1", "tenant-a")
	db.Where("visibility = $1 OR author_id = $1", "public", "u-1")

	sql, _ := db.Build()
	if !strings.Contains(sql, "(tenant_id = $1)") || !strings.Contains(sql, "(visibility = $2 OR author_id = $3)") {
		t.Errorf("Delete WHERE clauses not parenthesised: %s", sql)
	}
}
