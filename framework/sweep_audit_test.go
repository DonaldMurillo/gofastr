package framework

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	fdb "github.com/DonaldMurillo/gofastr/framework/db"
	"github.com/DonaldMurillo/gofastr/framework/hook"
)

func countAuditRows(t *testing.T, db *sql.DB) int {
	t.Helper()
	var n int
	if err := db.QueryRow("SELECT COUNT(*) FROM audit_log").Scan(&n); err != nil {
		t.Fatalf("count audit_log: %v", err)
	}
	return n
}

// A write a later hook refuses rolls back, and its audit row with it.
func TestSweepAuditRowRollsBackWithTheWrite(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *sql.DB, _ Dialect) {
		app := auditApp(t, db, "alice")
		app.HookRegistry("posts").RegisterHook(hook.AfterCreate, func(context.Context, any) error {
			return errors.New("refused after the audit hook")
		})
		ta := TestHarness(t, app)
		resp := ta.Post("/posts", map[string]any{"title": "hello"})
		resp.AssertStatus(t, http.StatusInternalServerError)
		var posts int
		if err := db.QueryRow("SELECT COUNT(*) FROM posts").Scan(&posts); err != nil {
			t.Fatal(err)
		}
		if posts != 0 {
			t.Fatalf("posts = %d after a refused create, want 0", posts)
		}
		if n := countAuditRows(t, db); n != 0 {
			t.Fatalf("audit rows = %d after the write rolled back, want 0", n)
		}
	})
}

