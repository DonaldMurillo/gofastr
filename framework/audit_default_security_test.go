package framework

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestAudit_DefaultCreateRedactsSensitiveFields(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *sql.DB, _ Dialect) {
		ta := TestHarness(t, auditAppWithRedact(t, db, nil))
		ta.Post("/posts", map[string]any{"title": "hello", "secret": "shh"}).AssertStatus(t, http.StatusCreated)

		rows := readAuditRows(t, db)
		if len(rows) != 1 {
			t.Fatalf("expected 1 audit row, got %d", len(rows))
		}
		var diff map[string]any
		if err := json.Unmarshal([]byte(rows[0]["diff"].(string)), &diff); err != nil {
			t.Fatalf("diff decode: %v", err)
		}
		newRow := diff["new"].(map[string]any)
		if _, ok := newRow["secret"]; ok {
			t.Fatalf("SECURITY: [audit] default create audit diff leaked secret field: %+v", newRow)
		}
	})
}

func TestAudit_DefaultUpdateRedactsSensitiveFields(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *sql.DB, _ Dialect) {
		ta := TestHarness(t, auditAppWithRedact(t, db, nil))
		create := ta.Post("/posts", map[string]any{"title": "hello", "secret": "shh"})
		create.AssertStatus(t, http.StatusCreated)

		var created map[string]any
		if err := json.Unmarshal([]byte(create.Body()), &created); err != nil {
			t.Fatalf("decode create: %v", err)
		}

		ta.Put("/posts/"+created["id"].(string), map[string]any{"secret": "new-secret"}).AssertStatus(t, http.StatusOK)

		rows := readAuditRows(t, db)
		if len(rows) < 2 {
			t.Fatalf("expected at least 2 audit rows, got %d", len(rows))
		}
		var diff map[string]any
		if err := json.Unmarshal([]byte(rows[len(rows)-1]["diff"].(string)), &diff); err != nil {
			t.Fatalf("diff decode: %v", err)
		}
		newRow := diff["new"].(map[string]any)
		if _, ok := newRow["secret"]; ok {
			t.Fatalf("SECURITY: [audit] default update audit diff leaked secret field: %+v", newRow)
		}
	})
}

func TestAudit_CreateIncludesClientIPAddress(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *sql.DB, _ Dialect) {
		app := auditAppWithRedact(t, db, nil)
		req := httptest.NewRequest(http.MethodPost, "/posts", strings.NewReader(`{"title":"hello","secret":"shh"}`))
		req.Header.Set("Content-Type", "application/json")
		req.RemoteAddr = "198.51.100.44:1234"
		rec := httptest.NewRecorder()
		app.Router().ServeHTTP(rec, req)
		if rec.Code != http.StatusCreated {
			t.Fatalf("expected create status 201, got %d body=%s", rec.Code, rec.Body.String())
		}

		rows := readAuditRows(t, db)
		if len(rows) != 1 {
			t.Fatalf("expected 1 audit row, got %d", len(rows))
		}
		diff := rows[0]["diff"].(string)
		if !strings.Contains(diff, "198.51.100.44") {
			t.Fatalf("SECURITY: [audit] create audit row omitted client IP metadata: %s", diff)
		}
	})
}

func TestAudit_CreateIncludesUserAgent(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *sql.DB, _ Dialect) {
		app := auditAppWithRedact(t, db, nil)
		req := httptest.NewRequest(http.MethodPost, "/posts", strings.NewReader(`{"title":"hello","secret":"shh"}`))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("User-Agent", "security-test-agent/1.0")
		rec := httptest.NewRecorder()
		app.Router().ServeHTTP(rec, req)
		if rec.Code != http.StatusCreated {
			t.Fatalf("expected create status 201, got %d body=%s", rec.Code, rec.Body.String())
		}

		rows := readAuditRows(t, db)
		if len(rows) != 1 {
			t.Fatalf("expected 1 audit row, got %d", len(rows))
		}
		diff := rows[0]["diff"].(string)
		if !strings.Contains(diff, "security-test-agent/1.0") {
			t.Fatalf("SECURITY: [audit] create audit row omitted user-agent metadata: %s", diff)
		}
	})
}

func TestAudit_UpdateIncludesClientIPAddress(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *sql.DB, _ Dialect) {
		app := auditAppWithRedact(t, db, nil)
		ta := TestHarness(t, app)
		create := ta.Post("/posts", map[string]any{"title": "hello", "secret": "shh"})
		create.AssertStatus(t, http.StatusCreated)

		var created map[string]any
		if err := json.Unmarshal([]byte(create.Body()), &created); err != nil {
			t.Fatalf("decode create: %v", err)
		}

		req := httptest.NewRequest(http.MethodPut, "/posts/"+created["id"].(string), strings.NewReader(`{"title":"updated"}`))
		req.Header.Set("Content-Type", "application/json")
		req.RemoteAddr = "198.51.100.77:4444"
		req.SetPathValue("id", created["id"].(string))
		rec := httptest.NewRecorder()
		app.Router().ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("expected update status 200, got %d body=%s", rec.Code, rec.Body.String())
		}

		rows := readAuditRows(t, db)
		diff := rows[len(rows)-1]["diff"].(string)
		if !strings.Contains(diff, "198.51.100.77") {
			t.Fatalf("SECURITY: [audit] update audit row omitted client IP metadata: %s", diff)
		}
	})
}

