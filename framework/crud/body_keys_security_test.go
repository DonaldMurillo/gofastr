package crud

import (
	"database/sql"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
)

// A JSON body whose DISTINCT wire keys fold onto the SAME column is
// rejected deterministically (400), never silently last-wins: map
// iteration order otherwise decides which value persists, so neither the
// client nor a reviewer reading one of the two keys can know what was
// stored. Unknown keys that collide with nothing still pass through
// (pinned by TestWireName_RoundTripsBothCasings).

// setupCamelDocsHandler builds an un-gated docs-style entity using
// CaseCamel (the framework default), where the wire key for body_text is
// "bodyText" and the raw fallback re-fold of "body_text" resolves to the
// same column.
func setupCamelDocsHandler(t *testing.T) (*CrudHandler, *sql.DB) {
	t.Helper()
	db := setupDB(t, `CREATE TABLE reddocs (id TEXT PRIMARY KEY, body_text TEXT)`)
	ent := entity.Define("reddocs", entity.EntityConfig{
		Table:  "reddocs",
		Fields: []schema.Field{{Name: "body_text", Type: schema.String}},
	}.WithTimestamps(false))
	ent.SetDB(db)
	return NewCrudHandler(ent, db).WithJSONCase(CaseCamel), db
}

// The same wire key twice in one body is a duplicate, refused by the
// strict decode (handler.UnmarshalStrict) readRequestBody runs.
func TestMapBodyRejectsDuplicateKeys(t *testing.T) {
	t.Run("create", func(t *testing.T) {
		ch, db := setupCamelDocsHandler(t)
		req := httptest.NewRequest(http.MethodPost, "/api/reddocs",
			strings.NewReader(`{"body_text":"wire-form","body_text":"raw-form"}`))
		req.Header.Set("Content-Type", "application/json")
		req = withTestUser(req, "alice")
		rec := httptest.NewRecorder()
		ch.Create()(rec, req)
		if rec.Code != http.StatusBadRequest {
			var stored string
			_ = db.QueryRow(`SELECT body_text FROM reddocs LIMIT 1`).Scan(&stored)
			t.Errorf("SECURITY: [map-key-collision] create with the same wire key \"body_text\" twice returned %d, want 400 (deterministic rejection). The map decode kept last-wins %q. body=%s", rec.Code, stored, rec.Body.String())
		}
	})
	t.Run("update", func(t *testing.T) {
		ch, db := setupCamelDocsHandler(t)
		if _, err := db.Exec(`INSERT INTO reddocs (id, body_text) VALUES ('d1','orig')`); err != nil {
			t.Fatal(err)
		}
		req := httptest.NewRequest(http.MethodPatch, "/api/reddocs/d1",
			strings.NewReader(`{"body_text":"wire-form","body_text":"raw-form"}`))
		req.Header.Set("Content-Type", "application/json")
		req = withTestUser(req, "alice")
		req.SetPathValue("id", "d1")
		rec := httptest.NewRecorder()
		ch.Update()(rec, req)
		if rec.Code != http.StatusBadRequest {
			var stored string
			_ = db.QueryRow(`SELECT body_text FROM reddocs WHERE id='d1'`).Scan(&stored)
			t.Errorf("SECURITY: [map-key-collision] update with the same wire key \"body_text\" twice returned %d, want 400 (deterministic rejection). The map decode kept last-wins %q. body=%s", rec.Code, stored, rec.Body.String())
		}
	})
}

// Two DISTINCT wire keys folding onto one column (CaseCamel's "bodyText"
// and "body_text") are refused by crud's own fold
// (handler.CheckTopLevelKeys with wireKeyColumn) before the decode.
func TestMapBodyRejectsCaseFoldedKeys(t *testing.T) {
	t.Run("create", func(t *testing.T) {
		ch, db := setupCamelDocsHandler(t)
		req := httptest.NewRequest(http.MethodPost, "/api/reddocs",
			strings.NewReader(`{"bodyText":"wire-form","body_text":"raw-form"}`))
		req.Header.Set("Content-Type", "application/json")
		req = withTestUser(req, "alice")
		rec := httptest.NewRecorder()
		ch.Create()(rec, req)
		if rec.Code != http.StatusBadRequest {
			var stored string
			_ = db.QueryRow(`SELECT body_text FROM reddocs LIMIT 1`).Scan(&stored)
			t.Errorf("SECURITY: [map-key-collision] create with two wire keys folding onto body_text returned %d, want 400 (deterministic rejection). Stored value was nondeterministically %q. body=%s", rec.Code, stored, rec.Body.String())
		}
	})
	t.Run("update", func(t *testing.T) {
		ch, db := setupCamelDocsHandler(t)
		if _, err := db.Exec(`INSERT INTO reddocs (id, body_text) VALUES ('d1','orig')`); err != nil {
			t.Fatal(err)
		}
		req := httptest.NewRequest(http.MethodPatch, "/api/reddocs/d1",
			strings.NewReader(`{"bodyText":"wire-form","body_text":"raw-form"}`))
		req.Header.Set("Content-Type", "application/json")
		req = withTestUser(req, "alice")
		req.SetPathValue("id", "d1")
		rec := httptest.NewRecorder()
		ch.Update()(rec, req)
		if rec.Code != http.StatusBadRequest {
			var stored string
			_ = db.QueryRow(`SELECT body_text FROM reddocs WHERE id='d1'`).Scan(&stored)
			t.Errorf("SECURITY: [map-key-collision] update with two wire keys folding onto body_text returned %d, want 400 (deterministic rejection). Stored value was nondeterministically %q. body=%s", rec.Code, stored, rec.Body.String())
		}
	})
}

// The batch surface folds item keys through the same unconvertMapKeys,
// so the same two-spelling body is refused per item (400, whole batch,
// before the transaction opens) rather than resolved by map order.
func TestBatchItemsRejectFoldedKeys(t *testing.T) {
	ch, db := setupCamelDocsHandler(t)
	if _, err := db.Exec(`INSERT INTO reddocs (id, body_text) VALUES ('d1','orig'), ('d2','orig')`); err != nil {
		t.Fatal(err)
	}
	for name, build := range map[string]func() (*http.Request, string){
		"create": func() (*http.Request, string) {
			req := httptest.NewRequest(http.MethodPost, "/api/reddocs/_batch",
				strings.NewReader(`{"items":[{"bodyText":"wire-form","body_text":"raw-form"}]}`))
			return req, ""
		},
		"update": func() (*http.Request, string) {
			req := httptest.NewRequest(http.MethodPatch, "/api/reddocs/_batch",
				strings.NewReader(`{"items":[{"id":"d1","bodyText":"wire-form","body_text":"raw-form"}]}`))
			return req, "d1"
		},
	} {
		req, id := build()
		req.Header.Set("Content-Type", "application/json")
		req = withTestUser(req, "alice")
		if name == "update" {
			req.SetPathValue("id", id)
		}
		rec := httptest.NewRecorder()
		if name == "create" {
			ch.BatchCreate()(rec, req)
		} else {
			ch.BatchUpdate()(rec, req)
		}
		if rec.Code != http.StatusBadRequest {
			t.Errorf("%s: folded item keys returned %d, want 400 (deterministic rejection). body=%s", name, rec.Code, rec.Body.String())
		}
	}
	var n int
	_ = db.QueryRow(`SELECT COUNT(*) FROM reddocs WHERE body_text IN ('wire-form','raw-form')`).Scan(&n)
	if n != 0 {
		t.Errorf("folded item keys persisted a value: %d rows carry one of the smuggled spellings", n)
	}
}
