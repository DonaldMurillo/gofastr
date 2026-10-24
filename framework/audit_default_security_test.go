package framework

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
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
	keys.Config.PrimaryKey = []string{"api_key"}
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

// A host Redact remaps the delete row's id and nothing else: create and
// update rows keep the raw id the Activity tab and the admin's live
// record link look up, as rows written before this change did.
func TestAudit_RedactRemapsDeleteOnly(t *testing.T) {
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

		want := map[string]string{"create": "k1", "update": "k1", "delete": "p:K1"}
		for _, r := range readAuditRows(t, db) {
			if r["record_id"] != want[r["op"].(string)] {
				t.Fatalf("%s row record_id = %q, want %q", r["op"], r["record_id"], want[r["op"].(string)])
			}
		}
		trail, err := entityUIHost{a: app}.Audit().Trail(context.Background(), "keys", "k1", 0)
		if err != nil || len(trail) != 2 {
			t.Fatalf("Activity tab found %d rows (%v), want 2", len(trail), err)
		}
	})
}

// A key column is sensitive under its WireName too, and every mapped id
// loses its control bytes, hashed or not, so the Activity tab's lookup
// matches what the hooks wrote.
func TestAuditKeyID(t *testing.T) {
	plain := entity.Define("p", entity.EntityConfig{Fields: []schema.Field{
		{Name: "user_code", Type: schema.String},
	}})
	plain.Config.PrimaryKey = []string{"user_code"}
	secret := entity.Define("s", entity.EntityConfig{Fields: []schema.Field{
		{Name: "user_code", Type: schema.String, WireName: "apiKey"},
	}})
	secret.Config.PrimaryKey = []string{"user_code"}
	var cfg AuditConfig
	if got := cfg.keyID(versionKey(secret), "\n"); got != "" {
		t.Fatalf("an id with nothing left after sanitizing: got %q, want empty", got)
	}
	if got := cfg.keyID(versionKey(secret), "sk_live"); !strings.HasPrefix(got, "sha256:") {
		t.Fatalf("WireName apiKey: record_id = %q, want a sha256 digest", got)
	}
	if got := cfg.keyID(versionKey(plain), "a\nb\x00c"); got != "abc" {
		t.Fatalf("control bytes: got %q", got)
	}
	if cfg.keyID(versionKey(secret), "a\nb") != cfg.keyID(versionKey(secret), "ab") {
		t.Fatal("a hashed id keeps the control bytes the plain path strips")
	}
	cfg.Redact = func(_ string, row map[string]any) map[string]any { return row }
	if got := cfg.keyID(versionKey(secret), "k\n1"); got != "k1" {
		t.Fatalf("with Redact: got %q, want the sanitized raw id", got)
	}
}

// The hooks are keyed by name, so a v2 row reaches the hook v1's fields
// built: the key is read under each version's wire key, and a version's
// alias that is another version's ordinary field does not win.
func TestAuditKeyEveryVersion(t *testing.T) {
	reg := NewRegistry()
	for v, wires := range map[string][2]string{
		"/api/v1": {"legacyId", "externalId"},
		"/api/v2": {"newId", "legacyId"},
	} {
		e := entity.Define("records", entity.EntityConfig{Fields: []schema.Field{
			{Name: "id", Type: schema.String, WireName: wires[0]},
			{Name: "external_id", Type: schema.String, WireName: wires[1]},
		}}.WithTimestamps(false))
		e.Version = v
		if err := reg.Register(e); err != nil {
			t.Fatalf("register %s: %v", v, err)
		}
	}
	k := auditKeyOf(reg, "records")
	for _, row := range []map[string]any{
		{"legacyId": "r1", "externalId": "x9"},
		{"newId": "r1", "legacyId": "x9"},
	} {
		if got := stringifyPK(row, k); got != "r1" {
			t.Fatalf("stringifyPK(%v) = %q, want r1", row, got)
		}
	}
}

// Two versions that give the key and another column each other's wire
// names write rows no reading can tell apart. The handler names the
// record from its own key columns, so the audit row does too.
func TestAuditSwappedWireNames(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *sql.DB, _ Dialect) {
		app := NewApp(WithDB(db), WithoutDefaultMiddleware())
		var v2 *entity.Entity
		for v, wires := range map[string][2]string{
			"/api/v1": {"legacyId", "newId"},
			"/api/v2": {"newId", "legacyId"},
		} {
			e := entity.Define("records", entity.EntityConfig{
				Table:    "records",
				Exposure: &entity.ExposureConfig{Public: true},
				Fields: []schema.Field{
					{Name: "id", Type: schema.String, Required: true, WireName: wires[0]},
					{Name: "external_id", Type: schema.String, WireName: wires[1]},
				},
			}.WithTimestamps(false))
			e.Version = v
			if err := app.Registry.Register(e); err != nil {
				t.Fatalf("register %s: %v", v, err)
			}
			if v == "/api/v2" {
				v2 = e
			}
		}
		if err := AutoMigrate(db, app.Registry); err != nil {
			t.Fatalf("automigrate: %v", err)
		}
		ch := crud.NewCrudHandler(v2, db)
		ch.Hooks = app.HookRegistry("records")
		crud.RegisterCrudRoutes(app.router, ch, "/records", crud.CrudRouteOptions{})
		app.WithAuditLog(AuditConfig{})
		ta := TestHarness(t, app)
		ta.Post("/records", map[string]any{"newId": "r1", "legacyId": "x9"}).AssertStatus(t, http.StatusCreated)
		ta.Put("/records/r1", map[string]any{"legacyId": "x8"}).AssertStatus(t, http.StatusOK)
		rows := readAuditRows(t, db)
		if len(rows) != 2 {
			t.Fatalf("expected 2 audit rows, got %d", len(rows))
		}
		for _, r := range rows {
			if r["record_id"] != "r1" {
				t.Fatalf("%s row names %v, want r1", r["op"], r["record_id"])
			}
		}
	})
}

func TestAuditDigestID(t *testing.T) {
	sum := sha256.Sum256([]byte("k1"))
	digest := "sha256:" + hex.EncodeToString(sum[:])
	if !AuditDigestID(digest) {
		t.Fatalf("%q is not read as a digest", digest)
	}
	for _, id := range []string{"k1", "sha256:", "sha256:zz", digest + "0", digest[:len(digest)-1]} {
		if AuditDigestID(id) {
			t.Fatalf("%q is read as a digest", id)
		}
	}
}
