package framework

import (
	"context"
	"database/sql"
	"encoding/json"
	"strings"
	"testing"
)

// AppendAuditEvent writes a non-CRUD row outside any entity/hook flow, so
// these tests exercise it directly against a bare DB (no App, no router).

func appendAuditRow(t *testing.T, db *sql.DB, table, entity, op, recordID, actor string, diff map[string]any) {
	t.Helper()
	if err := AppendAuditEvent(context.Background(), db, table, entity, op, recordID, actor, diff); err != nil {
		t.Fatalf("AppendAuditEvent: %v", err)
	}
}

func TestAppendAuditEvent_WritesAllColumns(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *sql.DB, _ Dialect) {
		if err := EnsureAuditTable(db, "audit_log"); err != nil {
			t.Fatalf("EnsureAuditTable: %v", err)
		}
		diff := map[string]any{"email": "alice@example.com", "reason": "bad_credentials"}
		appendAuditRow(t, db, "audit_log", "auth", "login.failed", "u_1", "u_1", diff)

		rows := readAuditRows(t, db)
		if len(rows) != 1 {
			t.Fatalf("expected 1 row, got %d", len(rows))
		}
		r := rows[0]
		if r["entity"] != "auth" {
			t.Errorf("entity = %v, want auth", r["entity"])
		}
		if r["op"] != "login.failed" {
			t.Errorf("op = %v, want login.failed", r["op"])
		}
		if r["record_id"] != "u_1" {
			t.Errorf("record_id = %v, want u_1", r["record_id"])
		}
		if r["actor_id"] != "u_1" {
			t.Errorf("actor_id = %v, want u_1", r["actor_id"])
		}
		raw, ok := r["diff"].(string)
		if !ok {
			t.Fatalf("expected diff string, got %T", r["diff"])
		}
		var got map[string]any
		if err := json.Unmarshal([]byte(raw), &got); err != nil {
			t.Fatalf("diff decode: %v", err)
		}
		if got["email"] != "alice@example.com" || got["reason"] != "bad_credentials" {
			t.Errorf("diff = %v, want email+reason", got)
		}
	})
}

func TestAppendAuditEvent_NilDiffWritesNULL(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *sql.DB, _ Dialect) {
		if err := EnsureAuditTable(db, "audit_log"); err != nil {
			t.Fatalf("EnsureAuditTable: %v", err)
		}
		// nil diff → NULL column.
		appendAuditRow(t, db, "audit_log", "auth", "login.succeeded", "u_2", "u_2", nil)
		// empty map diff → also NULL (no-detail event).
		appendAuditRow(t, db, "audit_log", "auth", "login.succeeded", "u_3", "u_3", map[string]any{})

		rows := readAuditRows(t, db)
		if len(rows) != 2 {
			t.Fatalf("expected 2 rows, got %d", len(rows))
		}
		for i, r := range rows {
			if _, hasDiff := r["diff"]; hasDiff {
				t.Errorf("row %d: expected NULL diff, got %v", i, r["diff"])
			}
		}
	})
}

// Sanitisation parity: a control character injected into a TEXT column must
// be stripped exactly as the CRUD delete path strips it
// (sanitizeAuditField). A forged newline in record_id would split audit
// records in forensics tooling.
func TestAppendAuditEvent_SanitisesTextColumns(t *testing.T) {
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	if err := EnsureAuditTable(db, "audit_log"); err != nil {
		t.Fatalf("EnsureAuditTable: %v", err)
	}
	injected := "u_1\nFAKE_AUDIT_LINE"
	appendAuditRow(t, db, "audit_log", "auth\nX", "login.failed\nX", injected, "actor\tY", map[string]any{"k": "v"})

	rows := readAuditRows(t, db)
	if len(rows) != 1 {
		t.Fatalf("expected 1 row, got %d", len(rows))
	}
	r := rows[0]
	// Each TEXT column must match what sanitizeAuditField produces from the
	// raw input — control bytes dropped, the remaining text verbatim.
	// "auth\nX" → "authX" (newline gone, X stays).
	wantEntity := sanitizeAuditField("auth\nX")
	wantOp := sanitizeAuditField("login.failed\nX")
	wantRecord := sanitizeAuditField(injected)
	wantActor := sanitizeAuditField("actor\tY")
	for _, c := range []struct{ name, got, want string }{
		{"entity", r["entity"].(string), wantEntity},
		{"op", r["op"].(string), wantOp},
		{"record_id", r["record_id"].(string), wantRecord},
		{"actor_id", r["actor_id"].(string), wantActor},
	} {
		if c.got != c.want {
			t.Errorf("%s = %q, want %q (sanitised)", c.name, c.got, c.want)
		}
		if strings.ContainsAny(c.got, "\n\t\r\x00") {
			t.Errorf("%s still contains control bytes: %q", c.name, c.got)
		}
	}
}

func TestAppendAuditEvent_InvalidTableErrors(t *testing.T) {
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	// A table name with a space is rejected by query.SafeIdent and must
	// surface as an error, never reach the DB.
	err = AppendAuditEvent(context.Background(), db, "audit log", "auth", "x", "r", "a", nil)
	if err == nil {
		t.Fatal("expected error for invalid table name, got nil")
	}
}

func TestAppendAuditEvent_EmptyRecordIDKept(t *testing.T) {
	// record_id is NOT NULL in the schema; an empty recordID must not
	// error (writeAuditRow writes the empty string verbatim). The auth
	// sink substitutes "-" for empty before calling, but AppendAuditEvent
	// itself must not panic or reject it.
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	if err := EnsureAuditTable(db, "audit_log"); err != nil {
		t.Fatalf("EnsureAuditTable: %v", err)
	}
	appendAuditRow(t, db, "audit_log", "auth", "login.failed", "", "", map[string]any{"email": "x@y.z"})

	rows := readAuditRows(t, db)
	if len(rows) != 1 {
		t.Fatalf("expected 1 row, got %d", len(rows))
	}
	if rows[0]["record_id"] != "" {
		t.Errorf("record_id = %v, want empty", rows[0]["record_id"])
	}
	if _, ok := rows[0]["actor_id"]; ok {
		t.Errorf("expected NULL actor_id, got %v", rows[0]["actor_id"])
	}
}
