package admin

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/battery/queue"
	"github.com/DonaldMurillo/gofastr/core/handler"
	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
)

type errBrowsableQueue struct {
	err error
}

func (e errBrowsableQueue) ListJobs(ctx context.Context, status string, limit int) ([]queue.Job, error) {
	return nil, e.err
}

func (e errBrowsableQueue) Stats(ctx context.Context) (queue.JobStats, error) {
	return queue.JobStats{}, nil
}

func TestAdmin_IndexRequiresAuthentication(t *testing.T) {
	h := mountAdminBare(t, Config{})
	req := httptest.NewRequest(http.MethodGet, "/admin", nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("SECURITY: [admin] unauthenticated /admin returned %d. Attack: admin overview exposed without auth.", rr.Code)
	}
}

func TestAdmin_QueuePageRequiresAuthentication(t *testing.T) {
	h := mountAdminBare(t, Config{Queue: errBrowsableQueue{}})
	req := httptest.NewRequest(http.MethodGet, "/admin/queue", nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("SECURITY: [admin] unauthenticated /admin/queue returned %d. Attack: queue dashboard exposed without auth.", rr.Code)
	}
}

func TestAdmin_AuditPageRequiresAuthentication(t *testing.T) {
	db := newDB(t)
	newAuditTable(t, db)
	h := mountAdminBare(t, Config{DB: db})
	req := httptest.NewRequest(http.MethodGet, "/admin/audit", nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("SECURITY: [admin] unauthenticated /admin/audit returned %d. Attack: audit dashboard exposed without auth.", rr.Code)
	}
}

func TestAdmin_QueueErrorDoesNotLeakInternalText(t *testing.T) {
	b := New(Config{Queue: errBrowsableQueue{err: errors.New("dial tcp 10.0.0.5:5432 password=secret")}})
	req := httptest.NewRequest(http.MethodGet, "/admin/queue", nil)
	rr := httptest.NewRecorder()
	b.handleQueue(rr, req)

	body := rr.Body.String()
	if strings.Contains(body, "10.0.0.5") || strings.Contains(body, "password=secret") {
		t.Fatalf("SECURITY: [admin] queue page leaked internal error text: %q", body)
	}
}

func TestAdmin_AuditErrorDoesNotLeakInternalText(t *testing.T) {
	db := newDB(t)
	_ = db.Close()
	b := New(Config{DB: db})
	req := httptest.NewRequest(http.MethodGet, "/admin/audit", nil)
	rr := httptest.NewRecorder()
	b.handleAudit(rr, req)

	body := rr.Body.String()
	if strings.Contains(strings.ToLower(body), "database is closed") || strings.Contains(strings.ToLower(body), "sql:") {
		t.Fatalf("SECURITY: [admin] audit page leaked internal DB error text: %q", body)
	}
}

