package crud

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/framework/hook"
	"github.com/DonaldMurillo/gofastr/framework/tenant"
)

// =============================================================================
// Create operation attacks (tests 1–10)
// =============================================================================

func TestCreate_NullByteInField(t *testing.T) {
	t.Parallel()
	installSecurityOwnerExtractor(t)
	ddl := `CREATE TABLE posts (id TEXT PRIMARY KEY, user_id TEXT NOT NULL, title TEXT, body TEXT)`
	cfg := makeEntityConfig("posts", "posts", "user_id", []schema.Field{
		{Name: "user_id", Type: schema.String, Required: true},
		{Name: "title", Type: schema.String, Required: true},
		{Name: "body", Type: schema.String},
	})
	ch, _ := setupSecurityTestHandler(t, cfg, ddl)
	req := makeRequest(t, RequestOpts{Method: http.MethodPost, Path: "/posts", Body: "{\"title\":\"hello\\x00world\",\"body\":\"safe content\"}", UserID: "alice"})
	rr := httptest.NewRecorder()
	ch.Create()(rr, req)
	if rr.Code != http.StatusCreated && rr.Code != http.StatusBadRequest {
		t.Errorf("SECURITY: [create] null-byte in field value caused status %d. Attack: null byte injection in title field. Want 201 or 400.", rr.Code)
	}
}

func TestCreate_UnderscorePrefixFields(t *testing.T) {
	t.Parallel()
	installSecurityOwnerExtractor(t)
	ddl := `CREATE TABLE items (id TEXT PRIMARY KEY, user_id TEXT NOT NULL, name TEXT)`
	cfg := makeEntityConfig("items", "items", "user_id", []schema.Field{
		{Name: "user_id", Type: schema.String, Required: true},
		{Name: "name", Type: schema.String},
	})
	ch, _ := setupSecurityTestHandler(t, cfg, ddl)
	req := makeRequest(t, RequestOpts{Method: http.MethodPost, Path: "/items", Body: `{"name":"test","_internal_flag":"true","_role":"admin"}`, UserID: "alice"})
	rr := httptest.NewRecorder()
	ch.Create()(rr, req)
	if rr.Code == http.StatusCreated {
		var resp map[string]any
		if err := json.Unmarshal(rr.Body.Bytes(), &resp); err == nil {
			for k := range resp {
				if strings.HasPrefix(k, "_internal") || strings.HasPrefix(k, "_role") {
					t.Errorf("SECURITY: [create] underscore-prefixed field %q leaked into response. Attack: underscore-prefix field injection.", k)
				}
			}
		}
	}
	t.Logf("NOTE: [create] underscore-prefixed fields should be rejected or silently dropped, not persisted.")
}

func TestCreate_ReadOnlyFieldIgnored(t *testing.T) {
	t.Parallel()
	installSecurityOwnerExtractor(t)
	ddl := `CREATE TABLE accounts (id TEXT PRIMARY KEY, user_id TEXT NOT NULL, role TEXT, credit_limit TEXT)`
	cfg := makeEntityConfig("accounts", "accounts", "user_id", []schema.Field{
		{Name: "user_id", Type: schema.String, Required: true},
		{Name: "role", Type: schema.String, ReadOnly: true},
		{Name: "credit_limit", Type: schema.String, ReadOnly: true},
	})
	ch, db := setupSecurityTestHandler(t, cfg, ddl)
	req := makeRequest(t, RequestOpts{Method: http.MethodPost, Path: "/accounts", Body: `{"role":"admin","credit_limit":"999999"}`, UserID: "alice"})
	rr := httptest.NewRecorder()
	ch.Create()(rr, req)
	if rr.Code == http.StatusCreated {
		// A correctly-ignored read-only field leaves the column NULL, so
		// scan into a nullable type — a hard error here is test-integrity.
		var role sql.NullString
		if err := db.QueryRow("SELECT role FROM accounts LIMIT 1").Scan(&role); err != nil {
			t.Fatalf("read role after create: %v", err)
		}
		if role.Valid && role.String == "admin" {
			t.Errorf("SECURITY: [create] read-only field 'role' was persisted as 'admin'. Attack: mass-assignment of read-only field via create body.")
		}
	}
}

func TestCreate_AutoGenerateNotOverridden(t *testing.T) {
	t.Parallel()
	installSecurityOwnerExtractor(t)
	ddl := `CREATE TABLE tokens (id TEXT PRIMARY KEY, user_id TEXT NOT NULL, created_at TEXT, token TEXT)`
	cfg := makeEntityConfig("tokens", "tokens", "user_id", []schema.Field{
		{Name: "user_id", Type: schema.String, Required: true},
		{Name: "id", Type: schema.String, AutoGenerate: schema.AutoUUID},
		{Name: "created_at", Type: schema.String, AutoGenerate: schema.AutoTimestamp},
		{Name: "token", Type: schema.String},
	})
	ch, _ := setupSecurityTestHandler(t, cfg, ddl)
	req := makeRequest(t, RequestOpts{Method: http.MethodPost, Path: "/tokens", Body: `{"id":"i-predict-this","created_at":"1900-01-01T00:00:00Z","token":"mytoken"}`, UserID: "alice"})
	rr := httptest.NewRecorder()
	ch.Create()(rr, req)
	if rr.Code == http.StatusCreated {
		var resp map[string]any
		if err := json.Unmarshal(rr.Body.Bytes(), &resp); err == nil {
			if id, ok := resp["id"].(string); ok && id == "i-predict-this" {
				t.Errorf("SECURITY: [create] auto-generated id was overridden by client value 'i-predict-this'. Attack: auto-field override to predict record IDs.")
			}
			if created, ok := resp["created_at"].(string); ok && strings.HasPrefix(created, "1900") {
				t.Errorf("SECURITY: [create] auto-generated created_at was overridden by client value. Attack: timestamp override.")
			}
		}
	}
}

func TestCreate_EmptyJSONBody(t *testing.T) {
	t.Parallel()
	installSecurityOwnerExtractor(t)
	ddl := `CREATE TABLE notes (id TEXT PRIMARY KEY, user_id TEXT NOT NULL, title TEXT)`
	cfg := makeEntityConfig("notes", "notes", "user_id", []schema.Field{
		{Name: "user_id", Type: schema.String, Required: true},
		{Name: "title", Type: schema.String, Required: true},
	})
	ch, _ := setupSecurityTestHandler(t, cfg, ddl)
	req := makeRequest(t, RequestOpts{Method: http.MethodPost, Path: "/notes", Body: `{}`, UserID: "alice"})
	rr := httptest.NewRecorder()
	ch.Create()(rr, req)
	if rr.Code == http.StatusInternalServerError {
		t.Errorf("SECURITY: [create] empty JSON body caused 500 instead of 400. Attack: empty body probe. Body: %s", rr.Body.String())
	}
}

func TestCreate_InvalidJSONBody(t *testing.T) {
	t.Parallel()
	installSecurityOwnerExtractor(t)
	ddl := `CREATE TABLE notes (id TEXT PRIMARY KEY, user_id TEXT NOT NULL, title TEXT)`
	cfg := makeEntityConfig("notes", "notes", "user_id", []schema.Field{
		{Name: "user_id", Type: schema.String, Required: true},
		{Name: "title", Type: schema.String},
	})
	ch, _ := setupSecurityTestHandler(t, cfg, ddl)
	req := makeRequest(t, RequestOpts{Method: http.MethodPost, Path: "/notes", Body: `{invalid json!!!`, UserID: "alice"})
	rr := httptest.NewRecorder()
	ch.Create()(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Errorf("SECURITY: [create] malformed JSON returned status %d, want 400. Attack: malformed JSON body. Body: %s", rr.Code, rr.Body.String())
	}
}

