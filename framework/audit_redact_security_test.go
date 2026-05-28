package framework

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
)

func TestAudit_RedactPanicOnCreateDoesNotAbortRequest(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *sql.DB, _ Dialect) {
		app := auditAppWithRedact(t, db, func(string, map[string]any) map[string]any {
			panic("boom")
		})

		req := httptest.NewRequest(http.MethodPost, "/posts", strings.NewReader(`{"title":"hello"}`))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()

		defer func() {
			if r := recover(); r != nil {
				t.Fatalf("SECURITY: [audit] create request panicked when Redact panicked: %v. Attack: audit-hook DoS on create.", r)
			}
		}()
		app.Router().ServeHTTP(rec, req)
		if rec.Code != http.StatusCreated {
			t.Fatalf("SECURITY: [audit] create request failed when Redact panicked: status=%d body=%s. Attack: audit-hook DoS on create.", rec.Code, rec.Body.String())
		}
	})
}

func TestAudit_RedactPanicOnUpdateDoesNotAbortRequest(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *sql.DB, _ Dialect) {
		redact := func(_ string, row map[string]any) map[string]any {
			if row["title"] == "after" {
				panic("boom")
			}
			return row
		}
		ta := TestHarness(t, auditAppWithRedact(t, db, redact))
		create := ta.Post("/posts", map[string]any{"title": "before"}); create.AssertStatus(t, http.StatusCreated)

		var created map[string]any
		if err := json.Unmarshal([]byte(create.Body()), &created); err != nil {
			t.Fatalf("decode create: %v", err)
		}

		req := httptest.NewRequest(http.MethodPut, "/posts/"+created["id"].(string), strings.NewReader(`{"title":"after"}`))
		req.Header.Set("Content-Type", "application/json")
		req.SetPathValue("id", created["id"].(string))
		rec := httptest.NewRecorder()

		defer func() {
			if r := recover(); r != nil {
				t.Fatalf("SECURITY: [audit] update request panicked when Redact panicked: %v. Attack: audit-hook DoS on update.", r)
			}
		}()
		ta.App.Router().ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("SECURITY: [audit] update request failed when Redact panicked: status=%d body=%s. Attack: audit-hook DoS on update.", rec.Code, rec.Body.String())
		}
	})
}

func TestAudit_RedactPanicOnDeleteDoesNotAbortRequest(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *sql.DB, _ Dialect) {
		redact := func(_ string, row map[string]any) map[string]any {
			if len(row) == 1 && row["id"] != nil {
				panic("boom")
			}
			return row
		}
		ta := TestHarness(t, auditAppWithRedact(t, db, redact))
		create := ta.Post("/posts", map[string]any{"title": "to-delete"}); create.AssertStatus(t, http.StatusCreated)

		var created map[string]any
		if err := json.Unmarshal([]byte(create.Body()), &created); err != nil {
			t.Fatalf("decode create: %v", err)
		}

		req := httptest.NewRequest(http.MethodDelete, "/posts/"+created["id"].(string), nil)
		req.SetPathValue("id", created["id"].(string))
		rec := httptest.NewRecorder()

		defer func() {
			if r := recover(); r != nil {
				t.Fatalf("SECURITY: [audit] delete request panicked when Redact panicked: %v. Attack: audit-hook DoS on delete.", r)
			}
		}()
		ta.App.Router().ServeHTTP(rec, req)
		if rec.Code != http.StatusNoContent {
			t.Fatalf("SECURITY: [audit] delete request failed when Redact panicked: status=%d body=%s. Attack: audit-hook DoS on delete.", rec.Code, rec.Body.String())
		}
	})
}

func TestAudit_DeleteRedactOmittedIDKeepsOriginalRecordID(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *sql.DB, _ Dialect) {
		redact := func(_ string, row map[string]any) map[string]any {
			if len(row) == 1 && row["id"] != nil {
				return map[string]any{}
			}
			return row
		}
		ta := TestHarness(t, auditAppWithRedact(t, db, redact))
		create := ta.Post("/posts", map[string]any{"title": "to-delete"})
		create.AssertStatus(t, http.StatusCreated)

		var created map[string]any
		if err := json.Unmarshal([]byte(create.Body()), &created); err != nil {
			t.Fatalf("decode create: %v", err)
		}
		id := created["id"].(string)

		ta.Delete("/posts/" + id).AssertStatus(t, http.StatusNoContent)

		rows := readAuditRows(t, db)
		last := rows[len(rows)-1]
		if last["record_id"] != id {
			t.Fatalf("SECURITY: [audit] delete audit row lost original record_id when Redact omitted id: got=%q want=%q. Attack: audit-forensics erasure via malformed redact callback.", last["record_id"], id)
		}
	})
}

