package framework

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/crud"
	"github.com/DonaldMurillo/gofastr/framework/entity"
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
		if err := json.Unmarshal([]byte(create.Body()), singleMap(&created)); err != nil {
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
		if err := json.Unmarshal([]byte(create.Body()), singleMap(&created)); err != nil {
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
		if err := json.Unmarshal([]byte(create.Body()), singleMap(&created)); err != nil {
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
		if err := json.Unmarshal([]byte(create.Body()), singleMap(&created)); err != nil {
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
		if err := json.Unmarshal([]byte(create.Body()), singleMap(&created)); err != nil {
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
		if err := json.Unmarshal([]byte(create.Body()), singleMap(&created)); err != nil {
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
		if err := json.Unmarshal([]byte(create.Body()), singleMap(&created)); err != nil {
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

// auditKeyedApp registers "keys", an entity whose primary key is a
// credential, so the audit trail's record_id is the secret itself.
func auditKeyedApp(t *testing.T, db *sql.DB, redact func(string, map[string]any) map[string]any) *App {
	t.Helper()
	app := NewApp(WithDB(db), WithoutDefaultMiddleware())
	// App.Entity always keys on "id", so the handler is wired by hand.
	keys := entity.Define("keys", entity.EntityConfig{
		Table:    "keys",
		Exposure: &entity.ExposureConfig{Public: true},
		Fields: []schema.Field{
			{Name: "api_key", Type: schema.String, Required: true},
			{Name: "label", Type: schema.String},
		},
	}.WithTimestamps(false))
	keys.PrimaryKey = "api_key"
	if err := app.Registry.Register(keys); err != nil {
		t.Fatal(err)
	}
	if err := AutoMigrate(db, app.Registry); err != nil {
		t.Fatalf("automigrate: %v", err)
	}
	ch := crud.NewCrudHandler(keys, db)
	ch.Hooks = app.HookRegistry("keys")
	crud.RegisterCrudRoutes(app.router, ch, "/keys", crud.CrudRouteOptions{})
	app.WithAuditLog(AuditConfig{Redact: redact})
	return app
}

func TestAudit_SecretKeyStaysOutOfRecordID(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *sql.DB, _ Dialect) {
		app := auditKeyedApp(t, db, nil)
		ta := TestHarness(t, app)
		ta.Post("/keys", map[string]any{"api_key": "sk_live_secret", "label": "a"}).AssertStatus(t, http.StatusCreated)
		ta.Put("/keys/sk_live_secret", map[string]any{"label": "b"}).AssertStatus(t, http.StatusOK)
		ta.Delete("/keys/sk_live_secret").AssertStatus(t, http.StatusNoContent)

		rows := readAuditRows(t, db)
		if len(rows) != 3 {
			t.Fatalf("expected 3 audit rows, got %d", len(rows))
		}
		for _, r := range rows {
			if strings.Contains(fmt.Sprint(r), "sk_live_secret") {
				t.Fatalf("SECURITY: [audit] %s row leaked the key: %+v", r["op"], r)
			}
			if r["record_id"] != rows[0]["record_id"] {
				t.Fatalf("record ids differ across ops: %+v", rows)
			}
		}
		trail, err := entityUIHost{a: app}.Audit().Trail(context.Background(), "keys", "sk_live_secret", 0)
		if err != nil || len(trail) != 3 {
			t.Fatalf("Activity tab found %d rows (%v), want 3", len(trail), err)
		}
	})
}

func TestAudit_RedactNamesEveryOp(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *sql.DB, _ Dialect) {
		pseudo := func(_ string, row map[string]any) map[string]any {
			if id, ok := row["id"].(string); ok && len(row) == 1 {
				row["id"] = "p:" + strings.ToUpper(id)
			}
			return row
		}
		app := auditKeyedApp(t, db, pseudo)
		ta := TestHarness(t, app)
		ta.Post("/keys", map[string]any{"api_key": "k1", "label": "a"}).AssertStatus(t, http.StatusCreated)
		ta.Put("/keys/k1", map[string]any{"label": "b"}).AssertStatus(t, http.StatusOK)
		ta.Delete("/keys/k1").AssertStatus(t, http.StatusNoContent)

		for _, r := range readAuditRows(t, db) {
			if r["record_id"] != "p:K1" {
				t.Fatalf("%s row record_id = %q, want p:K1", r["op"], r["record_id"])
			}
		}
		trail, err := entityUIHost{a: app}.Audit().Trail(context.Background(), "keys", "k1", 0)
		if err != nil || len(trail) != 3 {
			t.Fatalf("Activity tab found %d rows (%v), want 3", len(trail), err)
		}
	})
}
