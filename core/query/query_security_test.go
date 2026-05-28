package query

import (
	"strings"
	"testing"
)

func TestSelectFrom_DoesNotInterpolateUnsafeTableName(t *testing.T) {
	payload := `users; DROP TABLE audit_logs; --`
	sql, _ := Select("*").From(payload).Build()

	if strings.Contains(sql, payload) || strings.Contains(sql, "; DROP TABLE") {
		t.Fatalf("SECURITY: [query] SELECT builder interpolated unsafe table name into SQL: %q", sql)
	}
}

func TestSelectColumns_DoesNotInterpolateUnsafeColumnExpression(t *testing.T) {
	payload := `name, (SELECT secret FROM api_keys LIMIT 1) AS leaked`
	sql, _ := Select(payload).From("users").Build()

	if strings.Contains(sql, payload) || strings.Contains(sql, "SELECT secret FROM api_keys") {
		t.Fatalf("SECURITY: [query] SELECT builder interpolated unsafe column expression into SQL: %q", sql)
	}
}

func TestSelectOrder_DoesNotInterpolateUnsafeOrderColumn(t *testing.T) {
	payload := `name; DROP TABLE audit_logs; --`
	sql, _ := Select("*").From("users").Order(payload, "ASC").Build()

	if strings.Contains(sql, payload) || strings.Contains(sql, "; DROP TABLE") {
		t.Fatalf("SECURITY: [query] ORDER BY builder interpolated unsafe column into SQL: %q", sql)
	}
}

func TestSelectOrder_DoesNotInterpolateUnsafeDirection(t *testing.T) {
	payload := `ASC; DROP TABLE audit_logs; --`
	sql, _ := Select("*").From("users").Order("name", payload).Build()

	if strings.Contains(sql, payload) || strings.Contains(sql, "; DROP TABLE") {
		t.Fatalf("SECURITY: [query] ORDER BY builder interpolated unsafe direction into SQL: %q", sql)
	}
}

func TestSelectCursor_DoesNotInterpolateUnsafeCursorField(t *testing.T) {
	payload := `id) DESC; DROP TABLE audit_logs; --`
	sql, _ := Select("*").From("users").Cursor(payload, 42, "forward").Build()

	if strings.Contains(sql, payload) || strings.Contains(sql, "; DROP TABLE") {
		t.Fatalf("SECURITY: [query] cursor builder interpolated unsafe field into SQL: %q", sql)
	}
}

func TestSelectJoin_DoesNotInterpolateUnsafeJoinTable(t *testing.T) {
	payload := `profiles; DROP TABLE users; --`
	sql, _ := Select("*").From("users").Join(payload, "profiles.user_id = users.id").Build()

	if strings.Contains(sql, payload) || strings.Contains(sql, "; DROP TABLE") {
		t.Fatalf("SECURITY: [query] JOIN builder interpolated unsafe table into SQL: %q", sql)
	}
}

func TestSelectJoin_DoesNotInterpolateUnsafeJoinPredicate(t *testing.T) {
	payload := `profiles.user_id = users.id; DROP TABLE audit_logs; --`
	sql, _ := Select("*").From("users").Join("profiles", payload).Build()

	if strings.Contains(sql, payload) || strings.Contains(sql, "; DROP TABLE") {
		t.Fatalf("SECURITY: [query] JOIN builder interpolated unsafe ON predicate into SQL: %q", sql)
	}
}

func TestSelectLeftJoin_DoesNotInterpolateUnsafeJoinTable(t *testing.T) {
	payload := `profiles; DROP TABLE users; --`
	sql, _ := Select("*").From("users").LeftJoin(payload, "profiles.user_id = users.id").Build()

	if strings.Contains(sql, payload) || strings.Contains(sql, "; DROP TABLE") {
		t.Fatalf("SECURITY: [query] LEFT JOIN builder interpolated unsafe table into SQL: %q", sql)
	}
}