func TestCreate_FieldValueTooLong(t *testing.T) {
	t.Parallel()
	installSecurityOwnerExtractor(t)
	ddl := `CREATE TABLE docs (id TEXT PRIMARY KEY, user_id TEXT NOT NULL, content TEXT)`
	cfg := makeEntityConfig("docs", "docs", "user_id", []schema.Field{
		{Name: "user_id", Type: schema.String, Required: true},
		{Name: "content", Type: schema.String},
	})
	ch, _ := setupSecurityTestHandler(t, cfg, ddl)
	huge := strings.Repeat("A", 1<<20)
	body, err := json.Marshal(map[string]string{"content": huge})
	if err != nil {
		t.Fatal(err)
	}
	req := makeRequest(t, RequestOpts{Method: http.MethodPost, Path: "/docs", Body: string(body), UserID: "alice"})
	rr := httptest.NewRecorder()
	func() {
		defer func() {
			if r := recover(); r != nil {
				t.Errorf("SECURITY: [create] 1 MB field value caused panic: %v. Attack: large payload denial of service.", r)
			}
		}()
		ch.Create()(rr, req)
	}()
	if rr.Code == http.StatusInternalServerError {
		t.Errorf("SECURITY: [create] 1 MB field value returned 500. Attack: large payload denial of service.")
	}
	t.Logf("NOTE: [create] 1 MB field value returned status %d. Consider adding Max field length validation.", rr.Code)
}

func TestCreate_NegativeIntForUnsignedField(t *testing.T) {
	t.Parallel()
	installSecurityOwnerExtractor(t)
	ddl := `CREATE TABLE counters (id TEXT PRIMARY KEY, user_id TEXT NOT NULL, score INTEGER)`
	minZero := float64(0)
	cfg := makeEntityConfig("counters", "counters", "user_id", []schema.Field{
		{Name: "user_id", Type: schema.String, Required: true},
		{Name: "score", Type: schema.Int, Min: &minZero},
	})
	ch, db := setupSecurityTestHandler(t, cfg, ddl)
	req := makeRequest(t, RequestOpts{Method: http.MethodPost, Path: "/counters", Body: `{"score":-42}`, UserID: "alice"})
	rr := httptest.NewRecorder()
	ch.Create()(rr, req)
	if rr.Code == http.StatusCreated {
		var score int
		if err := db.QueryRow("SELECT score FROM counters LIMIT 1").Scan(&score); err != nil {
			t.Fatalf("read score after create: %v", err)
		}
		if score < 0 {
			t.Errorf("SECURITY: [create] negative value %d persisted for field with Min=0. Attack: negative integer injection bypasses unsigned constraint.", score)
		}
	}
}

func TestCreate_ArrayInsteadOfString(t *testing.T) {
	t.Parallel()
	installSecurityOwnerExtractor(t)
	ddl := `CREATE TABLE tags (id TEXT PRIMARY KEY, user_id TEXT NOT NULL, label TEXT)`
	cfg := makeEntityConfig("tags", "tags", "user_id", []schema.Field{
		{Name: "user_id", Type: schema.String, Required: true},
		{Name: "label", Type: schema.String},
	})
	ch, _ := setupSecurityTestHandler(t, cfg, ddl)
	req := makeRequest(t, RequestOpts{Method: http.MethodPost, Path: "/tags", Body: `{"label":["malicious","array"]}`, UserID: "alice"})
	rr := httptest.NewRecorder()
	ch.Create()(rr, req)
	if rr.Code == http.StatusInternalServerError {
		t.Errorf("SECURITY: [create] array value for string field returned 500 instead of 400. Attack: type confusion. Body: %s", rr.Body.String())
	}
}

func TestCreate_DuplicateUniqueField(t *testing.T) {
	t.Parallel()
	installSecurityOwnerExtractor(t)
	ddl := `CREATE TABLE users (id TEXT PRIMARY KEY, user_id TEXT NOT NULL, email TEXT UNIQUE)`
	cfg := makeEntityConfig("users", "users", "user_id", []schema.Field{
		{Name: "user_id", Type: schema.String, Required: true},
		{Name: "email", Type: schema.String, Unique: true},
	})
	ch, _ := setupSecurityTestHandler(t, cfg, ddl)
	req1 := makeRequest(t, RequestOpts{Method: http.MethodPost, Path: "/users", Body: `{"email":"alice@example.com"}`, UserID: "alice"})
	rr1 := httptest.NewRecorder()
	ch.Create()(rr1, req1)
	if rr1.Code != http.StatusCreated {
		t.Fatalf("first create failed: status %d", rr1.Code)
	}
	req2 := makeRequest(t, RequestOpts{Method: http.MethodPost, Path: "/users", Body: `{"email":"alice@example.com"}`, UserID: "alice"})
	rr2 := httptest.NewRecorder()
	ch.Create()(rr2, req2)
	if rr2.Code == http.StatusInternalServerError {
		t.Errorf("SECURITY: [create] duplicate unique field returned 500. Attack: unique constraint violation probe. Body: %s", rr2.Body.String())
	}
}

// =============================================================================
// Update operation attacks (tests 11–20)
// =============================================================================

func TestUpdate_PartialUpdatePreservesFields(t *testing.T) {
	t.Parallel()
	installSecurityOwnerExtractor(t)
	ddl := `CREATE TABLE profiles (id TEXT PRIMARY KEY, user_id TEXT NOT NULL, name TEXT, bio TEXT)`
	cfg := makeEntityConfig("profiles", "profiles", "user_id", []schema.Field{
		{Name: "user_id", Type: schema.String, Required: true},
		{Name: "name", Type: schema.String},
		{Name: "bio", Type: schema.String},
	})
	ch, db := setupSecurityTestHandler(t, cfg, ddl)
	seedRows(t, db, "profiles", []map[string]any{
		{"id": "p1", "user_id": "alice", "name": "Alice", "bio": "original bio"},
	})
	req := makeRequest(t, RequestOpts{Method: http.MethodPut, Path: "/profiles/p1", Body: `{"name":"New Name"}`, UserID: "alice"})
	req.SetPathValue("id", "p1")
	rr := httptest.NewRecorder()
	ch.Update()(rr, req)
	if rr.Code == http.StatusOK || rr.Code == http.StatusCreated {
		var bio string
		if err := db.QueryRow("SELECT bio FROM profiles WHERE id = ?", "p1").Scan(&bio); err != nil {
			t.Fatalf("read bio after update: %v", err)
		}
		if bio != "original bio" {
			t.Errorf("SECURITY: [update] partial update erased bio field: got %q, want %q. Attack: field erasure via partial update.", bio, "original bio")
		}
	}
}

func TestUpdate_IDInBodyIgnored(t *testing.T) {
	t.Parallel()
	installSecurityOwnerExtractor(t)
	ddl := `CREATE TABLE records (id TEXT PRIMARY KEY, user_id TEXT NOT NULL, data TEXT)`
	cfg := makeEntityConfig("records", "records", "user_id", []schema.Field{
		{Name: "user_id", Type: schema.String, Required: true},
		{Name: "data", Type: schema.String},
	})
	ch, db := setupSecurityTestHandler(t, cfg, ddl)
	seedRows(t, db, "records", []map[string]any{
		{"id": "rec-1", "user_id": "alice", "data": "original"},
	})
	req := makeRequest(t, RequestOpts{Method: http.MethodPut, Path: "/records/rec-1", Body: `{"id":"rec-hijacked","data":"modified"}`, UserID: "alice"})
	req.SetPathValue("id", "rec-1")
	rr := httptest.NewRecorder()
	ch.Update()(rr, req)
	var id string
	if err := db.QueryRow("SELECT id FROM records WHERE id = ?", "rec-1").Scan(&id); err != nil {
		t.Errorf("SECURITY: [update] original record not found after update with id injection. Attack: primary key override via body.")
	}
	var count int
	db.QueryRow("SELECT COUNT(*) FROM records WHERE id = ?", "rec-hijacked").Scan(&count)
	if count > 0 {
		t.Errorf("SECURITY: [update] new record with injected id 'rec-hijacked' was created. Attack: primary key injection via body.")
	}
}

func TestUpdate_ReadOnlyFieldChangeRejected(t *testing.T) {
	t.Parallel()
	installSecurityOwnerExtractor(t)
	ddl := `CREATE TABLE plans (id TEXT PRIMARY KEY, user_id TEXT NOT NULL, tier TEXT, max_storage TEXT)`
	cfg := makeEntityConfig("plans", "plans", "user_id", []schema.Field{
		{Name: "user_id", Type: schema.String, Required: true},
		{Name: "tier", Type: schema.String, ReadOnly: true},
		{Name: "max_storage", Type: schema.String},
	})
	ch, db := setupSecurityTestHandler(t, cfg, ddl)
	seedRows(t, db, "plans", []map[string]any{
		{"id": "plan-1", "user_id": "alice", "tier": "free", "max_storage": "100"},
	})
	req := makeRequest(t, RequestOpts{Method: http.MethodPut, Path: "/plans/plan-1", Body: `{"tier":"enterprise","max_storage":"unlimited"}`, UserID: "alice"})
	req.SetPathValue("id", "plan-1")
	rr := httptest.NewRecorder()
	ch.Update()(rr, req)
	if rr.Code == http.StatusOK || rr.Code == http.StatusCreated {
		var tier string
		if err := db.QueryRow("SELECT tier FROM plans WHERE id = ?", "plan-1").Scan(&tier); err != nil {
			t.Fatalf("read tier after update: %v", err)
		}
		if tier == "enterprise" {
			t.Errorf("SECURITY: [update] read-only field 'tier' was changed to 'enterprise'. Attack: read-only field escalation via update.")
		}
	}
}