func TestAudit_ActorStripsNewlines(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *sql.DB, _ Dialect) {
		app := NewApp(WithDB(db), WithoutDefaultMiddleware())
		app.Entity("posts", entity.EntityConfig{
			Table: "posts",
			Fields: []schema.Field{
				{Name: "title", Type: schema.String, Required: true},
			},
		}.WithTimestamps(false))
		if err := AutoMigrate(db, app.Registry); err != nil {
			t.Fatalf("automigrate: %v", err)
		}
		app.WithAuditLog(AuditConfig{
			Actor: func(_ context.Context) string { return "alice\nadmin" },
		})
		ta := TestHarness(t, app)
		ta.Post("/posts", map[string]any{"title": "hello"}).AssertStatus(t, http.StatusCreated)

		rows := readAuditRows(t, db)
		if rows[0]["actor_id"] != "aliceadmin" {
			t.Fatalf("SECURITY: [audit] actor_id retained newline/control bytes %q. Attack: audit-log injection via actor resolver.", rows[0]["actor_id"])
		}
	})
}

func TestAudit_DeleteRedactStripsNewlinesFromRecordID(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *sql.DB, _ Dialect) {
		redact := func(_ string, row map[string]any) map[string]any {
			if len(row) == 1 && row["id"] != nil {
				return map[string]any{"id": "public-id\nforged"}
			}
			return row
		}
		ta := TestHarness(t, auditAppWithRedact(t, db, redact))
		create := ta.Post("/posts", map[string]any{"title": "to-delete"})
		create.AssertStatus(t, http.StatusCreated)

		var created map[string]any
		if err := json.Unmarshal([]byte(create.Body()), &created); err != nil {
			t.Fatalf("decode create: %v", err)
		}

		ta.Delete("/posts/" + created["id"].(string)).AssertStatus(t, http.StatusNoContent)

		rows := readAuditRows(t, db)
		last := rows[len(rows)-1]
		if last["record_id"] != "public-idforged" {
			t.Fatalf("SECURITY: [audit] delete audit record_id retained newline/control payload %q. Attack: audit-log record_id injection via redact callback.", last["record_id"])
		}
	})
}

func TestAudit_ActorStripsNUL(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *sql.DB, _ Dialect) {
		app := NewApp(WithDB(db), WithoutDefaultMiddleware())
		app.Entity("posts", entity.EntityConfig{
			Table: "posts",
			Fields: []schema.Field{
				{Name: "title", Type: schema.String, Required: true},
			},
		}.WithTimestamps(false))
		if err := AutoMigrate(db, app.Registry); err != nil {
			t.Fatalf("automigrate: %v", err)
		}
		app.WithAuditLog(AuditConfig{
			Actor: func(_ context.Context) string { return "alice\x00admin" },
		})
		ta := TestHarness(t, app)
		ta.Post("/posts", map[string]any{"title": "hello"}).AssertStatus(t, http.StatusCreated)

		rows := readAuditRows(t, db)
		if rows[0]["actor_id"] != "aliceadmin" {
			t.Fatalf("SECURITY: [audit] actor_id retained NUL/control bytes %q. Attack: audit-log injection via actor resolver.", rows[0]["actor_id"])
		}
	})
}

func TestAudit_DeleteRedactStripsNULFromRecordID(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *sql.DB, _ Dialect) {
		redact := func(_ string, row map[string]any) map[string]any {
			if len(row) == 1 && row["id"] != nil {
				return map[string]any{"id": "public-id\x00forged"}
			}
			return row
		}
		ta := TestHarness(t, auditAppWithRedact(t, db, redact))
		create := ta.Post("/posts", map[string]any{"title": "to-delete"})
		create.AssertStatus(t, http.StatusCreated)

		var created map[string]any
		if err := json.Unmarshal([]byte(create.Body()), &created); err != nil {
			t.Fatalf("decode create: %v", err)
		}

		ta.Delete("/posts/" + created["id"].(string)).AssertStatus(t, http.StatusNoContent)

		rows := readAuditRows(t, db)
		last := rows[len(rows)-1]
		if last["record_id"] != "public-idforged" {
			t.Fatalf("SECURITY: [audit] delete audit record_id retained NUL/control payload %q. Attack: audit-log record_id injection via redact callback.", last["record_id"])
		}
	})
}

func TestAudit_ActorPanicOnCreateDoesNotAbortRequest(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *sql.DB, _ Dialect) {
		app := auditApp(t, db, "")
		app.WithAuditLog(AuditConfig{
			Actor: func(context.Context) string { panic("boom") },
		})
		req := httptest.NewRequest(http.MethodPost, "/posts", strings.NewReader(`{"title":"hello"}`))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()

		defer func() {
			if r := recover(); r != nil {
				t.Fatalf("SECURITY: [audit] create request panicked when Actor panicked: %v. Attack: actor-hook DoS on create.", r)
			}
		}()
		app.Router().ServeHTTP(rec, req)
		if rec.Code != http.StatusCreated {
			t.Fatalf("SECURITY: [audit] create request failed when Actor panicked: status=%d body=%s. Attack: actor-hook DoS on create.", rec.Code, rec.Body.String())
		}
	})
}