// AppendAuditEvent writes through the transaction on ctx, so a rollback
// drops the row. The database is a file with several connections, so a
// write through the pool would land outside the transaction.
func TestSweepAuditAppendJoinsContextTransaction(t *testing.T) {
	db, err := sql.Open("sqlite3", filepath.Join(t.TempDir(), "audit.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if err := EnsureAuditTable(db, ""); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	for _, commit := range []bool{false, true} {
		tx, err := db.BeginTx(ctx, nil)
		if err != nil {
			t.Fatal(err)
		}
		if err := AppendAuditEvent(fdb.WithTx(ctx, tx), db, "", "auth", "login", "u1", "u1", nil); err != nil {
			_ = tx.Rollback()
			t.Fatalf("append (commit=%v): %v", commit, err)
		}
		if commit {
			err = tx.Commit()
		} else {
			err = tx.Rollback()
		}
		if err != nil {
			t.Fatal(err)
		}
		want := 0
		if commit {
			want = 1
		}
		if n := countAuditRows(t, db); n != want {
			t.Fatalf("audit rows after commit=%v: %d, want %d", commit, n, want)
		}
	}
}

// An After hook handed a payload of the wrong type errors and writes no
// audit row, rather than one keyed on an empty record.
func TestSweepAuditHookRefusesDriftedPayload(t *testing.T) {
	db := openTestDB(t, DialectSQLite)
	app := auditApp(t, db, "alice")
	hr := app.HookRegistry("posts")
	ctx := context.Background()
	for _, c := range []struct {
		hook hook.HookType
		data any
	}{
		{hook.AfterCreate, "not a row"},
		{hook.AfterUpdate, 42},
		{hook.AfterDelete, map[string]any{"id": "1"}},
	} {
		if err := hr.ExecuteHooks(ctx, c.hook, c.data); err == nil {
			t.Errorf("%v with %T: no error, want the payload refused", c.hook, c.data)
		}
	}
	if n := countAuditRows(t, db); n != 0 {
		t.Fatalf("audit rows = %d after drifted payloads, want 0", n)
	}
}

// A Redact that panics keeps the row's keys with every value masked.
func TestSweepAuditRedactPanicMasksValues(t *testing.T) {
	db := openTestDB(t, DialectSQLite)
	app := auditAppWithRedact(t, db, func(string, map[string]any) map[string]any { panic("boom") })
	TestHarness(t, app).Post("/posts", map[string]any{"title": "hello"}).AssertStatus(t, http.StatusCreated)
	rows := readAuditRows(t, db)
	if len(rows) != 1 {
		t.Fatalf("audit rows = %d, want 1", len(rows))
	}
	diff, _ := rows[0]["diff"].(string)
	if strings.Contains(diff, "hello") || !strings.Contains(diff, `"title":"[redaction failed]"`) {
		t.Fatalf("diff = %q, want the title key with its value masked", diff)
	}
}

// A Redact whose output cannot be marshalled keeps the row's keys with
// every value masked, on create and on update, old image included.
func TestSweepAuditUnmarshalableRedactMasksValues(t *testing.T) {
	db := openTestDB(t, DialectSQLite)
	ta := TestHarness(t, auditAppWithRedact(t, db, func(_ string, row map[string]any) map[string]any {
		row["poison"] = func() {}
		return row
	}))
	create := ta.Post("/posts", map[string]any{"title": "before"})
	create.AssertStatus(t, http.StatusCreated)
	var created map[string]any
	if err := json.Unmarshal([]byte(create.Body()), singleMap(&created)); err != nil {
		t.Fatal(err)
	}
	ta.Put("/posts/"+created["id"].(string), map[string]any{"title": "after"}).AssertStatus(t, http.StatusOK)
	rows := readAuditRows(t, db)
	if len(rows) != 2 {
		t.Fatalf("audit rows = %d, want 2", len(rows))
	}
	for _, row := range rows {
		diff, _ := row["diff"].(string)
		var images map[string]map[string]any
		if err := json.Unmarshal([]byte(diff), &images); err != nil {
			t.Fatalf("%s diff %q: %v", row["op"], diff, err)
		}
		want := []string{"new"}
		if row["op"] == "update" {
			want = append(want, "old")
		}
		for _, image := range want {
			if images[image]["title"] != "[redaction failed]" {
				t.Errorf("%s diff = %q, want %s.title masked", row["op"], diff, image)
			}
		}
	}
}

// A delete Redact that panics keeps the original record_id.
func TestSweepAuditDeleteRedactPanicKeepsRecordID(t *testing.T) {
	db := openTestDB(t, DialectSQLite)
	ta := TestHarness(t, auditAppWithRedact(t, db, func(_ string, row map[string]any) map[string]any {
		if len(row) == 1 {
			panic("boom")
		}
		return row
	}))
	create := ta.Post("/posts", map[string]any{"title": "x"})
	create.AssertStatus(t, http.StatusCreated)
	var created map[string]any
	if err := json.Unmarshal([]byte(create.Body()), singleMap(&created)); err != nil {
		t.Fatal(err)
	}
	id := created["id"].(string)
	ta.Delete("/posts/"+id).AssertStatus(t, http.StatusNoContent)
	rows := readAuditRows(t, db)
	if len(rows) != 2 || rows[1]["op"] != "delete" {
		t.Fatalf("audit rows = %v, want a create then a delete", rows)
	}
	if rows[1]["record_id"] != id {
		t.Fatalf("delete record_id = %v, want %s", rows[1]["record_id"], id)
	}
}

// An update with no captured pre-image has no "old" key in its diff.
func TestSweepAuditUpdateDiffOmitsMissingOld(t *testing.T) {
	var got map[string]any
	if err := json.Unmarshal(buildAuditUpdateDiff(nil, map[string]any{"a": 1}, nil, nil, nil), &got); err != nil {
		t.Fatal(err)
	}
	if _, ok := got["old"]; ok {
		t.Fatalf("diff = %v, want no old key", got)
	}
	if buildAuditDeleteDiff(nil, nil, nil) != nil {
		t.Fatal("a delete with no pre-image and no request meta has a diff, want NULL")
	}
}

// The client IP an audit row records has no port.
func TestSweepAuditClientIPDropsPort(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.RemoteAddr = "203.0.113.7:51234"
	if got := clientIP(r); got != "203.0.113.7" {
		t.Fatalf("clientIP = %q, want 203.0.113.7", got)
	}
}