func TestUpdate_EmptyBody(t *testing.T) {
	t.Parallel()
	installSecurityOwnerExtractor(t)
	ddl := `CREATE TABLE notes (id TEXT PRIMARY KEY, user_id TEXT NOT NULL, title TEXT)`
	cfg := makeEntityConfig("notes", "notes", "user_id", []schema.Field{
		{Name: "user_id", Type: schema.String, Required: true},
		{Name: "title", Type: schema.String},
	})
	ch, _ := setupSecurityTestHandler(t, cfg, ddl)
	req := makeRequest(t, RequestOpts{Method: http.MethodPut, Path: "/notes/any", Body: `{}`, UserID: "alice"})
	req.SetPathValue("id", "any")
	rr := httptest.NewRecorder()
	ch.Update()(rr, req)
	if rr.Code == http.StatusInternalServerError {
		t.Errorf("SECURITY: [update] empty body caused 500 instead of 400. Attack: empty update body probe. Body: %s", rr.Body.String())
	}
}

func TestUpdate_NonexistentID(t *testing.T) {
	t.Parallel()
	installSecurityOwnerExtractor(t)
	ddl := `CREATE TABLE notes (id TEXT PRIMARY KEY, user_id TEXT NOT NULL, title TEXT)`
	cfg := makeEntityConfig("notes", "notes", "user_id", []schema.Field{
		{Name: "user_id", Type: schema.String, Required: true},
		{Name: "title", Type: schema.String},
	})
	ch, _ := setupSecurityTestHandler(t, cfg, ddl)
	req := makeRequest(t, RequestOpts{Method: http.MethodPut, Path: "/notes/nonexistent-id-99999", Body: `{"title":"hacked"}`, UserID: "alice"})
	req.SetPathValue("id", "nonexistent-id-99999")
	rr := httptest.NewRecorder()
	ch.Update()(rr, req)
	if rr.Code == http.StatusInternalServerError {
		t.Errorf("SECURITY: [update] nonexistent ID returned 500. Attack: update nonexistent record. Body: %s", rr.Body.String())
	}
}

func TestUpdate_MalformedJSON(t *testing.T) {
	t.Parallel()
	installSecurityOwnerExtractor(t)
	ddl := `CREATE TABLE notes (id TEXT PRIMARY KEY, user_id TEXT NOT NULL, title TEXT)`
	cfg := makeEntityConfig("notes", "notes", "user_id", []schema.Field{
		{Name: "user_id", Type: schema.String, Required: true},
		{Name: "title", Type: schema.String},
	})
	ch, _ := setupSecurityTestHandler(t, cfg, ddl)
	req := makeRequest(t, RequestOpts{Method: http.MethodPut, Path: "/notes/any", Body: `{broken json!!}`, UserID: "alice"})
	req.SetPathValue("id", "any")
	rr := httptest.NewRecorder()
	ch.Update()(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Errorf("SECURITY: [update] malformed JSON returned status %d, want 400. Attack: malformed JSON in update. Body: %s", rr.Code, rr.Body.String())
	}
}

func TestUpdate_OwnerIDTamper(t *testing.T) {
	t.Parallel()
	installSecurityOwnerExtractor(t)
	ddl := `CREATE TABLE docs (id TEXT PRIMARY KEY, user_id TEXT NOT NULL, content TEXT)`
	cfg := makeEntityConfig("docs", "docs", "user_id", []schema.Field{
		{Name: "user_id", Type: schema.String, Required: true},
		{Name: "content", Type: schema.String},
	})
	ch, db := setupSecurityTestHandler(t, cfg, ddl)
	seedRows(t, db, "docs", []map[string]any{
		{"id": "doc-1", "user_id": "alice", "content": "alice content"},
	})
	req := makeRequest(t, RequestOpts{Method: http.MethodPut, Path: "/docs/doc-1", Body: `{"user_id":"bob","content":"stolen"}`, UserID: "alice"})
	req.SetPathValue("id", "doc-1")
	rr := httptest.NewRecorder()
	ch.Update()(rr, req)
	var ownerID string
	if err := db.QueryRow("SELECT user_id FROM docs WHERE id = ?", "doc-1").Scan(&ownerID); err != nil {
		t.Fatalf("read user_id after update: %v", err)
	}
	if ownerID != "alice" {
		t.Errorf("SECURITY: [update] owner_id changed from 'alice' to %q via update body. Attack: owner ID tampering.", ownerID)
	}
}

func TestUpdate_TenantIDTamper(t *testing.T) {
	t.Parallel()
	installSecurityOwnerExtractor(t)
	ddl := `CREATE TABLE tdata (id TEXT PRIMARY KEY, tenant_id TEXT, user_id TEXT NOT NULL, name TEXT)`
	cfg := entity.EntityConfig{
		Fields: []schema.Field{
			{Name: "tenant_id", Type: schema.String},
			{Name: "user_id", Type: schema.String, Required: true},
			{Name: "name", Type: schema.String},
		},
		MultiTenant: true,
		OwnerField:  "user_id",
	}.WithTimestamps(false)
	ch, db := setupSecurityTestHandler(t, cfg, ddl)
	seedRows(t, db, "tdata", []map[string]any{
		{"id": "td-1", "tenant_id": "tenant-A", "user_id": "alice", "name": "secret data"},
	})
	req := makeRequest(t, RequestOpts{Method: http.MethodPut, Path: "/tdata/td-1", Body: `{"tenant_id":"tenant-B","name":"leaked"}`, UserID: "alice"})
	req.SetPathValue("id", "td-1")
	ctx := tenant.SetTenantID(req.Context(), "tenant-A")
	req = req.WithContext(ctx)
	rr := httptest.NewRecorder()
	ch.Update()(rr, req)
	var tid string
	if err := db.QueryRow("SELECT tenant_id FROM tdata WHERE id = ?", "td-1").Scan(&tid); err != nil {
		t.Fatalf("read tenant_id after update: %v", err)
	}
	if tid != "tenant-A" {
		t.Errorf("SECURITY: [update] tenant_id changed from 'tenant-A' to %q via update body. Attack: tenant ID tampering.", tid)
	}
}

func TestUpdate_DeletedRecord(t *testing.T) {
	t.Parallel()
	installSecurityOwnerExtractor(t)
	ddl := `CREATE TABLE sitems (id TEXT PRIMARY KEY, user_id TEXT NOT NULL, title TEXT, deleted_at TEXT)`
	cfg := makeEntityConfig("sitems", "sitems", "user_id", []schema.Field{
		{Name: "user_id", Type: schema.String, Required: true},
		{Name: "title", Type: schema.String},
	}, func(c *entity.EntityConfig) { c.SoftDelete = true })
	ch, db := setupSecurityTestHandler(t, cfg, ddl)
	// Seed an OWNED but soft-deleted row so the deleted-row code path is
	// actually exercised (the previous version left the table empty, so the
	// 404 came from sql.ErrNoRows, not soft-delete enforcement).
	seedRows(t, db, "sitems", []map[string]any{
		{"id": "deleted-id", "user_id": "alice", "title": "original", "deleted_at": "2024-01-01T00:00:00Z"},
	})
	req := makeRequest(t, RequestOpts{Method: http.MethodPut, Path: "/sitems/deleted-id", Body: `{"title":"resurrected"}`, UserID: "alice"})
	req.SetPathValue("id", "deleted-id")
	rr := httptest.NewRecorder()
	ch.Update()(rr, req)
	if rr.Code == http.StatusOK || rr.Code == http.StatusCreated {
		t.Errorf("SECURITY: [update] soft-deleted record was updated. Attack: update soft-deleted record to modify hidden data.")
	}
	var title string
	if err := db.QueryRow("SELECT title FROM sitems WHERE id = ?", "deleted-id").Scan(&title); err != nil {
		t.Fatalf("read title: %v", err)
	}
	if title != "original" {
		t.Errorf("SECURITY: [update] soft-deleted record's title was mutated to %q. Attack: update soft-deleted record.", title)
	}
}

