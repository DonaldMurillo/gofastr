package query

import (
	"strings"
	"testing"
)

func TestSelectWithWhere(t *testing.T) {
	sql, args := Select("id", "name").From("users").Where("age > $1", 21).Build()

	wantContains := "SELECT id, name FROM users WHERE (age > $1)"
	if !contains(sql, wantContains) {
		t.Errorf("SQL = %q, want to contain %q", sql, wantContains)
	}
	if len(args) != 1 || args[0].(int) != 21 {
		t.Errorf("args = %v, want [21]", args)
	}
}

func TestMultipleWhere(t *testing.T) {
	sql, args := Select("*").
		From("users").
		Where("age > $1", 21).
		Where("status = $1", "active").
		Build()

	if !contains(sql, "WHERE") {
		t.Errorf("SQL = %q, want WHERE clause", sql)
	}
	if !contains(sql, "AND") {
		t.Errorf("SQL = %q, want AND connector", sql)
	}
	if len(args) != 2 {
		t.Errorf("args = %v, want 2 args", args)
	}
	if args[0].(int) != 21 {
		t.Errorf("args[0] = %v, want 21", args[0])
	}
	if args[1].(string) != "active" {
		t.Errorf("args[1] = %v, want active", args[1])
	}

	// Verify placeholders are renumbered: $1 for age, $2 for status
	if !contains(sql, "$1") {
		t.Errorf("SQL = %q, want $1 placeholder", sql)
	}
	if !contains(sql, "$2") {
		t.Errorf("SQL = %q, want $2 placeholder", sql)
	}
}

func TestOrWhere(t *testing.T) {
	sql, args := Select("*").
		From("users").
		Where("role = $1", "admin").
		OrWhere("role = $1", "superadmin").
		Build()

	if !contains(sql, "OR") {
		t.Errorf("SQL = %q, want OR connector", sql)
	}
	if len(args) != 2 {
		t.Errorf("args = %v, want 2 args", args)
	}
}

func TestJoin(t *testing.T) {
	sql, args := Select("users.id", "orders.total").
		From("users").
		Join("orders", "orders.user_id = users.id").
		Where("users.active = $1", true).
		Build()

	if !contains(sql, "JOIN orders ON orders.user_id = users.id") {
		t.Errorf("SQL = %q, want JOIN clause", sql)
	}
	if len(args) != 1 || args[0].(bool) != true {
		t.Errorf("args = %v, want [true]", args)
	}
}

func TestLeftJoin(t *testing.T) {
	sql, _ := Select("users.name", "profiles.bio").
		From("users").
		LeftJoin("profiles", "profiles.user_id = users.id").
		Build()

	if !contains(sql, "LEFT JOIN profiles ON profiles.user_id = users.id") {
		t.Errorf("SQL = %q, want LEFT JOIN clause", sql)
	}
}

func TestOrderLimitOffset(t *testing.T) {
	sql, args := Select("*").
		From("users").
		Order("name", "ASC").
		Limit(10).
		Offset(20).
		Build()

	if !contains(sql, "ORDER BY name ASC") {
		t.Errorf("SQL = %q, want ORDER BY name ASC", sql)
	}
	if !contains(sql, "LIMIT $1") {
		t.Errorf("SQL = %q, want LIMIT $1", sql)
	}
	if !contains(sql, "OFFSET $2") {
		t.Errorf("SQL = %q, want OFFSET $2", sql)
	}
	if len(args) != 2 {
		t.Errorf("args = %v, want 2 args", args)
	}
	if args[0].(int) != 10 {
		t.Errorf("args[0] = %v, want 10", args[0])
	}
	if args[1].(int) != 20 {
		t.Errorf("args[1] = %v, want 20", args[1])
	}
}

func TestCursorForward(t *testing.T) {
	sql, args := Select("*").
		From("users").
		Cursor("id", 42, "forward").
		Limit(10).
		Build()

	if !contains(sql, "id > $1") {
		t.Errorf("SQL = %q, want id > $1 for forward cursor", sql)
	}
	if !contains(sql, "ORDER BY id") {
		t.Errorf("SQL = %q, want ORDER BY id", sql)
	}
	if len(args) != 2 {
		t.Errorf("args = %v, want 2 args (cursor value + limit)", args)
	}
}