func TestSelectLeftJoin_DoesNotInterpolateUnsafeJoinPredicate(t *testing.T) {
	payload := `profiles.user_id = users.id; DROP TABLE audit_logs; --`
	sql, _ := Select("*").From("users").LeftJoin("profiles", payload).Build()

	if strings.Contains(sql, payload) || strings.Contains(sql, "; DROP TABLE") {
		t.Fatalf("SECURITY: [query] LEFT JOIN builder interpolated unsafe ON predicate into SQL: %q", sql)
	}
}

func TestInsertColumns_DoesNotInterpolateUnsafeColumnName(t *testing.T) {
	payload := `name) VALUES ('attacker'); DELETE FROM users; --`
	sql, _ := Insert("users").Columns(payload).Values("x").Build()

	if strings.Contains(sql, payload) || strings.Contains(sql, "DELETE FROM users") {
		t.Fatalf("SECURITY: [query] INSERT builder interpolated unsafe column into SQL: %q", sql)
	}
}

func TestUpdateSet_DoesNotInterpolateUnsafeColumnName(t *testing.T) {
	payload := `role = 'admin' --`
	sql, _ := Update("users").Set(payload, true).Where("id = $1", 1).Build()

	if strings.Contains(sql, payload) {
		t.Fatalf("SECURITY: [query] UPDATE builder interpolated unsafe column into SQL: %q", sql)
	}
}

func TestCount_DoesNotInterpolateUnsafeTableName(t *testing.T) {
	payload := `users; DROP TABLE audit_logs; --`
	sql, _ := Count(payload).Build()

	if strings.Contains(sql, payload) || strings.Contains(sql, "; DROP TABLE") {
		t.Fatalf("SECURITY: [query] COUNT builder interpolated unsafe table name into SQL: %q", sql)
	}
}

func TestDelete_DoesNotInterpolateUnsafeTableName(t *testing.T) {
	payload := `users; DROP TABLE audit_logs; --`
	sql, _ := Delete(payload).Where("id = $1", 1).Build()

	if strings.Contains(sql, payload) || strings.Contains(sql, "; DROP TABLE") {
		t.Fatalf("SECURITY: [query] DELETE builder interpolated unsafe table name into SQL: %q", sql)
	}
}

func TestInsert_DoesNotInterpolateUnsafeTableName(t *testing.T) {
	payload := `users; DROP TABLE audit_logs; --`
	sql, _ := Insert(payload).Columns("name").Values("alice").Build()

	if strings.Contains(sql, payload) || strings.Contains(sql, "; DROP TABLE") {
		t.Fatalf("SECURITY: [query] INSERT builder interpolated unsafe table name into SQL: %q", sql)
	}
}

func TestUpdate_DoesNotInterpolateUnsafeTableName(t *testing.T) {
	payload := `users; DROP TABLE audit_logs; --`
	sql, _ := Update(payload).Set("name", "alice").Build()

	if strings.Contains(sql, payload) || strings.Contains(sql, "; DROP TABLE") {
		t.Fatalf("SECURITY: [query] UPDATE builder interpolated unsafe table name into SQL: %q", sql)
	}
}

func TestInsertReturning_DoesNotInterpolateUnsafeReturningColumn(t *testing.T) {
	payload := `id; DROP TABLE audit_logs; --`
	sql, _ := Insert("users").Columns("name").Values("alice").Returning(payload).Build()

	if strings.Contains(sql, payload) || strings.Contains(sql, "; DROP TABLE") {
		t.Fatalf("SECURITY: [query] INSERT RETURNING interpolated unsafe column into SQL: %q", sql)
	}
}

func TestUpdateReturning_DoesNotInterpolateUnsafeReturningColumn(t *testing.T) {
	payload := `id; DROP TABLE audit_logs; --`
	sql, _ := Update("users").Set("name", "alice").Returning(payload).Build()

	if strings.Contains(sql, payload) || strings.Contains(sql, "; DROP TABLE") {
		t.Fatalf("SECURITY: [query] UPDATE RETURNING interpolated unsafe column into SQL: %q", sql)
	}
}