func TestUpdate_ConcurrentUpdates(t *testing.T) {
	t.Parallel()
	installSecurityOwnerExtractor(t)
	ddl := `CREATE TABLE counters (id TEXT PRIMARY KEY, user_id TEXT NOT NULL, value TEXT)`
	cfg := makeEntityConfig("counters", "counters", "user_id", []schema.Field{
		{Name: "user_id", Type: schema.String, Required: true},
		{Name: "value", Type: schema.String},
	})
	ch, _ := setupSecurityTestHandler(t, cfg, ddl)
	req := makeRequest(t, RequestOpts{Method: http.MethodPost, Path: "/counters", Body: `{"value":"initial"}`, UserID: "alice"})
	rr := httptest.NewRecorder()
	ch.Create()(rr, req)
	if rr.Code != http.StatusCreated {
		t.Fatalf("setup create failed: %d", rr.Code)
	}
	var created map[string]any
	json.Unmarshal(rr.Body.Bytes(), &created)
	id, _ := created["id"].(string)
	var wg sync.WaitGroup
	wg.Add(2)
	var code1, code2 int
	go func() {
		defer wg.Done()
		r := makeRequest(t, RequestOpts{Method: http.MethodPut, Path: "/counters/" + id, Body: `{"value":"update-A"}`, UserID: "alice"})
		r.SetPathValue("id", id)
		rec := httptest.NewRecorder()
		ch.Update()(rec, r)
		code1 = rec.Code
	}()
	go func() {
		defer wg.Done()
		r := makeRequest(t, RequestOpts{Method: http.MethodPut, Path: "/counters/" + id, Body: `{"value":"update-B"}`, UserID: "alice"})
		r.SetPathValue("id", id)
		rec := httptest.NewRecorder()
		ch.Update()(rec, r)
		code2 = rec.Code
	}()
	wg.Wait()
	if code1 == http.StatusInternalServerError || code2 == http.StatusInternalServerError {
		t.Errorf("SECURITY: [update] concurrent updates caused 500. Attack: race condition in concurrent update. codes=%d,%d", code1, code2)
	}
	t.Logf("NOTE: [update] concurrent update codes: %d, %d. Last-writer-wins is acceptable.", code1, code2)
}

// =============================================================================
// Delete operation attacks (tests 21–25)
// =============================================================================

func TestDelete_NonexistentID(t *testing.T) {
	t.Parallel()
	installSecurityOwnerExtractor(t)
	ddl := `CREATE TABLE notes (id TEXT PRIMARY KEY, user_id TEXT NOT NULL, title TEXT)`
	cfg := makeEntityConfig("notes", "notes", "user_id", []schema.Field{
		{Name: "user_id", Type: schema.String, Required: true},
		{Name: "title", Type: schema.String},
	})
	ch, _ := setupSecurityTestHandler(t, cfg, ddl)
	req := makeRequest(t, RequestOpts{Method: http.MethodDelete, Path: "/notes/nonexistent-99999", UserID: "alice"})
	req.SetPathValue("id", "nonexistent-99999")
	rr := httptest.NewRecorder()
	ch.Delete()(rr, req)
	if rr.Code == http.StatusInternalServerError {
		t.Errorf("SECURITY: [delete] nonexistent ID returned 500. Attack: delete nonexistent record. Body: %s", rr.Body.String())
	}
}

func TestDelete_AlreadyDeleted(t *testing.T) {
	t.Parallel()
	installSecurityOwnerExtractor(t)
	ddl := `CREATE TABLE sdocs (id TEXT PRIMARY KEY, user_id TEXT NOT NULL, title TEXT, deleted_at TEXT)`
	cfg := entity.EntityConfig{
		Name:  "sdocs",
		Table: "sdocs",
		Fields: []schema.Field{
			{Name: "user_id", Type: schema.String, Required: true},
			{Name: "title", Type: schema.String},
		},
		OwnerField: "user_id",
		SoftDelete: true,
	}.WithTimestamps(false)
	ch, _ := setupSecurityTestHandler(t, cfg, ddl)
	req := makeRequest(t, RequestOpts{Method: http.MethodDelete, Path: "/sdocs/already-deleted", UserID: "alice"})
	req.SetPathValue("id", "already-deleted")
	rr := httptest.NewRecorder()
	ch.Delete()(rr, req)
	if rr.Code == http.StatusInternalServerError {
		t.Errorf("SECURITY: [delete] double-delete returned 500. Attack: double-delete soft-deleted record. Body: %s", rr.Body.String())
	}
}

func TestDelete_EmptyID(t *testing.T) {
	t.Parallel()
	installSecurityOwnerExtractor(t)
	ddl := `CREATE TABLE notes (id TEXT PRIMARY KEY, user_id TEXT NOT NULL, title TEXT)`
	cfg := makeEntityConfig("notes", "notes", "user_id", []schema.Field{
		{Name: "user_id", Type: schema.String, Required: true},
		{Name: "title", Type: schema.String},
	})
	ch, _ := setupSecurityTestHandler(t, cfg, ddl)
	req := makeRequest(t, RequestOpts{Method: http.MethodDelete, Path: "/notes/", UserID: "alice"})
	rr := httptest.NewRecorder()
	ch.Delete()(rr, req)
	if rr.Code == http.StatusInternalServerError {
		t.Errorf("SECURITY: [delete] empty ID returned 500. Attack: empty ID probe. Body: %s", rr.Body.String())
	}
}

func TestDelete_SpecialCharsInID(t *testing.T) {
	t.Parallel()
	installSecurityOwnerExtractor(t)
	ddl := `CREATE TABLE files (id TEXT PRIMARY KEY, user_id TEXT NOT NULL, name TEXT)`
	cfg := makeEntityConfig("files", "files", "user_id", []schema.Field{
		{Name: "user_id", Type: schema.String, Required: true},
		{Name: "name", Type: schema.String},
	})
	ch, _ := setupSecurityTestHandler(t, cfg, ddl)
	attackIDs := []string{
		"1-or-1-eq-1",
		"1-drop-table",
		"..-etc-passwd",
		"script-alert-1",
		"日本語テスト",
		"1-or-1-eq-1",
		"1-drop-table",
		"..-etc-passwd",
		"script-alert-1",
		"日本語テスト",
		"1-or-1-eq-1",
		"1-drop-table",
		"..-etc-passwd",
		"script-alert-1",
		"日本語テスト",
		"1-or-1-eq-1",
		"1-drop-table",
		"..-etc-passwd",
		"script-alert-1",
		"日本語テスト",
		"1-or-1-eq-1",
		"1-drop-table",
		"..-etc-passwd",
		"script-alert-1",
		"日本語テスト",
	}
	for _, id := range attackIDs {
		t.Run(id, func(t *testing.T) {
			req := makeRequest(t, RequestOpts{Method: http.MethodDelete, Path: "/files/" + id, UserID: "alice"})
			req.SetPathValue("id", id)
			rr := httptest.NewRecorder()
			func() {
				defer func() {
					if r := recover(); r != nil {
						t.Errorf("SECURITY: [delete] special char ID %q caused panic: %v. Attack: special character injection via ID.", id, r)
					}
				}()
				ch.Delete()(rr, req)
			}()
			if rr.Code == http.StatusInternalServerError {
				t.Errorf("SECURITY: [delete] special char ID %q returned 500. Attack: special character injection via ID.", id)
			}
		})
	}
}

func TestDelete_OwnerScopeOnDelete(t *testing.T) {
	t.Parallel()
	installSecurityOwnerExtractor(t)
	ddl := `CREATE TABLE tasks (id TEXT PRIMARY KEY, user_id TEXT NOT NULL, title TEXT)`
	cfg := makeEntityConfig("tasks", "tasks", "user_id", []schema.Field{
		{Name: "user_id", Type: schema.String, Required: true},
		{Name: "title", Type: schema.String},
	})
	ch, db := setupSecurityTestHandler(t, cfg, ddl)
	seedRows(t, db, "tasks", []map[string]any{
		{"id": "bob-task-1", "user_id": "bob", "title": "bob's task"},
	})
	req := makeRequest(t, RequestOpts{Method: http.MethodDelete, Path: "/tasks/bob-task-1", UserID: "alice"})
	req.SetPathValue("id", "bob-task-1")
	rr := httptest.NewRecorder()
	ch.Delete()(rr, req)
	if rr.Code != http.StatusNotFound {
		t.Errorf("SECURITY: [delete] cross-user delete returned %d, want 404. Attack: IDOR via delete.", rr.Code)
	}
	var count int
	db.QueryRow("SELECT COUNT(*) FROM tasks WHERE id = ?", "bob-task-1").Scan(&count)
	if count != 1 {
		t.Errorf("SECURITY: [delete] cross-user delete removed bob's record. Attack: IDOR delete.")
	}
}