func TestCursorBackward(t *testing.T) {
	sql, args := Select("*").
		From("users").
		Cursor("id", 100, "backward").
		Limit(10).
		Build()

	if !contains(sql, "id < $1") {
		t.Errorf("SQL = %q, want id < $1 for backward cursor", sql)
	}
	if args[0].(int) != 100 {
		t.Errorf("args[0] = %v, want 100", args[0])
	}
}

func TestInsertWithReturning(t *testing.T) {
	sql, args := Insert("users").
		Columns("name", "email", "age").
		Values("Alice", "alice@example.com", 30).
		Returning("id", "created_at").
		Build()

	want := "INSERT INTO users (name, email, age) VALUES ($1, $2, $3) RETURNING id, created_at"
	if sql != want {
		t.Errorf("SQL = %q, want %q", sql, want)
	}
	if len(args) != 3 {
		t.Errorf("args = %v, want 3 args", args)
	}
	if args[0].(string) != "Alice" {
		t.Errorf("args[0] = %v, want Alice", args[0])
	}
	if args[1].(string) != "alice@example.com" {
		t.Errorf("args[1] = %v, want alice@example.com", args[1])
	}
	if args[2].(int) != 30 {
		t.Errorf("args[2] = %v, want 30", args[2])
	}
}

func TestUpdateWithSetAndWhere(t *testing.T) {
	sql, args := Update("users").
		Set("name", "Bob").
		Set("age", 25).
		Where("id = $1", 1).
		Returning("id", "updated_at").
		Build()

	if !contains(sql, "UPDATE users") {
		t.Errorf("SQL = %q, want UPDATE users", sql)
	}
	if !contains(sql, "SET name = $1, age = $2") {
		t.Errorf("SQL = %q, want SET name = $1, age = $2", sql)
	}
	if !contains(sql, "WHERE (id = $3)") {
		t.Errorf("SQL = %q, want WHERE (id = $3)", sql)
	}
	if !contains(sql, "RETURNING id, updated_at") {
		t.Errorf("SQL = %q, want RETURNING id, updated_at", sql)
	}
	if len(args) != 3 {
		t.Errorf("args = %v, want 3 args", args)
	}
	if args[0].(string) != "Bob" {
		t.Errorf("args[0] = %v, want Bob", args[0])
	}
	if args[1].(int) != 25 {
		t.Errorf("args[1] = %v, want 25", args[1])
	}
	if args[2].(int) != 1 {
		t.Errorf("args[2] = %v, want 1", args[2])
	}
}

func TestDeleteWithWhere(t *testing.T) {
	sql, args := Delete("users").
		Where("id = $1", 5).
		Build()

	want := "DELETE FROM users WHERE (id = $1)"
	if sql != want {
		t.Errorf("SQL = %q, want %q", sql, want)
	}
	if len(args) != 1 || args[0].(int) != 5 {
		t.Errorf("args = %v, want [5]", args)
	}
}

func TestDeleteMultipleWhere(t *testing.T) {
	sql, args := Delete("users").
		Where("id = $1", 5).
		Where("status = $1", "inactive").
		Build()

	if !contains(sql, "AND") {
		t.Errorf("SQL = %q, want AND connector", sql)
	}
	if len(args) != 2 {
		t.Errorf("args = %v, want 2 args", args)
	}
	// Verify placeholders are renumbered
	if !contains(sql, "$1") || !contains(sql, "$2") {
		t.Errorf("SQL = %q, want $1 and $2 placeholders", sql)
	}
}

func TestCount(t *testing.T) {
	sql, args := Count("users").
		Where("active = $1", true).
		Build()

	want := "SELECT COUNT(*) FROM users WHERE (active = $1)"
	if sql != want {
		t.Errorf("SQL = %q, want %q", sql, want)
	}
	if len(args) != 1 || args[0].(bool) != true {
		t.Errorf("args = %v, want [true]", args)
	}
}