func TestAudit_UpdateIncludesUserAgent(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *sql.DB, _ Dialect) {
		app := auditAppWithRedact(t, db, nil)
		ta := TestHarness(t, app)
		create := ta.Post("/posts", map[string]any{"title": "hello", "secret": "shh"})
		create.AssertStatus(t, http.StatusCreated)

		var created map[string]any
		if err := json.Unmarshal([]byte(create.Body()), &created); err != nil {
			t.Fatalf("decode create: %v", err)
		}

		req := httptest.NewRequest(http.MethodPut, "/posts/"+created["id"].(string), strings.NewReader(`{"title":"updated"}`))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("User-Agent", "security-test-agent/2.0")
		req.SetPathValue("id", created["id"].(string))
		rec := httptest.NewRecorder()
		app.Router().ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("expected update status 200, got %d body=%s", rec.Code, rec.Body.String())
		}

		rows := readAuditRows(t, db)
		diff := rows[len(rows)-1]["diff"].(string)
		if !strings.Contains(diff, "security-test-agent/2.0") {
			t.Fatalf("SECURITY: [audit] update audit row omitted user-agent metadata: %s", diff)
		}
	})
}

func TestAudit_UpdateIncludesPreviousValues(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *sql.DB, _ Dialect) {
		ta := TestHarness(t, auditAppWithRedact(t, db, nil))
		create := ta.Post("/posts", map[string]any{"title": "before", "secret": "shh"})
		create.AssertStatus(t, http.StatusCreated)

		var created map[string]any
		if err := json.Unmarshal([]byte(create.Body()), &created); err != nil {
			t.Fatalf("decode create: %v", err)
		}

		ta.Put("/posts/"+created["id"].(string), map[string]any{"title": "after"}).AssertStatus(t, http.StatusOK)

		rows := readAuditRows(t, db)
		diff := rows[len(rows)-1]["diff"].(string)
		if !strings.Contains(diff, "before") {
			t.Fatalf("SECURITY: [audit] update audit row omitted previous values: %s", diff)
		}
	})
}

func TestAudit_DeleteIncludesDeletedRecordSnapshot(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *sql.DB, _ Dialect) {
		ta := TestHarness(t, auditAppWithRedact(t, db, nil))
		create := ta.Post("/posts", map[string]any{"title": "to-delete", "secret": "shh"})
		create.AssertStatus(t, http.StatusCreated)

		var created map[string]any
		if err := json.Unmarshal([]byte(create.Body()), &created); err != nil {
			t.Fatalf("decode create: %v", err)
		}

		ta.Delete("/posts/"+created["id"].(string)).AssertStatus(t, http.StatusNoContent)

		rows := readAuditRows(t, db)
		last := rows[len(rows)-1]
		diff, _ := last["diff"].(string)
		if !strings.Contains(diff, "to-delete") {
			t.Fatalf("SECURITY: [audit] delete audit row omitted deleted record snapshot: %+v", last)
		}
	})
}

func TestAudit_DeleteIncludesClientIPAddress(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *sql.DB, _ Dialect) {
		app := auditAppWithRedact(t, db, nil)
		ta := TestHarness(t, app)
		create := ta.Post("/posts", map[string]any{"title": "to-delete", "secret": "shh"})
		create.AssertStatus(t, http.StatusCreated)

		var created map[string]any
		if err := json.Unmarshal([]byte(create.Body()), &created); err != nil {
			t.Fatalf("decode create: %v", err)
		}

		req := httptest.NewRequest(http.MethodDelete, "/posts/"+created["id"].(string), nil)
		req.RemoteAddr = "198.51.100.88:7777"
		req.SetPathValue("id", created["id"].(string))
		rec := httptest.NewRecorder()
		app.Router().ServeHTTP(rec, req)
		if rec.Code != http.StatusNoContent {
			t.Fatalf("expected delete status 204, got %d body=%s", rec.Code, rec.Body.String())
		}

		rows := readAuditRows(t, db)
		last := rows[len(rows)-1]
		diff, _ := last["diff"].(string)
		if !strings.Contains(diff, "198.51.100.88") {
			t.Fatalf("SECURITY: [audit] delete audit row omitted client IP metadata: %+v", last)
		}
	})
}

func TestAudit_DeleteIncludesUserAgent(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *sql.DB, _ Dialect) {
		app := auditAppWithRedact(t, db, nil)
		ta := TestHarness(t, app)
		create := ta.Post("/posts", map[string]any{"title": "to-delete", "secret": "shh"})
		create.AssertStatus(t, http.StatusCreated)

		var created map[string]any
		if err := json.Unmarshal([]byte(create.Body()), &created); err != nil {
			t.Fatalf("decode create: %v", err)
		}

		req := httptest.NewRequest(http.MethodDelete, "/posts/"+created["id"].(string), nil)
		req.Header.Set("User-Agent", "security-test-agent/3.0")
		req.SetPathValue("id", created["id"].(string))
		rec := httptest.NewRecorder()
		app.Router().ServeHTTP(rec, req)
		if rec.Code != http.StatusNoContent {
			t.Fatalf("expected delete status 204, got %d body=%s", rec.Code, rec.Body.String())
		}

		rows := readAuditRows(t, db)
		last := rows[len(rows)-1]
		diff, _ := last["diff"].(string)
		if !strings.Contains(diff, "security-test-agent/3.0") {
			t.Fatalf("SECURITY: [audit] delete audit row omitted user-agent metadata: %+v", last)
		}
	})
}