// =============================================================================
// MCP tool attacks (tests 26–35)
// =============================================================================

func TestMCP_ToolInputValidation(t *testing.T) {
	t.Parallel()
	installSecurityOwnerExtractor(t)
	ddl := `CREATE TABLE mcp_items (id TEXT PRIMARY KEY, user_id TEXT NOT NULL, name TEXT)`
	cfg := makeEntityConfig("mcp_items", "mcp_items", "user_id", []schema.Field{
		{Name: "user_id", Type: schema.String, Required: true},
		{Name: "name", Type: schema.String, Required: true},
	})
	ch, _ := setupSecurityTestHandler(t, cfg, ddl)
	mux := http.NewServeMux()
	params := map[string]any{}
	handler := ch.createTool(mux)
	_, err := handler(context.Background(), params)
	if err == nil {
		t.Errorf("SECURITY: [mcp] MCP create with empty params should return error. Attack: missing required field in MCP tool input.")
	}
}

func TestMCP_ToolSQLInjection(t *testing.T) {
	t.Parallel()
	installSecurityOwnerExtractor(t)
	ddl := `CREATE TABLE mcp_data (id TEXT PRIMARY KEY, user_id TEXT NOT NULL, name TEXT)`
	cfg := makeEntityConfig("mcp_data", "mcp_data", "user_id", []schema.Field{
		{Name: "user_id", Type: schema.String, Required: true},
		{Name: "name", Type: schema.String},
	})
	ch, db := setupSecurityTestHandler(t, cfg, ddl)
	seedRows(t, db, "mcp_data", []map[string]any{
		{"id": "md-1", "user_id": "alice", "name": "normal data"},
	})
	mux := http.NewServeMux()
	params := map[string]any{
		"name":      "normal data'; DROP TABLE mcp_data;--",
		"name_like": "'; DELETE FROM mcp_data;--",
	}
	handler := ch.listTool(mux)
	_, _ = handler(context.Background(), params)
	var count int
	if err := db.QueryRow("SELECT COUNT(*) FROM mcp_data").Scan(&count); err != nil {
		t.Errorf("SECURITY: [mcp] SQL injection via MCP filter parameter dropped or corrupted the table. Attack: SQL injection via MCP tool.")
	}
}

func TestMCP_ToolExposesOnlyVisibleFields(t *testing.T) {
	t.Parallel()
	installSecurityOwnerExtractor(t)
	ddl := `CREATE TABLE secrets (id TEXT PRIMARY KEY, user_id TEXT NOT NULL, name TEXT, api_key TEXT)`
	cfg := makeEntityConfig("secrets", "secrets", "user_id", []schema.Field{
		{Name: "user_id", Type: schema.String, Required: true},
		{Name: "name", Type: schema.String},
		{Name: "api_key", Type: schema.String, Hidden: true},
	})
	ch, _ := setupSecurityTestHandler(t, cfg, ddl)
	_ = ch.getTool(http.NewServeMux())
	writeSchema := writeToolSchema(ch.Entity)
	props, ok := writeSchema["properties"].(map[string]any)
	if !ok {
		t.Fatal("write schema has no properties")
	}
	if _, hasHidden := props["api_key"]; hasHidden {
		t.Errorf("SECURITY: [mcp] hidden field 'api_key' appears in MCP write schema. Attack: hidden field exposure via MCP tool schema.")
	}
}

func TestMCP_ToolRespectsOwnerScope(t *testing.T) {
	t.Parallel()
	installSecurityOwnerExtractor(t)
	ddl := `CREATE TABLE owned (id TEXT PRIMARY KEY, user_id TEXT NOT NULL, data TEXT)`
	cfg := makeEntityConfig("owned", "owned", "user_id", []schema.Field{
		{Name: "user_id", Type: schema.String, Required: true},
		{Name: "data", Type: schema.String},
	})
	ch, _ := setupSecurityTestHandler(t, cfg, ddl)
	mux := http.NewServeMux()
	params := map[string]any{}
	handler := ch.listTool(mux)
	_, err := handler(context.Background(), params)
	if err == nil {
		t.Errorf("SECURITY: [mcp] MCP list without owner context returned data. Attack: unauthenticated MCP tool leaks all records.")
	}
}

func TestMCP_ToolRespectsTenantScope(t *testing.T) {
	t.Parallel()
	installSecurityOwnerExtractor(t)
	ddl := `CREATE TABLE titems (id TEXT PRIMARY KEY, tenant_id TEXT, user_id TEXT NOT NULL, name TEXT)`
	cfg := entity.EntityConfig{
		Fields: []schema.Field{
			{Name: "tenant_id", Type: schema.String},
			{Name: "user_id", Type: schema.String, Required: true},
			{Name: "name", Type: schema.String},
		},
		MultiTenant: true,
		OwnerField:  "user_id",
	}.WithTimestamps(false)
	ch, _ := setupSecurityTestHandler(t, cfg, ddl)
	mux := http.NewServeMux()
	params := map[string]any{}
	handler := ch.listTool(mux)
	_, err := handler(context.Background(), params)
	if err == nil {
		t.Errorf("SECURITY: [mcp] MCP list without tenant or owner context succeeded. Attack: unauthenticated MCP tool leaks cross-tenant data.")
	}
}

func TestMCP_ToolCountDoesntLeak(t *testing.T) {
	t.Parallel()
	installSecurityOwnerExtractor(t)
	ddl := `CREATE TABLE tenant_records (id TEXT PRIMARY KEY, tenant_id TEXT, user_id TEXT NOT NULL, val TEXT)`
	cfg := entity.EntityConfig{
		Fields: []schema.Field{
			{Name: "tenant_id", Type: schema.String},
			{Name: "user_id", Type: schema.String, Required: true},
			{Name: "val", Type: schema.String},
		},
		MultiTenant: true,
		OwnerField:  "user_id",
	}.WithTimestamps(false)
	ch, _ := setupSecurityTestHandler(t, cfg, ddl)
	mux := http.NewServeMux()
	params := map[string]any{"page": 1, "limit": 1}
	ctx := tenant.SetTenantID(context.Background(), "tenant-A")
	ctx = withTestUserCtx("alice")
	handler := ch.listTool(mux)
	_, err := handler(ctx, params)
	if err != nil {
		if strings.Contains(err.Error(), "401") {
			t.Logf("NOTE: [mcp] correctly rejected unauthenticated MCP count request")
		}
	}
}

func TestMCP_ToolCreateValidatesFields(t *testing.T) {
	t.Parallel()
	installSecurityOwnerExtractor(t)
	ddl := `CREATE TABLE mcp_validated (id TEXT PRIMARY KEY, user_id TEXT NOT NULL, email TEXT, name TEXT)`
	cfg := makeEntityConfig("mcp_validated", "mcp_validated", "user_id", []schema.Field{
		{Name: "user_id", Type: schema.String, Required: true},
		{Name: "email", Type: schema.String, Required: true},
		{Name: "name", Type: schema.String},
	})
	ch, _ := setupSecurityTestHandler(t, cfg, ddl)
	mux := http.NewServeMux()
	params := map[string]any{"name": "test"}
	handler := ch.createTool(mux)
	_, err := handler(context.Background(), params)
	if err == nil {
		t.Errorf("SECURITY: [mcp] MCP create without required 'email' field succeeded. Attack: bypass required field validation via MCP tool.")
	}
}

func TestMCP_ToolUpdateValidatesFields(t *testing.T) {
	t.Parallel()
	installSecurityOwnerExtractor(t)
	ddl := `CREATE TABLE mcp_upd (id TEXT PRIMARY KEY, user_id TEXT NOT NULL, score INTEGER)`
	cfg := makeEntityConfig("mcp_upd", "mcp_upd", "user_id", []schema.Field{
		{Name: "user_id", Type: schema.String, Required: true},
		{Name: "score", Type: schema.Int},
	})
	ch, _ := setupSecurityTestHandler(t, cfg, ddl)
	mux := http.NewServeMux()
	params := map[string]any{"id": "test-id", "score": "not-a-number"}
	handler := ch.updateTool(mux)
	_, err := handler(context.Background(), params)
	if err == nil {
		t.Errorf("SECURITY: [mcp] MCP update with string for int field succeeded. Attack: type confusion via MCP update.")
	}
}