func TestCountMultipleWhere(t *testing.T) {
	sql, args := Count("orders").
		Where("status = $1", "pending").
		Where("total > $1", 100.0).
		Build()

	if !contains(sql, "SELECT COUNT(*) FROM orders") {
		t.Errorf("SQL = %q, want SELECT COUNT(*) FROM orders", sql)
	}
	if !contains(sql, "AND") {
		t.Errorf("SQL = %q, want AND connector", sql)
	}
	if len(args) != 2 {
		t.Errorf("args = %v, want 2 args", args)
	}
}

func TestSelectNoClauses(t *testing.T) {
	sql, args := Select("id", "name").From("users").Build()

	want := "SELECT id, name FROM users"
	if sql != want {
		t.Errorf("SQL = %q, want %q", sql, want)
	}
	if len(args) != 0 {
		t.Errorf("args = %v, want empty", args)
	}
}

func TestSelectAllColumns(t *testing.T) {
	sql, _ := Select("*").From("users").Build()

	if !contains(sql, "SELECT *") {
		t.Errorf("SQL = %q, want SELECT *", sql)
	}
}

// --- Full integration-style test ---

func TestComplexQuery(t *testing.T) {
	sql, args := Select("u.id", "u.name", "p.title").
		From("users u").
		LeftJoin("posts p", "p.author_id = u.id").
		Where("u.active = $1", true).
		Where("u.role = $1", "admin").
		Order("u.name", "ASC").
		Limit(10).
		Offset(0).
		Build()

	if !contains(sql, "LEFT JOIN posts p ON p.author_id = u.id") {
		t.Errorf("SQL = %q, want LEFT JOIN", sql)
	}
	if !contains(sql, "WHERE") {
		t.Errorf("SQL = %q, want WHERE clause", sql)
	}
	if !contains(sql, "AND") {
		t.Errorf("SQL = %q, want AND between conditions", sql)
	}
	if !contains(sql, "ORDER BY u.name ASC") {
		t.Errorf("SQL = %q, want ORDER BY", sql)
	}

	// Should have: active(true), role("admin"), limit(10), offset(0)
	if len(args) != 4 {
		t.Errorf("args = %v, want 4 args", args)
	}
}

// helper
func contains(s, substr string) bool {
	return strings.Contains(s, substr)
}

// --- F4: Build idempotency ---

func TestBuildIdempotent(t *testing.T) {
	qb := Select("id").From("users").Where("active = $1", true).Limit(10).Offset(5)

	sql1, args1 := qb.Build()
	sql2, args2 := qb.Build()

	if sql1 != sql2 {
		t.Errorf("Build() not idempotent: sql1=%q sql2=%q", sql1, sql2)
	}
	if len(args1) != len(args2) {
		t.Errorf("Build() args differ: %v vs %v", args1, args2)
	}
}

// --- F5: Cursor + Where placeholder interaction ---

func TestCursorThenWherePlaceholderOrdering(t *testing.T) {
	qb := Select("id", "name").From("posts").
		Cursor("id", 100, "forward").
		Where("status = $1", "published")

	sql, args := qb.Build()

	// Cursor placeholder must not collide with Where placeholder
	if strings.Count(sql, "$1") != 1 || strings.Count(sql, "$2") != 1 {
		t.Errorf("expected unique placeholders, got SQL: %q", sql)
	}
	if len(args) != 2 {
		t.Fatalf("expected 2 args, got %d: %v", len(args), args)
	}
	// First arg is the cursor value (100)
	if args[0].(int) != 100 {
		t.Errorf("first arg should be cursor value 100, got %v", args[0])
	}
}

func TestMultipleCursorsPlaceholderOrdering(t *testing.T) {
	qb := Select("id").From("posts").
		Cursor("id", 50, "forward").
		Cursor("created_at", "2024-01-01", "forward")

	sql, args := qb.Build()

	// Both cursors must get unique placeholders
	if strings.Count(sql, "$1") != 1 || strings.Count(sql, "$2") != 1 {
		t.Errorf("expected unique $1 and $2, got SQL: %q", sql)
	}
	if len(args) != 2 {
		t.Errorf("expected 2 args, got %d: %v", len(args), args)
	}
}
