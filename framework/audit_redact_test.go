package framework

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
)

func auditAppWithRedact(t *testing.T, db *sql.DB, redact func(string, map[string]any) map[string]any) *App {
	t.Helper()
	app := NewApp(WithDB(db), WithoutDefaultMiddleware())
	app.Entity("posts", entity.EntityConfig{
		Table: "posts",
		// Public: this audit suite posts anonymously throughout — the
		// secure-by-default session gate (issue #65) would otherwise 401
		// every request here.
		Public: true,
		Fields: []schema.Field{
			{Name: "title", Type: schema.String, Required: true},
			{Name: "secret", Type: schema.String},
		},
	}.WithTimestamps(false))
	if err := AutoMigrate(db, app.Registry); err != nil {
		t.Fatalf("automigrate: %v", err)
	}
	app.WithAuditLog(AuditConfig{
		Actor:  func(_ context.Context) string { return "rachel" },
		Redact: redact,
	})
	return app
}

// Redact = nil → audit JSON contains every non-sensitive column
// verbatim. The default scrubber (defaultSensitiveSuffixes) still
// removes fields whose names look like passwords / tokens / secrets;
// "title" is neutral so it must pass through unchanged.
func TestAudit_RedactNilKeepsAllColumns(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *sql.DB, _ Dialect) {
		ta := TestHarness(t, auditAppWithRedact(t, db, nil))

		resp := ta.Post("/posts", map[string]any{"title": "hello"})
		resp.AssertStatus(t, http.StatusCreated)

		rows := readAuditRows(t, db)
		if len(rows) != 1 {
			t.Fatalf("expected 1 audit row, got %d", len(rows))
		}
		var diff map[string]any
		if err := json.Unmarshal([]byte(rows[0]["diff"].(string)), &diff); err != nil {
			t.Fatalf("diff decode: %v", err)
		}
		newRow := diff["new"].(map[string]any)
		if newRow["title"] != "hello" {
			t.Fatalf("nil redact dropped non-sensitive column: %+v", newRow)
		}
	})
}

// Redact returning a stripped map → only its keys appear in the diff.
func TestAudit_RedactStripsSensitiveKeys(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *sql.DB, _ Dialect) {
		redact := func(entityName string, row map[string]any) map[string]any {
			if entityName != "posts" {
				return row
			}
			out := make(map[string]any, len(row))
			for k, v := range row {
				if k == "secret" {
					continue
				}
				out[k] = v
			}
			return out
		}
		ta := TestHarness(t, auditAppWithRedact(t, db, redact))

		resp := ta.Post("/posts", map[string]any{"title": "hello", "secret": "shh"})
		resp.AssertStatus(t, http.StatusCreated)

		rows := readAuditRows(t, db)
		var diff map[string]any
		if err := json.Unmarshal([]byte(rows[0]["diff"].(string)), &diff); err != nil {
			t.Fatalf("diff decode: %v", err)
		}
		newRow := diff["new"].(map[string]any)
		if _, ok := newRow["secret"]; ok {
			t.Fatalf("redact failed to drop secret column: %+v", newRow)
		}
		if newRow["title"] != "hello" {
			t.Fatalf("redact dropped non-sensitive column: %+v", newRow)
		}
	})
}

// When Redact returns nil the audit diff must NOT contain `{"new":null}`.
// The doc claims nil ⇒ empty map; this test asserts that contract.
func TestAudit_RedactNilProducesEmptyMapNotNull(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *sql.DB, _ Dialect) {
		redact := func(_ string, _ map[string]any) map[string]any { return nil }
		ta := TestHarness(t, auditAppWithRedact(t, db, redact))
		ta.Post("/posts", map[string]any{"title": "hello"}).AssertStatus(t, http.StatusCreated)

		rows := readAuditRows(t, db)
		var diff map[string]any
		if err := json.Unmarshal([]byte(rows[0]["diff"].(string)), &diff); err != nil {
			t.Fatalf("diff decode: %v", err)
		}
		// {"new": null} is the bug; want {"new": {}}.
		newVal, ok := diff["new"]
		if !ok {
			t.Fatalf("diff missing 'new' key: %+v", diff)
		}
		if newVal == nil {
			t.Fatalf("diff.new == null — doc says nil Redact ⇒ empty map, not null")
		}
		m, ok := newVal.(map[string]any)
		if !ok {
			t.Fatalf("diff.new is %T, want map", newVal)
		}
		if len(m) != 0 {
			t.Fatalf("diff.new = %+v, want empty map", m)
		}
	})
}