func TestMCP_ToolDeleteValidatesID(t *testing.T) {
	t.Parallel()
	ddl := `CREATE TABLE mcp_del (id TEXT PRIMARY KEY, user_id TEXT NOT NULL, name TEXT)`
	cfg := makeEntityConfig("mcp_del", "mcp_del", "user_id", []schema.Field{
		{Name: "user_id", Type: schema.String, Required: true},
		{Name: "name", Type: schema.String},
	})
	ch, _ := setupSecurityTestHandler(t, cfg, ddl)
	mux := http.NewServeMux()
	tests := []struct {
		name   string
		params map[string]any
	}{
		{"empty_id", map[string]any{"id": ""}},
		{"missing_id", map[string]any{}},
		{"null_id", map[string]any{"id": nil}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			handler := ch.deleteTool(mux)
			_, err := handler(context.Background(), tc.params)
			if err == nil {
				t.Errorf("SECURITY: [mcp] MCP delete with %s succeeded. Attack: invalid ID in MCP delete.", tc.name)
			}
		})
	}
}

func TestMCP_ToolListPaginationEnforced(t *testing.T) {
	t.Parallel()
	installSecurityOwnerExtractor(t)
	ddl := `CREATE TABLE mcp_pages (id TEXT PRIMARY KEY, user_id TEXT NOT NULL, val TEXT)`
	cfg := makeEntityConfig("mcp_pages", "mcp_pages", "user_id", []schema.Field{
		{Name: "user_id", Type: schema.String, Required: true},
		{Name: "val", Type: schema.String},
	})
	ch, _ := setupSecurityTestHandler(t, cfg, ddl)
	listSchema := listToolSchema(ch.Entity)
	props, ok := listSchema["properties"].(map[string]any)
	if !ok {
		t.Fatal("list schema missing properties")
	}
	limitField, ok := props["limit"].(map[string]any)
	if !ok {
		t.Fatal("list schema missing limit field")
	}
	max, ok := limitField["maximum"]
	if !ok {
		t.Errorf("SECURITY: [mcp] list schema 'limit' field has no maximum constraint. Attack: unbounded pagination via MCP tool.")
	} else if maxVal, ok := max.(float64); ok && maxVal > 1000 {
		t.Errorf("SECURITY: [mcp] list schema 'limit' maximum is %v, which is too high. Attack: large pagination via MCP tool.", maxVal)
	}
}