func TestAdmin_ResponseCarriesFrameDenyHeader(t *testing.T) {
	h := mountAdmin(t, Config{})
	req := httptest.NewRequest(http.MethodGet, "/admin", nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Header().Get("X-Frame-Options") != "DENY" {
		t.Fatalf("SECURITY: [admin] admin page missing X-Frame-Options DENY: %#v", rr.Header())
	}
}

func TestAdmin_ResponseCarriesContentSecurityPolicy(t *testing.T) {
	h := mountAdmin(t, Config{})
	req := httptest.NewRequest(http.MethodGet, "/admin", nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Header().Get("Content-Security-Policy") == "" {
		t.Fatalf("SECURITY: [admin] admin page missing Content-Security-Policy header: %#v", rr.Header())
	}
}

func TestAdmin_ResponseCarriesNoSniffHeader(t *testing.T) {
	h := mountAdmin(t, Config{})
	req := httptest.NewRequest(http.MethodGet, "/admin", nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Fatalf("SECURITY: [admin] admin page missing X-Content-Type-Options nosniff: %#v", rr.Header())
	}
}

func TestAdmin_ResponseCarriesReferrerPolicy(t *testing.T) {
	h := mountAdmin(t, Config{})
	req := httptest.NewRequest(http.MethodGet, "/admin", nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Header().Get("Referrer-Policy") == "" {
		t.Fatalf("SECURITY: [admin] admin page missing Referrer-Policy header: %#v", rr.Header())
	}
}

var _ queue.Browsable = errBrowsableQueue{}
var _ = sql.ErrNoRows

// TestAdmin_QueueReplayRequiresAuth pins that the mutating replay endpoint is
// gated. An ungated /queue/_replay/{id} would let anyone re-fire dead-lettered
// jobs (privilege escalation / job amplification).
func TestAdmin_QueueReplayRequiresAuth(t *testing.T) {
	h := mountAdminBare(t, Config{})
	req := httptest.NewRequest(http.MethodPost, "/admin/queue/_replay/some-job-id", nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("SECURITY: [admin] unauthenticated POST /queue/_replay returned %d, want 401. Attack: anyone re-fires dead jobs.", rr.Code)
	}
}

// TestAdmin_QueueReplayForbidsNonAdmin confirms an authenticated non-admin is
// refused (403), same gate as the rest of the admin.
func TestAdmin_QueueReplayForbidsNonAdmin(t *testing.T) {
	h := mountAdminBare(t, Config{})
	req := httptest.NewRequest(http.MethodPost, "/admin/queue/_replay/some-job-id", nil)
	req = req.WithContext(handler.SetUser(req.Context(), roleUser{roles: []string{"reader"}}))
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusForbidden {
		t.Fatalf("non-admin POST /queue/_replay = %d, want 403", rr.Code)
	}
}

// TestAdminFormIgnoresNonEditableKeys pins the admin entity form's field
// whitelist: formToJSON builds the CRUD body from editableFields only, so
// posted keys for Hidden fields (write-capable at the schema layer —
// Hidden gates responses, not writes), ReadOnly fields, and entirely
// unknown columns never reach the create/update body. Without the
// whitelist, crafting a form POST with api_key/revision/is_admin values
// would mass-assign columns the screen never renders.
func TestAdminFormIgnoresNonEditableKeys(t *testing.T) {
	db := newDB(t)
	cfg := entity.EntityConfig{
		Table: "gizmos",
		Fields: []schema.Field{
			{Name: "name", Type: schema.String, Required: true},
			{Name: "api_key", Type: schema.String, Hidden: true},
			{Name: "revision", Type: schema.Int, ReadOnly: true},
		},
	}.WithTimestamps(false)
	app := newHostedApp(t, db, map[string]entity.EntityConfig{"gizmos": cfg})
	h := mountEntityAdmin(t, app, Config{Entities: []string{"gizmos"}}, testUser{"u1"})

	rr := postForm(h, "/admin/e/gizmos/_create", url.Values{
		"name":     {"legit"},
		"api_key":  {"PWNED-SECRET"}, // Hidden field
		"revision": {"999"},          // ReadOnly field
		"is_admin": {"true"},         // unknown key entirely
	})
	if rr.Code != http.StatusSeeOther {
		t.Fatalf("create form: status=%d body=%s", rr.Code, rr.Body.String())
	}

	var name string
	var apiKey sql.NullString
	var revision sql.NullInt64
	err := db.QueryRow(`SELECT name, api_key, revision FROM gizmos`).Scan(&name, &apiKey, &revision)
	if err != nil {
		t.Fatalf("read row: %v", err)
	}
	if name != "legit" {
		t.Fatalf("editable field not persisted: name=%q", name)
	}
	if apiKey.Valid && apiKey.String != "" {
		t.Fatalf("SECURITY: [admin-form-whitelist] posted Hidden field value reached the row: api_key=%q", apiKey.String)
	}
	if revision.Valid && revision.Int64 != 0 {
		t.Fatalf("SECURITY: [admin-form-whitelist] posted ReadOnly field value reached the row: revision=%d", revision.Int64)
	}
}