// Redact must fire on AfterDelete too — otherwise per-entity redaction
// has a hole for the very op (delete) most likely to leak natural-key
// PHI through record_id.
func TestAudit_RedactFiresOnDelete(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *sql.DB, _ Dialect) {
		var deleteCalls int
		var sawEntity, sawID string
		redact := func(entityName string, row map[string]any) map[string]any {
			// Return a fresh map — never mutate the input. The hook
			// passes the live response payload through Redact, so a
			// mutation would leak into the API response too.
			out := make(map[string]any, len(row))
			for k, v := range row {
				out[k] = v
			}
			if id, ok := out["id"].(string); ok {
				deleteCalls++
				sawEntity = entityName
				sawID = id
				out["id"] = "REDACTED"
			}
			return out
		}
		ta := TestHarness(t, auditAppWithRedact(t, db, redact))

		create := ta.Post("/posts", map[string]any{"title": "to delete"})
		create.AssertStatus(t, http.StatusCreated)
		var created map[string]any
		if err := json.Unmarshal([]byte(create.Body()), singleMap(&created)); err != nil {
			t.Fatalf("decode create: %v", err)
		}
		id := created["id"].(string)
		ta.Delete("/posts/"+id).AssertStatus(t, http.StatusNoContent)

		rows := readAuditRows(t, db)
		if len(rows) != 2 {
			t.Fatalf("expected 2 audit rows, got %d", len(rows))
		}
		// The second row is the delete; record_id must reflect the
		// Redact-substituted value, not the original id.
		if rows[1]["op"] != "delete" {
			t.Fatalf("rows[1].op = %v, want delete", rows[1]["op"])
		}
		if rows[1]["record_id"] != "REDACTED" {
			t.Fatalf("rows[1].record_id = %v, want REDACTED (Redact must fire on delete)", rows[1]["record_id"])
		}
		// Sanity: Redact was actually invoked with the entity name + id.
		if deleteCalls == 0 || sawEntity != "posts" || sawID != id {
			t.Fatalf("Redact invocation log: calls=%d entity=%q id=%q", deleteCalls, sawEntity, sawID)
		}
	})
}

// A Redact callback that mutates its input map must NOT leak the
// mutation into the API response payload. The framework defensively
// copies the row before handing it to Redact, so even a misbehaving
// host can't corrupt the user-visible body.
func TestAudit_RedactMutationDoesNotLeakIntoResponse(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *sql.DB, _ Dialect) {
		// Pathological Redact: in-place mutate keys + add a sentinel.
		redact := func(_ string, row map[string]any) map[string]any {
			row["title"] = "MUTATED-BY-REDACT"
			row["injected_by_redact"] = true
			return row
		}
		ta := TestHarness(t, auditAppWithRedact(t, db, redact))

		resp := ta.Post("/posts", map[string]any{"title": "original-title"})
		resp.AssertStatus(t, http.StatusCreated)

		var body map[string]any
		if err := json.Unmarshal([]byte(resp.Body()), singleMap(&body)); err != nil {
			t.Fatalf("decode response: %v", err)
		}
		if body["title"] != "original-title" {
			t.Fatalf("response.title = %v, want original-title — Redact mutation leaked into response payload", body["title"])
		}
		if _, leaked := body["injected_by_redact"]; leaked {
			t.Fatal("response payload contains 'injected_by_redact' key — Redact mutation leaked")
		}
	})
}

// Audit IDs must not collide under concurrent CRUD. The PK is `id TEXT
// PRIMARY KEY`; a `fmt.Sprintf("aud_%d", UnixNano())` ID space loses
// in any race where two After-hooks land in the same nanosecond,
// rolling back the *user's* transaction along with the audit insert.
func TestAudit_IDsDoNotCollideUnderConcurrency(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *sql.DB, dialect Dialect) {
		// SQLite :memory: doesn't share state across connections, so the
		// goroutines below can't see the same DB. Concurrency-collision
		// is a Postgres-shaped problem in practice.
		if dialect == DialectSQLite {
			t.Skip("sqlite :memory: doesn't share state across goroutines")
		}
		ta := TestHarness(t, auditAppWithRedact(t, db, nil))

		const N = 60
		errs := make(chan error, N)
		for i := 0; i < N; i++ {
			go func(n int) {
				resp := ta.Post("/posts", map[string]any{"title": fmt.Sprintf("p%d", n)})
				if resp.Status() != http.StatusCreated {
					errs <- fmt.Errorf("post %d: status %d body %s", n, resp.Status(), resp.Body())
					return
				}
				errs <- nil
			}(i)
		}
		for i := 0; i < N; i++ {
			if err := <-errs; err != nil {
				t.Fatalf("concurrent post failed (audit PK collision?): %v", err)
			}
		}

		rows := readAuditRows(t, db)
		if len(rows) != N {
			t.Fatalf("expected %d audit rows, got %d (collisions?)", N, len(rows))
		}
	})
}

// Redact receives the entity name so callers can apply per-entity rules.
func TestAudit_RedactReceivesEntityName(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *sql.DB, _ Dialect) {
		seen := ""
		redact := func(entityName string, row map[string]any) map[string]any {
			seen = entityName
			return row
		}
		ta := TestHarness(t, auditAppWithRedact(t, db, redact))
		ta.Post("/posts", map[string]any{"title": "x"}).AssertStatus(t, http.StatusCreated)

		if seen != "posts" {
			t.Fatalf("redact entity arg = %q, want posts", seen)
		}
	})
}