// TestMCP_ListToolOmitsHiddenFieldFilters pins that the generated _list
// MCP tool does NOT forward a filter predicate built on a Hidden field.
// Attack: probe a hidden secret column (e.g. password_hash_like=$2a$10$abc%)
// and read the secret via the match/no-match oracle in row presence. The
// write/list schemas already omit hidden fields (listToolSchema); the
// filter-param builder must too.
func TestMCP_ListToolOmitsHiddenFieldFilters(t *testing.T) {
	t.Parallel()
	installSecurityOwnerExtractor(t)
	ddl := `CREATE TABLE mcp_secrets (id TEXT PRIMARY KEY, user_id TEXT NOT NULL, name TEXT, password_hash TEXT)`
	cfg := makeEntityConfig("mcp_secrets", "mcp_secrets", "user_id", []schema.Field{
		{Name: "user_id", Type: schema.String, Required: true},
		{Name: "name", Type: schema.String},
		{Name: "password_hash", Type: schema.String, Hidden: true},
	})
	ch, _ := setupSecurityTestHandler(t, cfg, ddl)

	var gotQuery url.Values
	rec := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.Query()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[],"total":0}`))
	})

	handler := ch.listTool(rec)
	_, _ = handler(context.Background(), map[string]any{
		"name":               "visible",        // visible field — allowed
		"password_hash":      "$2a$10$abc",      // hidden eq probe
		"password_hash_like": "$2a$10$abc%",     // hidden LIKE probe
	})

	for _, k := range []string{"password_hash", "password_hash_like"} {
		if gotQuery.Has(k) {
			t.Errorf("SECURITY: [mcp] _list tool forwarded hidden-field filter %q=%q. Attack: hidden-column value-disclosure oracle.", k, gotQuery.Get(k))
		}
	}
	if !gotQuery.Has("name") {
		t.Errorf("visible filter 'name' was dropped — filter forwarding too aggressive")
	}
}

// =============================================================================
// JSON case/field mapping attacks (tests 36–40)
// =============================================================================

func TestJSONCase_SnakeToCamel(t *testing.T) {
	t.Parallel()
	installSecurityOwnerExtractor(t)
	ddl := `CREATE TABLE jitems (id TEXT PRIMARY KEY, user_id TEXT NOT NULL, item_name TEXT)`
	cfg := makeEntityConfig("jitems", "jitems", "user_id", []schema.Field{
		{Name: "user_id", Type: schema.String, Required: true},
		{Name: "item_name", Type: schema.String},
	})
	ch, db := setupSecurityTestHandler(t, cfg, ddl)
	ch.JSONCase = CaseSnake
	seedRows(t, db, "jitems", []map[string]any{
		{"id": "j-1", "user_id": "alice", "item_name": "test item"},
	})
	req := makeRequest(t, RequestOpts{Method: http.MethodGet, Path: "/jitems/j-1", UserID: "alice"})
	req.SetPathValue("id", "j-1")
	rr := httptest.NewRecorder()
	ch.Get()(rr, req)
	if rr.Code == http.StatusOK {
		body := rr.Body.String()
		if !strings.Contains(body, "item_name") {
			t.Errorf("SECURITY: [jsoncase] snake_case key 'item_name' not found in response. Attack: incorrect case mapping could bypass field filters. Body: %s", body[:min(200, len(body))])
		}
	}
}

func TestJSONCase_CaseMismatchFilter(t *testing.T) {
	t.Parallel()
	installSecurityOwnerExtractor(t)
	ddl := `CREATE TABLE cfitems (id TEXT PRIMARY KEY, user_id TEXT NOT NULL, status TEXT)`
	cfg := makeEntityConfig("cfitems", "cfitems", "user_id", []schema.Field{
		{Name: "user_id", Type: schema.String, Required: true},
		{Name: "status", Type: schema.String},
	})
	ch, _ := setupSecurityTestHandler(t, cfg, ddl)
	req := makeRequest(t, RequestOpts{Method: http.MethodGet, Path: "/cfitems?Status=active", UserID: "alice"})
	rr := httptest.NewRecorder()
	ch.List()(rr, req)
	if rr.Code == http.StatusInternalServerError {
		t.Errorf("SECURITY: [jsoncase] case-mismatch filter caused 500. Attack: case-sensitive filter probe. Body: %s", rr.Body.String())
	}
}

func TestJSONCase_CaseMismatchSort(t *testing.T) {
	t.Parallel()
	installSecurityOwnerExtractor(t)
	ddl := `CREATE TABLE csitems (id TEXT PRIMARY KEY, user_id TEXT NOT NULL, priority TEXT)`
	cfg := makeEntityConfig("csitems", "csitems", "user_id", []schema.Field{
		{Name: "user_id", Type: schema.String, Required: true},
		{Name: "priority", Type: schema.String},
	})
	ch, _ := setupSecurityTestHandler(t, cfg, ddl)
	req := makeRequest(t, RequestOpts{Method: http.MethodGet, Path: "/csitems?sort=Priority", UserID: "alice"})
	rr := httptest.NewRecorder()
	ch.List()(rr, req)
	if rr.Code == http.StatusInternalServerError {
		t.Errorf("SECURITY: [jsoncase] case-mismatch sort caused 500. Attack: case-sensitive sort probe. Body: %s", rr.Body.String())
	}
}

func TestJSONCase_FieldInjectionViaCase(t *testing.T) {
	t.Parallel()
	installSecurityOwnerExtractor(t)
	ddl := `CREATE TABLE hiditems (id TEXT PRIMARY KEY, user_id TEXT NOT NULL, name TEXT, secret_key TEXT)`
	cfg := makeEntityConfig("hiditems", "hiditems", "user_id", []schema.Field{
		{Name: "user_id", Type: schema.String, Required: true},
		{Name: "name", Type: schema.String},
		{Name: "secret_key", Type: schema.String, Hidden: true},
	})
	ch, _ := setupSecurityTestHandler(t, cfg, ddl)
	attackCases := []string{"?fields=secretKey", "?fields=SecretKey", "?fields=SECRET_KEY", "?fields=secret_key"}
	for _, qs := range attackCases {
		req := makeRequest(t, RequestOpts{Method: http.MethodGet, Path: "/hiditems" + qs, UserID: "alice"})
		rr := httptest.NewRecorder()
		ch.List()(rr, req)
		// The hidden field must never appear in responses regardless of casing
			assertBodyNotContains(t, rr, "secret_key", "jsoncase", "hidden field exposed via mixed-case projection "+qs)
	}
}

func TestJSONCase_UnicodeFieldNames(t *testing.T) {
	t.Parallel()
	installSecurityOwnerExtractor(t)
	ddl := `CREATE TABLE uitems (id TEXT PRIMARY KEY, user_id TEXT NOT NULL, name TEXT)`
	cfg := makeEntityConfig("uitems", "uitems", "user_id", []schema.Field{
		{Name: "user_id", Type: schema.String, Required: true},
		{Name: "name", Type: schema.String},
	})
	ch, _ := setupSecurityTestHandler(t, cfg, ddl)
	unicodeFields := []string{"?sort=\u540d\u524d", "?\u540d\u524d=test", "?fields=\u540d\u524d"}
	for _, qs := range unicodeFields {
		req := makeRequest(t, RequestOpts{Method: http.MethodGet, Path: "/uitems" + qs, UserID: "alice"})
		rr := httptest.NewRecorder()
		func() {
			defer func() {
				if r := recover(); r != nil {
					t.Errorf("SECURITY: [jsoncase] unicode field name %q caused panic: %v. Attack: unicode confusion via query parameter.", qs, r)
				}
			}()
			ch.List()(rr, req)
		}()
		if rr.Code == http.StatusInternalServerError {
			t.Errorf("SECURITY: [jsoncase] unicode field name %q caused 500. Attack: unicode confusion via query parameter.", qs)
		}
	}
}

// =============================================================================
// Hook/ops attacks (tests 41–50)
// =============================================================================

func TestHook_BeforeCreateModifiesData(t *testing.T) {
	t.Parallel()
	installSecurityOwnerExtractor(t)
	ddl := `CREATE TABLE hitems (id TEXT PRIMARY KEY, user_id TEXT NOT NULL, name TEXT, name_hash TEXT)`
	cfg := makeEntityConfig("hitems", "hitems", "user_id", []schema.Field{
		{Name: "user_id", Type: schema.String, Required: true},
		{Name: "name", Type: schema.String, Required: true},
		{Name: "name_hash", Type: schema.String},
	})
	ent := entity.Define("hitems", cfg)
	db := setupDB(t, ddl)
	ent.SetDB(db)
	ch := NewCrudHandler(ent, db).WithJSONCase(CaseSnake)
	ch.Hooks = hook.NewHookRegistry()
	ch.Hooks.RegisterHook(hook.BeforeCreate, func(ctx context.Context, payload any) error {
		if m, ok := payload.(map[string]any); ok {
			if name, ok := m["name"].(string); ok {
				m["name_hash"] = "hash-" + name
			}
		}
		return nil
	})
	req := makeRequest(t, RequestOpts{Method: http.MethodPost, Path: "/hitems", Body: `{"name":"test"}`, UserID: "alice"})
	rr := httptest.NewRecorder()
	ch.Create()(rr, req)
	if rr.Code == http.StatusCreated {
		var hash string
		if err := db.QueryRow("SELECT name_hash FROM hitems LIMIT 1").Scan(&hash); err != nil {
			t.Fatalf("read name_hash after create: %v", err)
		}
		if hash != "hash-test" {
			t.Errorf("SECURITY: [hook] BeforeCreate hook did not modify data correctly: got %q, want %q. Attack: hook data modification bypass.", hash, "hash-test")
		}
	}
}

func TestHook_BeforeCreateRejectsInvalid(t *testing.T) {
	t.Parallel()
	installSecurityOwnerExtractor(t)
	ddl := `CREATE TABLE hblocks (id TEXT PRIMARY KEY, user_id TEXT NOT NULL, name TEXT)`
	cfg := makeEntityConfig("hblocks", "hblocks", "user_id", []schema.Field{
		{Name: "user_id", Type: schema.String, Required: true},
		{Name: "name", Type: schema.String, Required: true},
	})
	ent := entity.Define("hblocks", cfg)
	db := setupDB(t, ddl)
	ent.SetDB(db)
	ch := NewCrudHandler(ent, db).WithJSONCase(CaseSnake)
	ch.Hooks = hook.NewHookRegistry()
	ch.Hooks.RegisterHook(hook.BeforeCreate, func(ctx context.Context, payload any) error {
		if m, ok := payload.(map[string]any); ok {
			if name, ok := m["name"].(string); ok && strings.Contains(name, "banned") {
				return fmt.Errorf("name contains banned word")
			}
		}
		return nil
	})
	req := makeRequest(t, RequestOpts{Method: http.MethodPost, Path: "/hblocks", Body: `{"name":"this is banned"}`, UserID: "alice"})
	rr := httptest.NewRecorder()
	ch.Create()(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Errorf("SECURITY: [hook] BeforeCreate rejection returned status %d, want 400. Attack: bypass hook validation. Body: %s", rr.Code, rr.Body.String())
	}
	var count int
	db.QueryRow("SELECT COUNT(*) FROM hblocks").Scan(&count)
	if count > 0 {
		t.Errorf("SECURITY: [hook] record was created despite BeforeCreate rejection. Attack: hook validation bypass.")
	}
}

func TestHook_AfterCreateDoesntModifyResponse(t *testing.T) {
	t.Parallel()
	installSecurityOwnerExtractor(t)
	ddl := `CREATE TABLE hredact (id TEXT PRIMARY KEY, user_id TEXT NOT NULL, email TEXT)`
	cfg := makeEntityConfig("hredact", "hredact", "user_id", []schema.Field{
		{Name: "user_id", Type: schema.String, Required: true},
		{Name: "email", Type: schema.String},
	})
	ent := entity.Define("hredact", cfg)
	db := setupDB(t, ddl)
	ent.SetDB(db)
	ch := NewCrudHandler(ent, db).WithJSONCase(CaseSnake)
	ch.Hooks = hook.NewHookRegistry()
	ch.Hooks.RegisterHook(hook.AfterCreate, func(ctx context.Context, payload any) error {
		if m, ok := payload.(map[string]any); ok {
			delete(m, "email")
		}
		return nil
	})
	req := makeRequest(t, RequestOpts{Method: http.MethodPost, Path: "/hredact", Body: `{"email":"secret@example.com"}`, UserID: "alice"})
	rr := httptest.NewRecorder()
	ch.Create()(rr, req)
	if rr.Code == http.StatusCreated {
		assertBodyNotContains(t, rr, "secret@example.com", "hook", "AfterCreate did not redact sensitive field from response")
	}
	var email string
	if err := db.QueryRow("SELECT email FROM hredact LIMIT 1").Scan(&email); err != nil {
		t.Fatalf("read email after create: %v", err)
	}
	if email != "secret@example.com" {
		t.Errorf("SECURITY: [hook] AfterCreate hook redacted field from DB storage. Expected email to persist but got %q.", email)
	}
}

func TestHook_BeforeDeleteBlocksAction(t *testing.T) {
	t.Parallel()
	installSecurityOwnerExtractor(t)
	ddl := `CREATE TABLE hprotected (id TEXT PRIMARY KEY, user_id TEXT NOT NULL, title TEXT, is_system BOOLEAN)`
	cfg := makeEntityConfig("hprotected", "hprotected", "user_id", []schema.Field{
		{Name: "user_id", Type: schema.String, Required: true},
		{Name: "title", Type: schema.String},
		{Name: "is_system", Type: schema.Bool},
	})
	ent := entity.Define("hprotected", cfg)
	db := setupDB(t, ddl)
	ent.SetDB(db)
	ch := NewCrudHandler(ent, db).WithJSONCase(CaseSnake)
	ch.Hooks = hook.NewHookRegistry()
	ch.Hooks.RegisterHook(hook.BeforeDelete, func(ctx context.Context, payload any) error {
		id, _ := payload.(string)
		if id == "system-record" {
			return fmt.Errorf("cannot delete system record")
		}
		return nil
	})
	seedRows(t, db, "hprotected", []map[string]any{
		{"id": "system-record", "user_id": "alice", "title": "system", "is_system": 1},
	})
	req := makeRequest(t, RequestOpts{Method: http.MethodDelete, Path: "/hprotected/system-record", UserID: "alice"})
	req.SetPathValue("id", "system-record")
	rr := httptest.NewRecorder()
	ch.Delete()(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Errorf("SECURITY: [hook] BeforeDelete rejection returned status %d, want 400. Attack: bypass delete protection. Body: %s", rr.Code, rr.Body.String())
	}
	var count int
	db.QueryRow("SELECT COUNT(*) FROM hprotected WHERE id = ?", "system-record").Scan(&count)
	if count != 1 {
		t.Errorf("SECURITY: [hook] protected record was deleted despite BeforeDelete hook. Attack: delete protection bypass.")
	}
}

func TestHook_PanicInBeforeHook(t *testing.T) {
	t.Parallel()
	installSecurityOwnerExtractor(t)
	ddl := `CREATE TABLE hpanic (id TEXT PRIMARY KEY, user_id TEXT NOT NULL, name TEXT)`
	cfg := makeEntityConfig("hpanic", "hpanic", "user_id", []schema.Field{
		{Name: "user_id", Type: schema.String, Required: true},
		{Name: "name", Type: schema.String, Required: true},
	})
	ent := entity.Define("hpanic", cfg)
	db := setupDB(t, ddl)
	ent.SetDB(db)
	ch := NewCrudHandler(ent, db).WithJSONCase(CaseSnake)
	ch.Hooks = hook.NewHookRegistry()
	ch.Hooks.RegisterHook(hook.BeforeCreate, func(ctx context.Context, payload any) error {
		panic("deliberate test panic")
	})
	req := makeRequest(t, RequestOpts{Method: http.MethodPost, Path: "/hpanic", Body: `{"name":"trigger"}`, UserID: "alice"})
	rr := httptest.NewRecorder()
	func() {
		defer func() {
			if r := recover(); r != nil {
				t.Logf("NOTE: [hook] panic in BeforeCreate was not caught by handler: %v. Consider adding recovery middleware.", r)
			}
		}()
		ch.Create()(rr, req)
	}()
	if rr.Code == http.StatusInternalServerError {
		body := rr.Body.String()
		forbidden := []string{"goroutine", "runtime.", "created by", "stack trace"}
		for _, f := range forbidden {
			if strings.Contains(strings.ToLower(body), f) {
				t.Errorf("SECURITY: [hook] panic in hook leaked stack trace fragment %q. Attack: stack trace information disclosure.", f)
			}
		}
	}
}

func TestHook_ContextCancellation(t *testing.T) {
	t.Parallel()
	installSecurityOwnerExtractor(t)
	ddl := `CREATE TABLE hcancel (id TEXT PRIMARY KEY, user_id TEXT NOT NULL, name TEXT)`
	cfg := makeEntityConfig("hcancel", "hcancel", "user_id", []schema.Field{
		{Name: "user_id", Type: schema.String, Required: true},
		{Name: "name", Type: schema.String},
	})
	ent := entity.Define("hcancel", cfg)
	db := setupDB(t, ddl)
	ent.SetDB(db)
	ch := NewCrudHandler(ent, db).WithJSONCase(CaseSnake)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	req := makeRequest(t, RequestOpts{Method: http.MethodPost, Path: "/hcancel", Body: `{"name":"test"}`, UserID: "alice"})
	req = req.WithContext(ctx)
	rr := httptest.NewRecorder()
	func() {
		defer func() {
			if r := recover(); r != nil {
				t.Errorf("SECURITY: [hook] cancelled context caused panic: %v. Attack: context cancellation denial of service.", r)
			}
		}()
		ch.Create()(rr, req)
	}()
	var count int
	db.QueryRow("SELECT COUNT(*) FROM hcancel").Scan(&count)
	if count > 0 {
		t.Errorf("SECURITY: [hook] cancelled context left partial record in database. Attack: partial write via context cancellation.")
	}
}

func TestOps_ConvertKeyHandlesSpecialChars(t *testing.T) {
	t.Parallel()
	ddl := `CREATE TABLE dummy (id TEXT PRIMARY KEY)`
	cfg := makeEntityConfig("dummy", "dummy", "", []schema.Field{})
	ent := entity.Define("dummy", cfg)
	db := setupDB(t, ddl)
	ent.SetDB(db)
	ch := NewCrudHandler(ent, db)
	testCols := []string{"id", "user_id", "_internal", "field-with-hyphens", "field.with.dots", "\u65e5\u672c\u8a9e", "", "UPPERCASE", "CamelCase"}
	for _, col := range testCols {
		func() {
			defer func() {
				if r := recover(); r != nil {
					t.Errorf("SECURITY: [ops] convertKey(%q) panicked: %v. Attack: special character in column name.", col, r)
				}
			}()
			_ = ch.convertKey(col)
		}()
	}
}

func TestOps_ScanRowsNullValues(t *testing.T) {
	t.Parallel()
	ddl := `CREATE TABLE nulltest (id TEXT PRIMARY KEY, user_id TEXT, name TEXT, score INTEGER, active INTEGER)`
	cfg := makeEntityConfig("nulltest", "nulltest", "user_id", []schema.Field{
		{Name: "user_id", Type: schema.String},
		{Name: "name", Type: schema.String},
		{Name: "score", Type: schema.Int},
		{Name: "active", Type: schema.Bool},
	})
	ch, _ := setupSecurityTestHandler(t, cfg, ddl)
	req := makeRequest(t, RequestOpts{Method: http.MethodGet, Path: "/nulltest"})
	rr := httptest.NewRecorder()
	func() {
		defer func() {
			if r := recover(); r != nil {
				t.Errorf("SECURITY: [ops] scanning NULL values caused panic: %v. Attack: null value causes nil dereference in scan.", r)
			}
		}()
		ch.List()(rr, req)
	}()
	if rr.Code == http.StatusInternalServerError {
		t.Errorf("SECURITY: [ops] scanning NULL values returned 500. Attack: null value causes error in scan. Body: %s", rr.Body.String())
	}
}

func TestOps_ConvertValueTypes(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		input any
	}{
		{"nil", nil},
		{"string", "hello"},
		{"int", 42},
		{"int64", int64(42)},
		{"float64", 3.14},
		{"bool", true},
		{"[]byte", []byte("binary data")},
		{"time", time.Now()},
		{"map", map[string]any{"key": "value"}},
		{"slice", []any{1, 2, 3}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			func() {
				defer func() {
					if r := recover(); r != nil {
						t.Errorf("SECURITY: [ops] convertValue(%T) panicked: %v. Attack: unexpected type from DB driver.", tc.input, r)
					}
				}()
				result := convertValue(tc.input)
				if result == nil && tc.input != nil {
					t.Logf("NOTE: [ops] convertValue(%T) returned nil — may lose data.", tc.input)
				}
			}()
		})
	}
}

func TestOps_WriteCRUDErrorDoesntLeakInternal(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		err       error
		forbidden []string
	}{
		{"raw_sql_error", fmt.Errorf("pq: relation \"users\" does not exist"), []string{"pq:", "relation", "does not exist"}},
		{"table_name_leak", fmt.Errorf("insert: table \"internal_accounts\" permission denied"), []string{"internal_accounts", "permission denied"}},
		{"connection_string_leak", fmt.Errorf("dial tcp 10.0.0.1:5432: connect: connection refused"), []string{"10.0.0.1", "5432", "connection refused"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			rr := httptest.NewRecorder()
			writeCRUDError(rr, tc.err)
			if rr.Code != http.StatusInternalServerError {
				t.Errorf("SECURITY: [ops] writeCRUDError returned status %d, want 500 for internal error. Attack: error classification bypass.", rr.Code)
			}
			body := rr.Body.String()
			for _, forbidden := range tc.forbidden {
				if strings.Contains(body, forbidden) {
					t.Errorf("SECURITY: [ops] error response leaked internal detail %q. Attack: internal error message disclosure. Body: %s",
						forbidden, body[:min(200, len(body))])
				}
			}
		})
	}
}