func TestAudit_ActorPanicOnUpdateDoesNotAbortRequest(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *sql.DB, _ Dialect) {
		armed := false
		app := auditApp(t, db, "")
		app.WithAuditLog(AuditConfig{
			Actor: func(context.Context) string {
				if armed {
					panic("boom")
				}
				return ""
			},
		})
		ta := TestHarness(t, app)
		create := ta.Post("/posts", map[string]any{"title": "before"})
		create.AssertStatus(t, http.StatusCreated)
		var created map[string]any
		if err := json.Unmarshal([]byte(create.Body()), &created); err != nil {
			t.Fatalf("decode create: %v", err)
		}
		armed = true

		req := httptest.NewRequest(http.MethodPut, "/posts/"+created["id"].(string), strings.NewReader(`{"title":"after"}`))
		req.Header.Set("Content-Type", "application/json")
		req.SetPathValue("id", created["id"].(string))
		rec := httptest.NewRecorder()

		defer func() {
			if r := recover(); r != nil {
				t.Fatalf("SECURITY: [audit] update request panicked when Actor panicked: %v. Attack: actor-hook DoS on update.", r)
			}
		}()
		app.Router().ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("SECURITY: [audit] update request failed when Actor panicked: status=%d body=%s. Attack: actor-hook DoS on update.", rec.Code, rec.Body.String())
		}
	})
}

func TestAudit_ActorPanicOnDeleteDoesNotAbortRequest(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *sql.DB, _ Dialect) {
		armed := false
		app := auditApp(t, db, "")
		app.WithAuditLog(AuditConfig{
			Actor: func(context.Context) string {
				if armed {
					panic("boom")
				}
				return ""
			},
		})
		ta := TestHarness(t, app)
		create := ta.Post("/posts", map[string]any{"title": "to-delete"})
		create.AssertStatus(t, http.StatusCreated)
		var created map[string]any
		if err := json.Unmarshal([]byte(create.Body()), &created); err != nil {
			t.Fatalf("decode create: %v", err)
		}
		armed = true

		req := httptest.NewRequest(http.MethodDelete, "/posts/"+created["id"].(string), nil)
		req.SetPathValue("id", created["id"].(string))
		rec := httptest.NewRecorder()

		defer func() {
			if r := recover(); r != nil {
				t.Fatalf("SECURITY: [audit] delete request panicked when Actor panicked: %v. Attack: actor-hook DoS on delete.", r)
			}
		}()
		app.Router().ServeHTTP(rec, req)
		if rec.Code != http.StatusNoContent {
			t.Fatalf("SECURITY: [audit] delete request failed when Actor panicked: status=%d body=%s. Attack: actor-hook DoS on delete.", rec.Code, rec.Body.String())
		}
	})
}

func TestAudit_RedactUnmarshalableCreateValueDoesNotEraseDiff(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *sql.DB, _ Dialect) {
		ta := TestHarness(t, auditAppWithRedact(t, db, func(_ string, row map[string]any) map[string]any {
			row["poison"] = func() {}
			return row
		}))
		ta.Post("/posts", map[string]any{"title": "hello"}).AssertStatus(t, http.StatusCreated)

		rows := readAuditRows(t, db)
		diff, _ := rows[len(rows)-1]["diff"].(string)
		if diff == "" {
			t.Fatalf("SECURITY: [audit] create audit diff disappeared when Redact returned an unmarshalable value. Attack: audit-evidence erasure via malicious redact output.")
		}
	})
}

func TestAudit_RedactUnmarshalableUpdateValueDoesNotEraseDiff(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *sql.DB, _ Dialect) {
		redact := func(_ string, row map[string]any) map[string]any {
			if row["title"] == "after" {
				row["poison"] = make(chan int)
			}
			return row
		}
		ta := TestHarness(t, auditAppWithRedact(t, db, redact))
		create := ta.Post("/posts", map[string]any{"title": "before"})
		create.AssertStatus(t, http.StatusCreated)
		var created map[string]any
		if err := json.Unmarshal([]byte(create.Body()), &created); err != nil {
			t.Fatalf("decode create: %v", err)
		}
		ta.Put("/posts/"+created["id"].(string), map[string]any{"title": "after"}).AssertStatus(t, http.StatusOK)

		rows := readAuditRows(t, db)
		diff, _ := rows[len(rows)-1]["diff"].(string)
		if diff == "" {
			t.Fatalf("SECURITY: [audit] update audit diff disappeared when Redact returned an unmarshalable value. Attack: audit-evidence erasure via malicious redact output.")
		}
	})
}
